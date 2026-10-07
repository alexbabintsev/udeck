package widgets

import (
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
)

func siteServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestSiteAPIGetSendsTokenAndDecodes(t *testing.T) {
	srv := siteServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.URL.Path != "/x" {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"visitors": 7}`)
	})
	api := newSiteAPI(&config.Widget{URL: srv.URL + "/ ", Token: " secret "})
	var out siteOnlineStats
	if err := api.get("/x", &out); err != nil || out.Visitors != 7 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if err := (siteAPI{baseURL: srv.URL}).get("/x", &out); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected status error, got %v", err)
	}
	if err := (siteAPI{baseURL: "http://\x00bad"}).get("/x", &out); err == nil {
		t.Fatal("expected request error")
	}
	if err := (siteAPI{baseURL: "http://127.0.0.1:1"}).get("/x", &out); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestSiteAPIRejectsOversizedOrInvalidBody(t *testing.T) {
	srv := siteServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"visitors": `+strings.Repeat("1", maxHTTPBodyBytes+10)+`}`)
	})
	var out siteOnlineStats
	if err := (siteAPI{baseURL: srv.URL}).get("/", &out); err == nil {
		t.Fatal("expected error for a body larger than the limit")
	}
}

func TestSitePoll(t *testing.T) {
	if sitePoll(0) != siteDefaultPoll || sitePoll(1000) != siteMinPoll || sitePoll(120000) != 2*time.Minute {
		t.Fatal("sitePoll")
	}
}

func TestSiteOnlineWidgetFetchesAndRendersOnChange(t *testing.T) {
	var visitors atomic.Int32
	visitors.Store(12)
	srv := siteServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != siteOnlinePath {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"visitors": %d}`, visitors.Load())
	})
	rec := &recorder{}
	w, err := NewSiteOnlineWidget("online", &config.Widget{URL: srv.URL}, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitFor(t, "first fetch", func() bool { return w.frameKey() == "12" })
	waitFor(t, "notification", func() bool { return rec.count() == 1 })

	// Same value: no new notification.
	w.refresh()
	if rec.count() != 1 {
		t.Fatal("unchanged value must not notify")
	}
	visitors.Store(3000)
	w.HandleGesture(ig.Tap)
	waitFor(t, "tap refresh", func() bool { return rec.count() == 2 })
	w.HandleGesture(ig.Hold) // ignored
}

func TestSiteOnlineWidgetKeepsValueOnError(t *testing.T) {
	var fail atomic.Bool
	srv := siteServer(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `{"visitors": 5}`)
	})
	w, err := NewSiteOnlineWidget("online", &config.Widget{URL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitFor(t, "first fetch", func() bool { return w.frameKey() == "5" })
	fail.Store(true)
	w.refresh()
	if w.frameKey() != "5" {
		t.Fatal("failed fetch must keep the last value")
	}
}

func TestSiteViewsWidgetFetchesBothWindows(t *testing.T) {
	var failHourly atomic.Bool
	srv := siteServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/metrics/views/daily":
			fmt.Fprint(w, `{"views": 12345, "visitors": 900, "change": -12.5}`)
		case "/api/metrics/views/hourly":
			if failHourly.Load() {
				http.Error(w, "x", http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `{"views": 42, "visitors": 7, "change": 10}`)
		default:
			http.NotFound(w, r)
		}
	})
	rec := &recorder{}
	w, err := NewSiteViewsWidget("views", &config.Widget{URL: srv.URL, Title: " Site "}, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitFor(t, "both windows", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.data.Daily != nil && w.data.Hourly != nil
	})
	waitFor(t, "notification", func() bool { return rec.count() >= 1 })

	failHourly.Store(true)
	w.HandleGesture(ig.Tap)
	time.Sleep(50 * time.Millisecond)
	w.mu.Lock()
	hourly := w.data.Hourly
	w.mu.Unlock()
	if hourly == nil || hourly.Views != 42 {
		t.Fatal("a failed window must keep its previous value")
	}
	w.HandleGesture(ig.Hold)
}

func TestSiteViewsFormatting(t *testing.T) {
	for in, want := range map[int]string{5: "5", 1500: "1.5k", 15000: "15k", 2500000: "2.5M"} {
		if got := formatSiteViewsCount(in); got != want {
			t.Fatalf("formatSiteViewsCount(%d) = %q, want %q", in, got, want)
		}
	}
	if formatSiteViewsChange(12.4) != "+12%" || formatSiteViewsChange(-3.6) != "-4%" {
		t.Fatal("formatSiteViewsChange")
	}
	if siteViewsChangeColor(-1) == siteViewsChangeColor(1) {
		t.Fatal("change colors must differ by sign")
	}
	if siteViewsFrameKey("t", siteViewsData{}) == siteViewsFrameKey("t", siteViewsData{Daily: &siteViewsStats{Views: 1}}) {
		t.Fatal("frame key must reflect data")
	}
}

func TestSiteCardsRenderPlaceholdersAndValues(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 196, 196))
	renderSiteViewsCard(img, "", siteViewsData{})
	renderSiteViewsCard(img, "", siteViewsData{
		Daily:  &siteViewsStats{Views: 100, Visitors: 10, Change: 5},
		Hourly: &siteViewsStats{Views: 3, Visitors: 1, Change: -5},
	})
	renderSiteOnlineCard(img, nil)
	renderSiteOnlineCard(img, &siteOnlineStats{Visitors: 0})
	renderSiteOnlineCard(img, &siteOnlineStats{Visitors: 123456})
}
