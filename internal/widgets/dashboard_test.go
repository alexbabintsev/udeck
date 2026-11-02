package widgets

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
)

func TestWideDashboardWidgetContextAndWeather(t *testing.T) {
	newOpenMeteo(t)
	rec := &recorder{}
	w, err := NewWideDashboardWidget("dash", &config.Widget{City: "Kyiv"}, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitFor(t, "weather", func() bool {
		w.mu.RLock()
		defer w.mu.RUnlock()
		return w.weather.Ready && !w.fetching
	})
	if w.weather.CityName != "Kyiv, Ukraine" {
		t.Fatalf("city = %q", w.weather.CityName)
	}
	// The fetched weather is published right away, not on the next
	// minute change.
	waitFor(t, "weather redraw", func() bool { return rec.count() > 0 })

	// The dashboard is display-only: a tap must not redraw anything.
	frameKey := func() string {
		w.mu.RLock()
		defer w.mu.RUnlock()
		return w.lastFrameKey
	}
	key := frameKey()
	before := rec.count()
	w.HandleGesture(ig.Tap)
	if rec.count() != before || frameKey() != key {
		t.Fatal("tap must not change the dashboard")
	}

	// In a folder the widget is the Back key: the context switch redraws
	// silently (the page upload carries it).
	w.SetContext(Context{FolderDepth: 1})
	w.SetContext(Context{FolderDepth: 1})
	if rec.count() != before || !strings.HasPrefix(frameKey(), "back:") {
		t.Fatal("folder context must redraw as Back without notifying")
	}
}

func TestWideDashboardWithoutCity(t *testing.T) {
	w, err := NewWideDashboardWidget("dash", &config.Widget{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitFor(t, "weather state", func() bool {
		w.mu.RLock()
		defer w.mu.RUnlock()
		return w.weather.Error == "Set city"
	})
	w.tick(time.Now().Add(time.Hour))
}

func TestWideDashboardWeatherFailure(t *testing.T) {
	om := newOpenMeteo(t)
	om.fail.Store(true)
	w, err := NewWideDashboardWidget("dash", &config.Widget{City: "Kyiv"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitFor(t, "error state", func() bool {
		w.mu.RLock()
		defer w.mu.RUnlock()
		return w.weather.Error == "Weather unavailable" && !w.fetching
	})
	// A second refresh while one is in flight is a no-op.
	w.mu.Lock()
	w.fetching = true
	w.mu.Unlock()
	w.refreshWeather()
	w.mu.Lock()
	w.fetching = false
	w.mu.Unlock()
}

func TestDashboardHelpers(t *testing.T) {
	if dashboardWeatherPoll(0) != dashboardWeatherDefaultPoll || dashboardWeatherPoll(1) != dashboardWeatherMinPoll || dashboardWeatherPoll(3600000) != time.Hour {
		t.Fatal("dashboardWeatherPoll")
	}
	for d := time.Sunday; d <= time.Saturday; d++ {
		if dashboardWeekday(d) == "" {
			t.Fatalf("no weekday name for %v", d)
		}
	}
	if rgb("#ff8000") != (color.RGBA{255, 128, 0, 255}) || rgb("bad").A != 0 || rgb("zzzzzz").A != 0 {
		t.Fatal("rgb")
	}
	if rectWH(1, 2, 3, 4) != image.Rect(1, 2, 4, 6) {
		t.Fatal("rectWH")
	}
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	temp := 3.0
	k1 := dashboardFrameKey(now, Context{}, dashboardWeatherState{})
	k2 := dashboardFrameKey(now.Add(time.Minute), Context{}, dashboardWeatherState{})
	k3 := dashboardFrameKey(now, Context{FolderDepth: 1}, dashboardWeatherState{})
	k4 := dashboardFrameKey(now, Context{}, dashboardWeatherState{Ready: true, TempC: &temp})
	k5 := dashboardFrameKey(now, Context{}, dashboardWeatherState{Ready: true, TempC: &temp, Code: 61})
	if k1 == k2 || k1 == k3 || k1 == k4 || k4 == k5 {
		t.Fatal("frame keys must differ per minute, context and weather")
	}

	// Rendering paths.
	img := image.NewRGBA(image.Rect(0, 0, 458, 196))
	renderDashboardClock(img, now, dashboardWeatherState{Ready: true, TempC: &temp, CityName: "Kyiv"})
	renderDashboardClock(img, now, dashboardWeatherState{Error: "Weather unavailable"})
	renderDashboardBack(img)
}
