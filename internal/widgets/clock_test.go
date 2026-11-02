package widgets

import (
	"image"
	"image/color"
	"sync"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

func TestClockLabel(t *testing.T) {
	cases := map[[2]string]string{
		{"", ""}:                     "",
		{"", "America/New_York"}:     "New York",
		{"", "UTC"}:                  "UTC",
		{" Office ", "Europe/Kyiv"}:  "Office",
		{"", "America/Indiana/Knox"}: "Knox",
	}
	for in, want := range cases {
		if got := clockLabel(in[0], in[1]); got != want {
			t.Fatalf("clockLabel(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestClockWidgetRedrawsOnlyOnNewMinute(t *testing.T) {
	var mu sync.Mutex
	tm := time.Date(2026, 11, 2, 9, 41, 10, 0, time.UTC)
	advance := func(d time.Duration) {
		mu.Lock()
		tm = tm.Add(d)
		mu.Unlock()
	}
	old := clockNow
	clockNow = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return tm
	}
	t.Cleanup(func() { clockNow = old })

	rec := &recorder{}
	w, err := NewClockWidget("c", &config.Widget{Type: "clock", Timezone: "Asia/Tokyo"}, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.tick()
	base := rec.count()
	w.tick()
	if rec.count() != base {
		t.Fatal("the same minute must not redraw")
	}
	advance(time.Minute)
	w.tick()
	if rec.count() != base+1 {
		t.Fatalf("a new minute must redraw once, notifications %d -> %d", base, rec.count())
	}
	if key := w.frameKey(clockNow()); key != "2026-11-02 18:42" {
		t.Fatalf("frame key %q must be in the widget's zone", key)
	}
}

func TestNewClockWidgetRejectsUnknownZone(t *testing.T) {
	if _, err := NewClockWidget("c", &config.Widget{Type: "clock", Timezone: "Mars/Olympus"}, nil); err == nil {
		t.Fatal("expected an error")
	}
}

func TestRenderClock(t *testing.T) {
	weekday := time.Date(2026, 11, 2, 18, 5, 0, 0, time.UTC)
	saturday := time.Date(2026, 11, 7, 18, 5, 0, 0, time.UTC)
	if hasColor(renderClock(weekday, "", false), styleAlert) || !hasColor(renderClock(saturday, "", false), styleAlert) {
		t.Fatal("the date is red on weekends only")
	}
	if !hasColor(renderClock(weekday, "Tokyo", false), styleAccent) || hasColor(renderClock(weekday, "", false), styleAccent) {
		t.Fatal("the city label is blue and drawn only when set")
	}
	if string(renderClock(weekday, "", true).Pix) == string(renderClock(weekday, "", false).Pix) {
		t.Fatal("12-hour format must differ")
	}
	// The colon is dimmer than the digits.
	img := renderClock(weekday, "", false)
	if !anyPixel(img, image.Rect(60, 64, 136, 150), func(c color.RGBA) bool { return near(c, styleDim) }) {
		t.Fatal("expected a dim colon")
	}
}
