package main

import (
	"testing"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

func TestNavigatorEnterDirectoryAndBack(t *testing.T) {
	action := &config.Action{Type: "shell", Cmd: "echo hi"}
	dir := &config.Directory{
		Buttons: []config.Button{
			{At: [2]int{0, 0}, Label: "Nested", Action: action},
		},
	}

	nav := newNavigator([]config.Button{
		{At: [2]int{1, 0}, Label: "Folder", Directory: dir},
	})

	if changed, gotAction := nav.handleEvent(actionIndex([2]int{1, 0}), true); !changed || gotAction != nil {
		t.Fatalf("expected directory press to change page without action, got changed=%v action=%v", changed, gotAction)
	}

	buttons := nav.currentButtons()
	if len(buttons) != 2 {
		t.Fatalf("expected nested page plus back button, got %d buttons", len(buttons))
	}

	if changed, gotAction := nav.handleEvent(actionIndex([2]int{0, 0}), true); changed || gotAction != action {
		t.Fatalf("expected nested action, got changed=%v action=%v", changed, gotAction)
	}

	if changed, gotAction := nav.handleEvent(actionIndex(backButton.At), true); !changed || gotAction != nil {
		t.Fatalf("expected back button to change page, got changed=%v action=%v", changed, gotAction)
	}

	if len(nav.currentButtons()) != 1 {
		t.Fatalf("expected to return to root page")
	}
}

func TestNavigatorIgnoresUnknownIndex(t *testing.T) {
	nav := newNavigator([]config.Button{
		{At: [2]int{0, 0}, Label: "A"},
	})

	if changed, action := nav.handleEvent(99, true); changed || action != nil {
		t.Fatalf("expected unknown button to do nothing, got changed=%v action=%v", changed, action)
	}
}

func actionIndex(at [2]int) int {
	return at[1]*5 + at[0]
}
