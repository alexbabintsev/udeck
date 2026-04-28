package widgets

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
)

func TestParseFreqtradeDSNs(t *testing.T) {
	got := parseFreqtradeDSNs("# comment\nalice:secret@http://127.0.0.1:9910\nbroken\nbob:pass@https://ft.example.com/\n")
	if len(got) != 2 {
		t.Fatalf("expected 2 apis, got %d", len(got))
	}
	if got[0].User != "alice" || got[0].Pass != "secret" || got[0].URL != "http://127.0.0.1:9910" || got[0].Name != "bot1" {
		t.Fatalf("unexpected first api: %+v", got[0])
	}
	if got[1].URL != "https://ft.example.com" || got[1].Name != "bot2" {
		t.Fatalf("unexpected second api: %+v", got[1])
	}
}

func TestFetchAllFreqtradeData(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/trades":
			if r.URL.Query().Get("limit") == "1" {
				_, _ = w.Write([]byte(`{"total_trades":4}`))
				return
			}
			_, _ = w.Write([]byte(`{"trades":[
				{"is_open":false,"profit_abs":1.11119,"pair":"BTC/USDT","close_timestamp":200},
				{"is_open":false,"profit_abs":-2.34567,"pair":"ETH/USDT","close_timestamp":150},
				{"is_open":true,"profit_abs":99,"pair":"XRP/USDT","close_timestamp":210}
			]}`))
		case "/api/v1/status":
			_, _ = w.Write([]byte(`[{"profit_abs":0.5},{"profit_abs":1.25}]`))
		case "/api/v1/profit":
			_, _ = w.Write([]byte(`{"closed_trade_count":2,"profit_closed_coin":3.33339}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/trades":
			if r.URL.Query().Get("limit") == "1" {
				_, _ = w.Write([]byte(`{"total_trades":3}`))
				return
			}
			_, _ = w.Write([]byte(`{"trades":[
				{"is_open":false,"profit_abs":4.0,"pair":"SOL/USDT","close_timestamp":300},
				{"is_open":false,"profit_abs":0.125,"pair":"ADA/USDT","close_timestamp":250}
			]}`))
		case "/api/v1/status":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v1/profit":
			_, _ = w.Write([]byte(`{"trade_count":1,"profit_closed_coin":-1.25}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv2.Close()

	trades, summary := fetchAllFreqtradeData([]freqtradeAPI{
		{User: "a", Pass: "b", URL: srv1.URL, Name: "bot1"},
		{User: "c", Pass: "d", URL: srv2.URL, Name: "bot2"},
	})
	if len(trades) != 3 {
		t.Fatalf("expected top 3 trades, got %d", len(trades))
	}
	if trades[0].CloseTimestamp != 300 || trades[1].CloseTimestamp != 250 || trades[2].CloseTimestamp != 200 {
		t.Fatalf("unexpected trade ordering: %+v", trades)
	}
	if summary == nil || summary.OpenProfit == nil || summary.ClosedProfit == nil {
		t.Fatalf("expected non-nil summary, got %+v", summary)
	}
	if *summary.OpenProfit != 1.75 {
		t.Fatalf("unexpected open profit: %v", *summary.OpenProfit)
	}
	if *summary.ClosedProfit != 2.0834 {
		t.Fatalf("unexpected closed profit: %v", *summary.ClosedProfit)
	}
}

func TestFreqtradeWidgetTapCyclesMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/trades":
			if r.URL.Query().Get("limit") == "1" {
				_, _ = w.Write([]byte(`{"total_trades":1}`))
				return
			}
			_, _ = w.Write([]byte(`{"trades":[{"is_open":false,"profit_abs":1.25,"pair":"BTC/USDT","close_timestamp":100}]}`))
		case "/api/v1/status":
			_, _ = w.Write([]byte(`[{"profit_abs":0.75}]`))
		case "/api/v1/profit":
			_, _ = w.Write([]byte(`{"closed_trade_count":1,"profit_closed_coin":2.5}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	w, err := NewFreqtradeWidget("buttons[0]", &config.Widget{
		Type:   "freqtrade",
		DSNs:   "user:pass@" + srv.URL,
		Title:  "BTC",
		PollMS: 3000,
	}, func(string) {})
	if err != nil {
		t.Fatalf("new freqtrade widget: %v", err)
	}
	defer w.Close()

	waitForFile(t, w.IconPath())
	w.refresh()
	waitForFile(t, w.IconPath())

	before, err := os.ReadFile(w.IconPath())
	if err != nil {
		t.Fatalf("read initial icon: %v", err)
	}

	w.HandleGesture(ig.Tap)

	after, err := os.ReadFile(w.IconPath())
	if err != nil {
		t.Fatalf("read cycled icon: %v", err)
	}
	if string(before) == string(after) {
		t.Fatal("expected icon to change after mode switch")
	}

	if _, err := os.Stat(filepath.Dir(w.IconPath())); err != nil {
		t.Fatalf("expected widget temp dir to exist before close: %v", err)
	}
}
