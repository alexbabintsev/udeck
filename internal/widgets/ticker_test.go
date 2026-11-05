package widgets

import (
	"errors"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
)

func TestFormatPrice(t *testing.T) {
	cases := map[float64]string{
		78299.99:   "78,300",
		107428.26:  "107,428",
		1234567:    "1,234,567",
		4321.5:     "4,321.50",
		331.9:      "331.90",
		12.5:       "12.50",
		1.08321:    "1.0832",
		0.12345:    "0.1235",
		0.00001234: "0.00001234",
		0:          "0.00",
		-37.63:     "-37.63",
	}
	for in, want := range cases {
		if got := formatPrice(in); got != want {
			t.Fatalf("formatPrice(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatChange(t *testing.T) {
	cases := map[float64]string{1.68: "+1.68%", -0.4: "-0.40%", 0: "0.00%"}
	for in, want := range cases {
		if got := formatChange(in); got != want {
			t.Fatalf("formatChange(%v) = %q, want %q", in, got, want)
		}
	}
	if (tickerQuote{Price: 5}).change() != 0 {
		t.Fatal("no base, no change")
	}
}

func TestRenderTickerColors(t *testing.T) {
	series := []float64{100, 101, 103, 102, 104}
	up := &tickerQuote{Price: 104, Base: 100, Series: series}
	if img := renderTicker("AAPL", up, false); !hasColor(img, styleLive) || hasColor(img, styleAlert) {
		t.Fatal("a rise is green")
	}
	down := &tickerQuote{Price: 96, Base: 100, Series: series}
	if img := renderTicker("AAPL", down, false); !hasColor(img, styleAlert) || hasColor(img, styleLive) {
		t.Fatal("a fall is red")
	}
	// A move too small to show as a percentage has no color.
	flat := &tickerQuote{Price: 100.001, Base: 100}
	if img := renderTicker("AAPL", flat, false); hasColor(img, styleLive) || hasColor(img, styleAlert) {
		t.Fatal("0.00% is neutral")
	}
	img := renderTicker("AAPL", up, true)
	if hasColor(img, styleLive) || hasColor(img, styleText) || hasColor(img, styleAccent) {
		t.Fatal("an old price is all grey")
	}
	if img := renderTicker("AAPL", nil, true); !hasColor(img, styleDim) || hasColor(img, styleText) {
		t.Fatal("no price yet renders dim")
	}
}

func TestSparklineScaleIncludesBase(t *testing.T) {
	rect := image.Rect(0, 0, 100, 50)
	lit := func(img *image.RGBA, y0, y1 int) int {
		n := 0
		for y := y0; y < y1; y++ {
			for x := 0; x < 100; x++ {
				if near(rgbaAt(img, x, y), styleLive) {
					n++
				}
			}
		}
		return n
	}
	// All of the series is above the base: nothing in the lower half.
	img := image.NewRGBA(rect)
	drawSparkline(img, rect, []float64{150, 160, 155, 200}, 100, styleLive)
	if lit(img, 30, 50) != 0 || lit(img, 0, 30) == 0 {
		t.Fatal("a series above base must sit above the middle")
	}
	// Flat and too short: no crash, and a flat line is drawn once.
	drawSparkline(image.NewRGBA(rect), rect, []float64{1}, 1, styleLive)
	flat := image.NewRGBA(rect)
	drawSparkline(flat, rect, []float64{5, 5, 5}, 5, styleLive)
	if lit(flat, 0, 50) == 0 {
		t.Fatal("a flat series is still a line")
	}
	drawSparkline(flat, rect, []float64{5, 7}, 0, styleLive) // no base known
}

// fakeTicker scripts the provider for a widget under test.
type fakeTicker struct {
	mu    sync.Mutex
	quote tickerQuote
	err   error
	calls int
}

func (f *fakeTicker) fetch(string) (tickerQuote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.quote, f.err
}

func (f *fakeTicker) set(q tickerQuote, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quote, f.err = q, err
}

func (f *fakeTicker) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newTestTicker(t *testing.T, f *fakeTicker, rec *recorder) *TickerWidget {
	t.Helper()
	old := tickerSources["yahoo"]
	tickerSources["yahoo"] = f.fetch
	t.Cleanup(func() { tickerSources["yahoo"] = old })
	w, err := NewTickerWidget("t", &config.Widget{Type: "ticker", Symbol: " aapl ", PollMS: 3600000}, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	waitFor(t, "first fetch", func() bool { return f.count() == 1 && !w.fetching.Load() })
	return w
}

func TestTickerWidgetKeepsOldPriceGreyOnFailure(t *testing.T) {
	f := &fakeTicker{quote: tickerQuote{Price: 110, Base: 100}}
	rec := &recorder{}
	w := newTestTicker(t, f, rec)
	if w.symbol != "AAPL" || w.label != "AAPL" {
		t.Fatalf("symbol %q label %q", w.symbol, w.label)
	}
	if !hasColor(readIcon(t, w.IconPath()), styleLive) {
		t.Fatal("the first reading shows")
	}
	f.set(tickerQuote{}, errors.New("offline"))
	w.refresh()
	img := readIcon(t, w.IconPath())
	if hasColor(img, styleLive) || !hasColor(img, styleDim) {
		t.Fatal("a failed update greys the old price")
	}
	f.set(tickerQuote{Price: 110, Base: 100}, nil)
	w.refresh()
	if !hasColor(readIcon(t, w.IconPath()), styleLive) {
		t.Fatal("a good reading brings the color back")
	}
}

func TestTickerWidgetRequests(t *testing.T) {
	f := &fakeTicker{quote: tickerQuote{Price: 1, Base: 1}}
	rec := &recorder{}
	w := newTestTicker(t, f, rec)

	// Coming back to the page soon after does not ask again; later does.
	tm := time.Now()
	w.mu.Lock()
	w.fetched = tm
	w.now = func() time.Time { return tm }
	w.mu.Unlock()
	w.resume()
	if f.count() != 1 {
		t.Fatalf("calls %d", f.count())
	}
	w.mu.Lock()
	w.now = func() time.Time { return tm.Add(2 * time.Hour) }
	w.mu.Unlock()
	w.resume()
	if f.count() != 2 {
		t.Fatalf("calls %d", f.count())
	}

	w.HandleGesture(ig.Tap)
	waitFor(t, "tap fetch", func() bool { return f.count() == 3 })
	waitFor(t, "tap fetch done", func() bool { return !w.fetching.Load() })

	// A request in flight is not doubled.
	w.fetching.Store(true)
	w.refresh()
	w.fetching.Store(false)
	if f.count() != 3 {
		t.Fatalf("calls %d", f.count())
	}
}

func TestNewTickerWidgetErrors(t *testing.T) {
	if _, err := NewTickerWidget("t", &config.Widget{Symbol: "X", Source: "nasdaq"}, nil); err == nil {
		t.Fatal("expected an unknown source error")
	}
	if _, err := NewTickerWidget("t", &config.Widget{Symbol: " "}, nil); err == nil {
		t.Fatal("expected a no symbol error")
	}
}

func useTickerServer(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	oldY, oldB := tickerYahooURL, tickerBinanceURL
	tickerYahooURL, tickerBinanceURL = srv.URL+"/chart/", srv.URL+"/api/"
	t.Cleanup(func() {
		tickerYahooURL, tickerBinanceURL = oldY, oldB
		srv.Close()
	})
}

func TestFetchYahooQuote(t *testing.T) {
	var path, agent string
	useTickerServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, agent = r.URL.EscapedPath(), r.UserAgent()
		_, _ = w.Write([]byte(`{"chart":{"result":[{"meta":{"regularMarketPrice":331.9,"chartPreviousClose":338.32},
			"indicators":{"quote":[{"close":[330.1,null,331.9]}]}}],"error":null}}`))
	})
	q, err := fetchYahooQuote("^GSPC")
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 331.9 || q.Base != 338.32 || len(q.Series) != 2 {
		t.Fatalf("%+v", q)
	}
	if !strings.HasSuffix(path, "/^GSPC") && !strings.HasSuffix(path, "/%5EGSPC") {
		t.Fatalf("path %q", path)
	}
	if agent != tickerUserAgent {
		t.Fatalf("Yahoo refuses requests without a User-Agent, sent %q", agent)
	}
}

func TestFetchYahooQuoteFailures(t *testing.T) {
	body, status := `{"chart":{"result":null}}`, 200
	useTickerServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	if _, err := fetchYahooQuote("X"); err == nil {
		t.Fatal("expected a no price error")
	}
	status = 404
	if _, err := fetchYahooQuote("X"); err == nil {
		t.Fatal("expected a status error")
	}
}

func TestFetchBinanceQuote(t *testing.T) {
	failChart := false
	useTickerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("symbol") != "BTCUSDT" {
			t.Errorf("symbol %q", r.URL.RawQuery)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/ticker/24hr"):
			_, _ = w.Write([]byte(`{"lastPrice":"78416.26","openPrice":"77073.32"}`))
		case failChart:
			w.WriteHeader(500)
		default:
			_, _ = w.Write([]byte(`[[1,"1","2","0","77100.5","9"],[2,"1","2","0","78000.0","9"],[3]]`))
		}
	})
	q, err := fetchBinanceQuote("BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 78416.26 || q.Base != 77073.32 || len(q.Series) != 2 || q.Series[1] != 78000 {
		t.Fatalf("%+v", q)
	}
	failChart = true
	if q, err := fetchBinanceQuote("BTCUSDT"); err != nil || q.Price == 0 || q.Series != nil {
		t.Fatalf("a missing chart still gives the price: %+v %v", q, err)
	}
}

func TestFetchBinanceQuoteFailures(t *testing.T) {
	useTickerServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"lastPrice":"oops"}`))
	})
	if _, err := fetchBinanceQuote("BTCUSDT"); err == nil {
		t.Fatal("expected a no price error")
	}
}
