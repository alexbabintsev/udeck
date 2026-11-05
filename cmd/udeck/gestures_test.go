package main

import (
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
)

func TestGestureManagerTap(t *testing.T) {
	gm := newGestureManager()
	btn := config.Button{
		RuntimeID: "buttons[0]",
		On: &config.Handlers{
			Tap: &config.GestureBinding{Type: "shell", Cmd: "echo tap"},
		},
	}
	now := time.Unix(100, 0)
	if got := gm.HandleEvent(&btn, true, now); got != nil {
		t.Fatalf("expected no action on press, got %#v", got)
	}
	evt := gm.HandleEvent(&btn, false, now.Add(50*time.Millisecond))
	got, _ := resolveBindingEvent(evt, nil)
	if got == nil || got.Cmd != "echo tap" {
		t.Fatalf("expected tap binding, got evt=%#v binding=%#v", evt, got)
	}
}

func TestGestureManagerDoubleTap(t *testing.T) {
	gm := newGestureManager()
	btn := config.Button{
		RuntimeID: "buttons[0]",
		On: &config.Handlers{
			Tap:       &config.GestureBinding{Type: "shell", Cmd: "echo tap"},
			DoubleTap: &config.GestureBinding{Type: "shell", Cmd: "echo double"},
		},
	}
	now := time.Unix(100, 0)
	_ = gm.HandleEvent(&btn, true, now)
	_ = gm.HandleEvent(&btn, false, now.Add(30*time.Millisecond))
	_ = gm.HandleEvent(&btn, true, now.Add(120*time.Millisecond))
	evt := gm.HandleEvent(&btn, false, now.Add(150*time.Millisecond))
	got, _ := resolveBindingEvent(evt, nil)
	if got == nil || got.Cmd != "echo double" {
		t.Fatalf("expected double tap binding, got evt=%#v binding=%#v", evt, got)
	}
}

func TestGestureManagerHoldAndRepeat(t *testing.T) {
	gm := newGestureManager()
	btn := config.Button{
		RuntimeID: "buttons[0]",
		On: &config.Handlers{
			Hold:          &config.GestureBinding{Type: "shell", Cmd: "echo long"},
			Repeat:        &config.GestureBinding{Type: "shell", Cmd: "echo repeat"},
			HoldMS:        300,
			RepeatEveryMS: 100,
		},
	}
	now := time.Unix(100, 0)
	_ = gm.HandleEvent(&btn, true, now)
	evt := gm.Tick([]config.Button{btn}, now.Add(320*time.Millisecond))
	got, _ := resolveBindingEvent(evt, nil)
	if got == nil || got.Cmd != "echo long" {
		t.Fatalf("expected hold binding, got evt=%#v binding=%#v", evt, got)
	}
	evt = gm.Tick([]config.Button{btn}, now.Add(430*time.Millisecond))
	got, _ = resolveBindingEvent(evt, nil)
	if got == nil || got.Cmd != "echo repeat" {
		t.Fatalf("expected repeat binding, got evt=%#v binding=%#v", evt, got)
	}
}

func TestGestureManagerDelayedTapAfterDoubleWindow(t *testing.T) {
	gm := newGestureManager()
	btn := config.Button{
		RuntimeID: "buttons[0]",
		On: &config.Handlers{
			Tap:         &config.GestureBinding{Type: "shell", Cmd: "echo tap"},
			DoubleTap:   &config.GestureBinding{Type: "shell", Cmd: "echo double"},
			DoubleTapMS: 200,
		},
	}
	now := time.Unix(100, 0)
	_ = gm.HandleEvent(&btn, true, now)
	_ = gm.HandleEvent(&btn, false, now.Add(20*time.Millisecond))
	evt := gm.Tick([]config.Button{btn}, now.Add(230*time.Millisecond))
	got, _ := resolveBindingEvent(evt, nil)
	if got == nil || got.Cmd != "echo tap" {
		t.Fatalf("expected delayed tap binding, got evt=%#v binding=%#v", evt, got)
	}
}
