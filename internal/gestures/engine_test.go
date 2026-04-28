package gestures

import (
	"testing"
	"time"
)

func TestEngineDoubleTapFiresOnSecondRelease(t *testing.T) {
	e := New()
	opts := Options{
		HasTap:         true,
		HasDoubleTap:   true,
		DoubleTapDelay: 300 * time.Millisecond,
	}
	now := time.Unix(100, 0)
	if got := e.HandleEvent("a", opts, true, now); got != None {
		t.Fatalf("expected no gesture on first press, got %q", got)
	}
	if got := e.HandleEvent("a", opts, false, now.Add(20*time.Millisecond)); got != None {
		t.Fatalf("expected no gesture on first release, got %q", got)
	}
	if got := e.HandleEvent("a", opts, true, now.Add(120*time.Millisecond)); got != None {
		t.Fatalf("expected no gesture on second press, got %q", got)
	}
	if got := e.HandleEvent("a", opts, false, now.Add(150*time.Millisecond)); got != DoubleTap {
		t.Fatalf("expected double tap on second release, got %q", got)
	}
}

func TestEngineLongPressFiresDuringHold(t *testing.T) {
	e := New()
	opts := Options{
		HasTap:         true,
		HasLongPress:   true,
		HasRepeat:      true,
		LongPressDelay: 300 * time.Millisecond,
		RepeatEvery:    100 * time.Millisecond,
	}
	now := time.Unix(100, 0)
	_ = e.HandleEvent("a", opts, true, now)
	id, kind := e.Tick([]Binding{{ID: "a", Options: opts}}, now.Add(320*time.Millisecond))
	if id != "a" || kind != LongPress {
		t.Fatalf("expected long press during hold, got id=%q kind=%q", id, kind)
	}
	id, kind = e.Tick([]Binding{{ID: "a", Options: opts}}, now.Add(430*time.Millisecond))
	if id != "a" || kind != Repeat {
		t.Fatalf("expected repeat after long press, got id=%q kind=%q", id, kind)
	}
}
