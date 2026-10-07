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

func TestWideDashboardWidgetModesContextAndWeather(t *testing.T) {
	newOpenMeteo(t)
	rec := &recorder{}
	svc := &SpotifyService{}
	w, err := NewWideDashboardWidget("dash", &config.Widget{City: "Kyiv"}, svc, rec.notify)
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

	// Tap switches to the Spotify card, Spotify updates then redraw it.
	before := rec.count()
	w.HandleGesture(ig.Tap)
	if w.mode != dashboardModeSpotify || rec.count() == before {
		t.Fatal("tap must switch mode and redraw")
	}
	before = rec.count()
	w.spotifyUpdate(spotifySnapshot{Running: true, State: "playing", Playing: true, TrackName: "Song", Artist: "Band", PositionSec: 61, DurationMS: 180000})
	if rec.count() == before {
		t.Fatal("spotify update must redraw the Spotify card")
	}

	// In a folder the widget is the Back key: taps do nothing and the
	// context switch redraws silently (the page upload carries it).
	before = rec.count()
	w.SetContext(Context{FolderDepth: 1})
	w.SetContext(Context{FolderDepth: 1})
	w.HandleGesture(ig.Tap)
	w.spotifyUpdate(spotifySnapshot{Running: true, TrackName: "Other"})
	if rec.count() != before || w.mode != dashboardModeSpotify {
		t.Fatal("folder context must not notify or change mode")
	}
	w.HandleGesture(ig.Hold)
}

func TestWideDashboardWithoutCity(t *testing.T) {
	w, err := NewWideDashboardWidget("dash", &config.Widget{}, nil, nil)
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
	w, err := NewWideDashboardWidget("dash", &config.Widget{City: "Kyiv"}, nil, nil)
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
	if dashboardWeatherPoll(0) != dashboardWeatherPollMS || dashboardWeatherPoll(1) != dashboardWeatherMinPollMS || dashboardWeatherPoll(3600000) != time.Hour {
		t.Fatal("dashboardWeatherPoll")
	}
	for d := time.Sunday; d <= time.Saturday; d++ {
		if dashboardWeekday(d) == "" {
			t.Fatalf("no weekday name for %v", d)
		}
	}
	if formatSpotifyTimestamp(0) != "0:00" || formatSpotifyTimestamp(125) != "2:05" || formatSpotifyTimestamp(-5) != "0:00" {
		t.Fatalf("formatSpotifyTimestamp: %q %q", formatSpotifyTimestamp(0), formatSpotifyTimestamp(125))
	}
	s := spotifySnapshot{Running: true, Artist: "A", Album: "B", PositionSec: 30, DurationMS: 60000}
	if p := dashboardSpotifyProgress(s); p < 0.49 || p > 0.51 {
		t.Fatalf("progress = %v", p)
	}
	if dashboardSpotifyProgress(spotifySnapshot{PositionSec: 99, DurationMS: 1000}) != 1 || dashboardSpotifyProgress(spotifySnapshot{PositionSec: -1, DurationMS: 1000}) != 0 {
		t.Fatal("progress must clamp")
	}
	// Without a duration a fixed placeholder fill is drawn.
	if dashboardSpotifyProgress(spotifySnapshot{}) <= 0 || dashboardSpotifyProgress(spotifySnapshot{Running: true}) <= 0 {
		t.Fatal("placeholder progress")
	}
	if !strings.Contains(dashboardSpotifyArtistLine(s), "A") || dashboardSpotifyLeftStamp(s) == "" || dashboardSpotifyRightStamp(s) == "" {
		t.Fatal("spotify text helpers")
	}
	_ = dashboardSpotifyArtistLine(spotifySnapshot{})
	if rgb("#ff8000") != (color.RGBA{255, 128, 0, 255}) || rgb("bad").A != 0 || rgb("zzzzzz").A != 0 {
		t.Fatal("rgb")
	}
	if rectWH(1, 2, 3, 4) != image.Rect(1, 2, 4, 6) {
		t.Fatal("rectWH")
	}
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	k1 := dashboardFrameKey(now, 0, Context{}, dashboardWeatherState{}, spotifySnapshot{})
	k2 := dashboardFrameKey(now.Add(time.Minute), 0, Context{}, dashboardWeatherState{}, spotifySnapshot{})
	k3 := dashboardFrameKey(now, 0, Context{FolderDepth: 1}, dashboardWeatherState{}, spotifySnapshot{})
	k4 := dashboardFrameKey(now, dashboardModeSpotify, Context{}, dashboardWeatherState{}, spotifySnapshot{TrackName: "x"})
	if k1 == k2 || k1 == k3 || k1 == k4 {
		t.Fatal("frame keys must differ per minute, context and mode")
	}

	// Rendering paths, including artwork and the fallback card.
	img := image.NewRGBA(image.Rect(0, 0, 458, 196))
	temp := 3.0
	renderDashboardClock(img, now, dashboardWeatherState{Ready: true, TempC: &temp, CityName: "Kyiv"})
	renderDashboardClock(img, now, dashboardWeatherState{Error: "Weather unavailable"})
	renderDashboardSpotify(img, spotifySnapshot{})
	renderDashboardSpotify(img, spotifySnapshot{Running: true, Playing: true, TrackName: strings.Repeat("Long ", 20), Artwork: image.NewRGBA(image.Rect(0, 0, 64, 64))})
	renderDashboardBack(img)
}
