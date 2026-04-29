package main

import (
	"testing"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
)

func TestToggleManagerCyclesTapStates(t *testing.T) {
	cfg := &config.Config{
		Buttons: []config.Button{
			{
				At:        [2]int{0, 0},
				Label:     "A",
				Icon:      "one.png",
				RuntimeID: "buttons[0]",
				On: &config.Handlers{
					Tap: &config.GestureBinding{
						Toggle: []config.ToggleStep{
							{Label: "One", Icon: "one.png", IconColor: "FF0000", Action: config.Action{Type: "shell", Cmd: "echo 1"}},
							{Label: "Two", Icon: "two.png", IconColor: "00FF00", Action: config.Action{Type: "shell", Cmd: "echo 2"}},
							{Label: "Three", Icon: "three.png", Action: config.Action{Type: "shell", Cmd: "echo 3"}},
						},
					},
				},
			},
		},
	}

	m := newToggleManager(cfg)
	buttons := m.ResolveButtons(cfg.Buttons)
	if buttons[0].Label != "One" || buttons[0].Icon != "one.png" || buttons[0].IconColor != "FF0000" {
		t.Fatalf("expected first toggle visuals, got label=%q icon=%q color=%q", buttons[0].Label, buttons[0].Icon, buttons[0].IconColor)
	}

	action, changed := m.Handle("buttons[0]", ig.Tap)
	if !changed || action == nil || action.Cmd != "echo 1" {
		t.Fatalf("expected first toggle action and visual change, got action=%#v changed=%v", action, changed)
	}
	buttons = m.ResolveButtons(cfg.Buttons)
	if buttons[0].Label != "Two" || buttons[0].Icon != "two.png" || buttons[0].IconColor != "00FF00" {
		t.Fatalf("expected second toggle visuals, got label=%q icon=%q color=%q", buttons[0].Label, buttons[0].Icon, buttons[0].IconColor)
	}

	_, _ = m.Handle("buttons[0]", ig.Tap)
	buttons = m.ResolveButtons(cfg.Buttons)
	if buttons[0].Label != "Three" || buttons[0].Icon != "three.png" {
		t.Fatalf("expected third toggle visuals, got label=%q icon=%q", buttons[0].Label, buttons[0].Icon)
	}

	_, _ = m.Handle("buttons[0]", ig.Tap)
	buttons = m.ResolveButtons(cfg.Buttons)
	if buttons[0].Label != "One" || buttons[0].Icon != "one.png" {
		t.Fatalf("expected toggle wraparound visuals, got label=%q icon=%q", buttons[0].Label, buttons[0].Icon)
	}
}

func TestToggleManagerLeavesRegularButtonsUntouched(t *testing.T) {
	cfg := &config.Config{
		Buttons: []config.Button{
			{
				At:        [2]int{0, 0},
				Label:     "Plain",
				RuntimeID: "buttons[0]",
				On: &config.Handlers{
					Tap: &config.GestureBinding{Type: "shell", Cmd: "echo plain"},
				},
			},
		},
	}

	m := newToggleManager(cfg)
	action, changed := m.Handle("buttons[0]", ig.Tap)
	if action != nil || changed {
		t.Fatalf("expected no toggle handling, got action=%#v changed=%v", action, changed)
	}
	buttons := m.ResolveButtons(cfg.Buttons)
	if buttons[0].Label != "Plain" {
		t.Fatalf("expected regular button label to stay unchanged, got %q", buttons[0].Label)
	}
}
