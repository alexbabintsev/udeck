package widgets

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
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

// Regression: a freqtrade page opened once kept polling its (unreachable)
// API forever after the user left it.
func TestManagerPausesOffscreenWidgetsAndRefreshesOnReturn(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	ft := config.Button{At: [2]int{0, 0}, RuntimeID: "folder.ft", Widget: &config.Widget{
		Type: "freqtrade", DSNs: "u:p@" + srv.URL, PollMS: 3000,
	}}
	other := config.Button{At: [2]int{1, 0}, RuntimeID: "root.label", Label: "x"}
	m, err := NewManager(&config.Config{Buttons: []config.Button{
		{At: [2]int{0, 0}, RuntimeID: "root.folder", Folder: &config.Folder{Buttons: []config.Button{ft}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Open the folder: the widget is created and fetches once.
	if _, err := m.ResolveButtons([]config.Button{ft}); err != nil {
		t.Fatal(err)
	}
	w := m.items["folder.ft"].(*FreqtradeWidget)
	waitFor(t, "initial fetch", func() bool { return hits.Load() > 0 })

	// Leave the folder: the widget is paused and stops polling.
	if _, err := m.ResolveButtons([]config.Button{other}); err != nil {
		t.Fatal(err)
	}
	if w.isActive() {
		t.Fatal("off-screen widget must be paused")
	}
	time.Sleep(20 * time.Millisecond)
	paused := hits.Load()
	w.every(time.Millisecond, func(time.Time) { w.refresh() })
	time.Sleep(30 * time.Millisecond)
	if hits.Load() != paused {
		t.Fatal("paused widget kept polling")
	}

	// Come back: resumed and refreshed right away.
	if _, err := m.ResolveButtons([]config.Button{ft}); err != nil {
		t.Fatal(err)
	}
	if !w.isActive() {
		t.Fatal("visible widget must be active")
	}
	waitFor(t, "refresh on return", func() bool { return hits.Load() > paused })
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
