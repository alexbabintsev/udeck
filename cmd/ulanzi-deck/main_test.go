package main

import (
	"testing"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

func TestNavigatorEnterFolderAndBack(t *testing.T) {
	dir := &config.Folder{
		Buttons: []config.Button{
			{At: [2]int{0, 0}, Label: "Nested", On: &config.Handlers{Tap: &config.GestureBinding{Type: "shell", Cmd: "echo hi"}}},
		},
	}

	nav := newNavigator([]config.Button{
		{At: [2]int{1, 0}, Label: "Folder", Folder: dir},
	})

	if changed := nav.handleEvent(actionIndex([2]int{1, 0}), true); !changed {
		t.Fatalf("expected folder press to change page")
	}

	buttons := nav.currentButtons()
	if len(buttons) != 2 {
		t.Fatalf("expected nested page plus back button, got %d buttons", len(buttons))
	}

	if changed := nav.handleEvent(actionIndex([2]int{0, 0}), true); changed {
		t.Fatalf("expected nested action button to stay on same page")
	}

	if changed := nav.handleEvent(actionIndex(backButton.At), true); !changed {
		t.Fatalf("expected back button to change page")
	}

	if len(nav.currentButtons()) != 1 {
		t.Fatalf("expected to return to root page")
	}
}

func TestNavigatorIgnoresUnknownIndex(t *testing.T) {
	nav := newNavigator([]config.Button{
		{At: [2]int{0, 0}, Label: "A"},
	})

	if changed := nav.handleEvent(99, true); changed {
		t.Fatalf("expected unknown button to do nothing, got changed=%v", changed)
	}
}

func actionIndex(at [2]int) int {
	return at[1]*5 + at[0]
}
