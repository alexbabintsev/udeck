package widgets

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ig "github.com/alexbabintsev/udeck/internal/gestures"
)

// recordOSA captures osascript scripts and answers state queries with
// *state.
func recordOSA(t *testing.T, state *atomic.Value) func() []string {
	t.Helper()
	var mu sync.Mutex
	var scripts []string
	old := execCommandContext
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		mu.Lock()
		scripts = append(scripts, strings.Join(args, " "))
		mu.Unlock()
		return exec.CommandContext(ctx, "printf", "%s", state.Load().(string))
	}
	t.Cleanup(func() { execCommandContext = old })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), scripts...)
	}
}

func hasScript(scripts []string, part string) bool {
	for _, s := range scripts {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

func TestSpotifyPlayPauseWidgetTapAndHold(t *testing.T) {
	var state atomic.Value
	state.Store("")
	scripts := recordOSA(t, &state)
	w := &SpotifyPlayPauseWidget{spotifyBaseWidget: &spotifyBaseWidget{widgetBase: testBase(t, nil), service: &SpotifyService{}}}

	w.HandleGesture(ig.Tap)
	w.HandleGesture(ig.Hold)
	w.service.bg.Wait()
	if !hasScript(scripts(), `tell application "Spotify" to playpause`) || !hasScript(scripts(), `tell application "Spotify" to next track`) {
		t.Fatalf("scripts = %v", scripts())
	}
}

const (
	stalePaused = "1||paused||spotify:track:a||Song||Album||Artist||10||200000"
	nowPlaying  = "1||playing||spotify:track:a||Song||Album||Artist||10||200000"
)

// The icon must flip on tap, not when Spotify gets around to reporting
// the new state (it keeps returning the old one for a moment).
func TestSpotifyPlayPauseIsOptimisticAndIgnoresStaleReads(t *testing.T) {
	var state atomic.Value
	state.Store(stalePaused)
	recordOSA(t, &state)

	s := &SpotifyService{snapshot: spotifySnapshot{Running: true, State: "paused", TrackID: "spotify:track:a"}}
	var mu sync.Mutex
	var seen []bool
	s.Subscribe(subscriberFunc(func(snap spotifySnapshot) {
		mu.Lock()
		seen = append(seen, snap.Playing)
		mu.Unlock()
	}))

	s.PlayPause()
	defer s.bg.Wait()
	mu.Lock()
	immediate := seen[len(seen)-1]
	mu.Unlock()
	if !immediate {
		t.Fatal("subscribers must see playing right after the tap")
	}

	// Spotify still answers "paused": the optimistic state wins.
	s.refresh()
	if !s.snapshot.Playing || s.snapshot.State != "playing" {
		t.Fatalf("stale read reverted the state: %+v", s.snapshot)
	}

	// Spotify confirms: expectation cleared.
	state.Store(nowPlaying)
	s.refresh()
	if s.expectPlaying != nil || !s.snapshot.Playing {
		t.Fatal("confirmed state must clear the expectation")
	}
}

func TestSpotifyPlayPauseRevertsWhenSpotifyIgnoresIt(t *testing.T) {
	var state atomic.Value
	state.Store(stalePaused)
	recordOSA(t, &state)
	s := &SpotifyService{snapshot: spotifySnapshot{Running: true, State: "paused", TrackID: "spotify:track:a"}}

	s.PlayPause()
	s.bg.Wait()
	// Optimistic window over and Spotify still paused: trust Spotify.
	s.mu.Lock()
	s.expectUntil = time.Now().Add(-time.Millisecond)
	s.mu.Unlock()
	s.refresh()
	if s.snapshot.Playing || s.expectPlaying != nil {
		t.Fatalf("expected revert to paused, got %+v", s.snapshot)
	}
}

func TestSpotifyPlayPauseCommandFailureClearsExpectation(t *testing.T) {
	old := execCommandContext
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "false")
	}
	defer func() { execCommandContext = old }()
	s := &SpotifyService{}
	s.PlayPause()
	waitFor(t, "expectation cleared", func() bool {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.expectPlaying == nil
	})
	s.NextTrack()
	s.bg.Wait()
}
