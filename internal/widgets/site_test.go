//go:build personal

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

	"github.com/alexbabintsev/udeck/internal/config"
	ig "github.com/alexbabintsev/udeck/internal/gestures"
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
		fmt.Fprint(w, `{"online": {"visitors": 7}}`)
	})
	api := newSiteAPI(&config.Widget{URL: srv.URL + "/ ", Token: " secret "})
	var out siteStats
	if err := api.get("/x", &out); err != nil || out.Online.Visitors != 7 {
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
		fmt.Fprint(w, `{"online": `+strings.Repeat("1", maxHTTPBodyBytes+10)+`}`)
	})
	var out siteStats
	if err := (siteAPI{baseURL: srv.URL}).get("/", &out); err == nil {
		t.Fatal("expected error for a body larger than the limit")
	}
}

func TestSitePoll(t *testing.T) {
	if sitePoll(0) != siteDefaultPoll || sitePoll(1000) != siteMinPoll || sitePoll(120000) != 2*time.Minute {
		t.Fatal("sitePoll")
	}
}

func TestSiteWidgetFetchesOnceAndRendersOnChange(t *testing.T) {
	var visitors, requests atomic.Int32
	visitors.Store(12)
	srv := siteServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != sitePath {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		fmt.Fprintf(w, `{"online": {"visitors": %d, "views": 20}, "daily": {"views": 1234, "visitors": 300, "change": -12.4}}`, visitors.Load())
	})
	rec := &recorder{}
	w, err := NewSiteWidget("site", &config.Widget{URL: srv.URL}, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitFor(t, "first fetch", func() bool { return w.frameKey() == "12|1.2k|12" })
	waitFor(t, "notification", func() bool { return rec.count() == 1 })
	if requests.Load() != 1 {
		t.Fatalf("one fetch must feed both rows, got %d requests", requests.Load())
	}

	// Same values: no new notification.
	w.refresh()
	if rec.count() != 1 {
		t.Fatal("unchanged values must not notify")
	}
	visitors.Store(3000)
	w.HandleGesture(ig.Tap)
	waitFor(t, "tap refresh", func() bool { return rec.count() == 2 })
	w.HandleGesture(ig.Hold) // ignored
}

func TestSiteWidgetKeepsValuesOnError(t *testing.T) {
	var fail atomic.Bool
	srv := siteServer(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `{"online": {"visitors": 5}, "daily": {"views": 40, "change": null}}`)
	})
	w, err := NewSiteWidget("site", &config.Widget{URL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitFor(t, "first fetch", func() bool { return w.frameKey() == "5|40|-" })
	fail.Store(true)
	w.refresh()
	if w.frameKey() != "5|40|-" {
		t.Fatal("failed fetch must keep the last values")
	}
}

func TestSiteFormatting(t *testing.T) {
	for in, want := range map[int]string{5: "5", 1500: "1.5k", 15000: "15k", 2500000: "2.5M"} {
		if got := formatSiteCount(in); got != want {
			t.Fatalf("formatSiteCount(%d) = %q, want %q", in, got, want)
		}
	}
	// The badge's color carries the sign: the number alone, no "%".
	for in, want := range map[float64]string{12.4: "12", -3.6: "4", -0.2: "0", 140: "140"} {
		if got := formatSiteChange(in); got != want {
			t.Fatalf("formatSiteChange(%v) = %q, want %q", in, got, want)
		}
	}
	if siteFrameKey(nil) == siteFrameKey(&siteStats{}) {
		t.Fatal("frame key must reflect data")
	}
}

func TestSiteTitle(t *testing.T) {
	for _, c := range []struct{ title, url, want string }{
		{" laravel-ua ", "https://laravelukraine.com", "laravel-ua"},
		{"", "https://xmlvalid.com", "xmlvalid"},
		{"", "https://www.example.co:8443/x", "example"},
		{"", "http://localhost:8000", "localhost"},
		{"", "http://\x00bad", ""},
	} {
		if got := siteTitle(c.title, c.url); got != c.want {
			t.Fatalf("siteTitle(%q, %q) = %q, want %q", c.title, c.url, got, c.want)
		}
	}
}

func TestSiteCardRendersBadgeByColor(t *testing.T) {
	stats := func(change *float64) *siteStats {
		s := &siteStats{}
		s.Online.Visitors = 7
		s.Daily.Views = 1234
		s.Daily.Change = change
		return s
	}
	rise, fall := 12.0, -35.0
	// A pixel inside the badge, at its right edge and vertical center.
	badge := func(img *image.RGBA) (r, g, b uint8) {
		innerH := img.Bounds().Dy() - siteBodyTop - siteBottom
		topH := innerH * 11 / 20
		y := siteBodyTop + topH + (innerH-topH)/2
		c := rgbaAt(img, img.Bounds().Dx()-siteMargin-3, y)
		return c.R, c.G, c.B
	}

	img := image.NewRGBA(image.Rect(0, 0, 196, 196))
	renderSiteCard(img, "xmlvalid", stats(&rise))
	if r, g, b := badge(img); [3]uint8{r, g, b} != [3]uint8{siteRise.R, siteRise.G, siteRise.B} {
		t.Fatalf("rise badge = %d,%d,%d", r, g, b)
	}
	renderSiteCard(img, "xmlvalid", stats(&fall))
	if r, g, b := badge(img); [3]uint8{r, g, b} != [3]uint8{siteFall.R, siteFall.G, siteFall.B} {
		t.Fatalf("fall badge = %d,%d,%d", r, g, b)
	}
	renderSiteCard(img, "xmlvalid", stats(nil))
	if r, g, b := badge(img); r+g+b != 0 {
		t.Fatalf("no change must draw no badge, got %d,%d,%d", r, g, b)
	}
	renderSiteCard(img, "", nil)
	big := stats(&rise)
	big.Online.Visitors = 123456
	renderSiteCard(img, "laravelukraine", big)
}
