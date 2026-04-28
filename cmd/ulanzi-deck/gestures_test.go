package main

import (
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

func TestGestureManagerTap(t *testing.T) {
	gm := newGestureManager()
	btn := config.Button{
		RuntimeID: "buttons[0]",
		On: &config.Handlers{
			Tap: &config.Action{Type: "shell", Cmd: "echo tap"},
		},
	}
	now := time.Unix(100, 0)
	if got := gm.HandleEvent(&btn, true, now); got != nil {
		t.Fatalf("expected no action on press, got %#v", got)
	}
	got := gm.HandleEvent(&btn, false, now.Add(50*time.Millisecond))
	if got != btn.On.Tap {
		t.Fatalf("expected tap action, got %#v", got)
	}
}

func TestGestureManagerDoubleTap(t *testing.T) {
	gm := newGestureManager()
	btn := config.Button{
		RuntimeID: "buttons[0]",
		On: &config.Handlers{
			Tap:       &config.Action{Type: "shell", Cmd: "echo tap"},
			DoubleTap: &config.Action{Type: "shell", Cmd: "echo double"},
		},
	}
	now := time.Unix(100, 0)
	_ = gm.HandleEvent(&btn, true, now)
	_ = gm.HandleEvent(&btn, false, now.Add(30*time.Millisecond))
	_ = gm.HandleEvent(&btn, true, now.Add(120*time.Millisecond))
	got := gm.HandleEvent(&btn, false, now.Add(150*time.Millisecond))
	if got != btn.On.DoubleTap {
		t.Fatalf("expected double tap action, got %#v", got)
	}
}

func TestGestureManagerLongPressAndRepeat(t *testing.T) {
	gm := newGestureManager()
	btn := config.Button{
		RuntimeID: "buttons[0]",
		On: &config.Handlers{
			LongPress:     &config.Action{Type: "shell", Cmd: "echo long"},
			Repeat:        &config.Action{Type: "shell", Cmd: "echo repeat"},
			LongPressMS:   300,
			RepeatEveryMS: 100,
		},
	}
	now := time.Unix(100, 0)
	_ = gm.HandleEvent(&btn, true, now)
	got := gm.Tick([]config.Button{btn}, now.Add(320*time.Millisecond))
	if got != btn.On.LongPress {
		t.Fatalf("expected long press action, got %#v", got)
	}
	got = gm.Tick([]config.Button{btn}, now.Add(430*time.Millisecond))
	if got != btn.On.Repeat {
		t.Fatalf("expected repeat action, got %#v", got)
	}
}

func TestGestureManagerDelayedTapAfterDoubleWindow(t *testing.T) {
	gm := newGestureManager()
	btn := config.Button{
		RuntimeID: "buttons[0]",
		On: &config.Handlers{
			Tap:         &config.Action{Type: "shell", Cmd: "echo tap"},
			DoubleTap:   &config.Action{Type: "shell", Cmd: "echo double"},
			DoubleTapMS: 200,
		},
	}
	now := time.Unix(100, 0)
	_ = gm.HandleEvent(&btn, true, now)
	_ = gm.HandleEvent(&btn, false, now.Add(20*time.Millisecond))
	got := gm.Tick([]config.Button{btn}, now.Add(230*time.Millisecond))
	if got != btn.On.Tap {
		t.Fatalf("expected delayed tap action, got %#v", got)
	}
}
