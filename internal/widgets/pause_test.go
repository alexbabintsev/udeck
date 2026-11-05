package widgets

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestWidgetBasePauseSkipsTicks(t *testing.T) {
	b := testBase(t, nil)
	if !b.isActive() {
		t.Fatal("new widgets start active")
	}
	if b.setActive(true) {
		t.Fatal("already active: not a resume")
	}
	if b.setActive(false) || b.isActive() {
		t.Fatal("pause")
	}
	var ticks atomic.Int32
	b.every(time.Millisecond, func(time.Time) { ticks.Add(1) })
	time.Sleep(30 * time.Millisecond)
	if ticks.Load() != 0 {
		t.Fatal("paused widget must not tick")
	}
	if !b.setActive(true) {
		t.Fatal("expected resume")
	}
	waitFor(t, "ticks after resume", func() bool { return ticks.Load() > 0 })
	b.Close()
	if b.isActive() {
		t.Fatal("closed widget is not active")
	}
}

type activeSub struct{ active bool }

func (s *activeSub) spotifyUpdate(spotifySnapshot) {}
func (s *activeSub) isActive() bool                { return s.active }

func TestSpotifyServiceSkipsPollingWithoutVisibleWidgets(t *testing.T) {
	var state atomic.Value
	state.Store("0|||||||")
	calls := fakeOSA(t, &state)
	s, err := NewSpotifyService(1)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// The initial explicit refresh always runs.
	waitFor(t, "initial refresh", func() bool { return calls.Load() >= 1 })
	time.Sleep(30 * time.Millisecond)
	idle := calls.Load()
	if idle > 1 {
		t.Fatalf("polled %d times with no subscribers", idle)
	}

	sub := &activeSub{active: false}
	s.Subscribe(sub)
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != idle {
		t.Fatal("hidden subscriber must not enable polling")
	}
	s.mu.Lock()
	sub.active = true
	s.mu.Unlock()
	waitFor(t, "polling with a visible widget", func() bool { return calls.Load() > idle+2 })

	// Resume of a Spotify widget asks the service for fresh state.
	w := &spotifyBaseWidget{widgetBase: testBase(t, nil), service: s}
	before := calls.Load()
	w.resume()
	waitFor(t, "resume refresh", func() bool { return calls.Load() > before })
}
