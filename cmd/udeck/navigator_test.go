package main

import (
	"testing"

	"github.com/alexbabintsev/udeck/internal/config"
)

func TestNavigatorEnterFolderAndBack(t *testing.T) {
	dir := &config.Folder{
		Buttons: []config.Button{
			{At: [2]int{0, 0}, Label: "Nested", On: &config.Handlers{Tap: &config.GestureBinding{Type: "shell", Cmd: "echo hi"}}},
		},
	}

	nav := newNavigator([]config.Page{{
		Buttons: []config.Button{
			{At: [2]int{1, 0}, Label: "Folder", Folder: dir},
		},
	}})

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

func TestNavigatorUsesDashboardWidgetAsContextualBackButton(t *testing.T) {
	dir := &config.Folder{
		Buttons: []config.Button{
			{At: [2]int{0, 0}, Label: "Nested"},
		},
	}

	nav := newNavigator([]config.Page{{
		Buttons: []config.Button{
			{At: [2]int{0, 0}, Label: "Folder", Folder: dir},
			{At: [2]int{3, 2}, RuntimeID: "buttons[1]", Widget: &config.Widget{Type: "wide_dashboard", City: "Kyiv"}},
		},
	}})

	if changed := nav.handleEvent(actionIndex([2]int{0, 0}), true); !changed {
		t.Fatal("expected folder press to change page")
	}

	var back *config.Button
	for _, btn := range nav.currentButtons() {
		if btn.At == backButton.At {
			b := btn
			back = &b
			break
		}
	}
	if back == nil {
		t.Fatal("expected back button in wide slot")
	}
	if !back.SyntheticBack {
		t.Fatal("expected contextual back button to be synthetic")
	}
	if back.Widget == nil || back.Widget.Type != "wide_dashboard" {
		t.Fatalf("expected wide dashboard to render back button, got %#v", back.Widget)
	}
}

func TestNavigatorIgnoresUnknownIndex(t *testing.T) {
	nav := newNavigator([]config.Page{{
		Buttons: []config.Button{
			{At: [2]int{0, 0}, Label: "A"},
		},
	}})

	if changed := nav.handleEvent(99, true); changed {
		t.Fatalf("expected unknown button to do nothing, got changed=%v", changed)
	}
}

func TestNavigatorCyclesPagesAndClearsFolderStack(t *testing.T) {
	nav := newNavigator([]config.Page{
		{
			Name: "One",
			Buttons: []config.Button{
				{At: [2]int{0, 0}, Label: "Folder", Folder: &config.Folder{
					Buttons: []config.Button{{At: [2]int{0, 0}, Label: "Nested"}},
				}},
			},
		},
		{
			Name: "Two",
			Buttons: []config.Button{
				{At: [2]int{1, 0}, Label: "B"},
			},
		},
	})

	if changed := nav.handleEvent(actionIndex([2]int{0, 0}), true); !changed {
		t.Fatal("expected folder open to change page")
	}
	if len(nav.stack) != 1 {
		t.Fatalf("expected folder stack, got %d", len(nav.stack))
	}
	if !nav.nextPage() {
		t.Fatal("expected next page to change")
	}
	if len(nav.stack) != 0 {
		t.Fatalf("expected next page to clear folder stack, got %d", len(nav.stack))
	}
	if got := nav.currentPageButtons()[0].Label; got != "B" {
		t.Fatalf("expected second page, got %q", got)
	}
	if !nav.nextPage() {
		t.Fatal("expected looping next page to change")
	}
	if got := nav.currentPageButtons()[0].Label; got != "Folder" {
		t.Fatalf("expected wrap to first page, got %q", got)
	}
	if !nav.prevPage() {
		t.Fatal("expected prev page to loop")
	}
	if got := nav.currentPageButtons()[0].Label; got != "B" {
		t.Fatalf("expected prev to loop to last page, got %q", got)
	}
}

func TestRunBindingHandlesPageActions(t *testing.T) {
	nav := newNavigator([]config.Page{
		{Name: "One", Buttons: []config.Button{{At: [2]int{0, 0}, Label: "A"}}},
		{Name: "Two", Buttons: []config.Button{{At: [2]int{0, 0}, Label: "B"}}},
	})

	changed, err := runBinding(&config.GestureBinding{Type: "page_next"}, nav, nil)
	if err != nil {
		t.Fatalf("run binding: %v", err)
	}
	if !changed {
		t.Fatal("expected page change")
	}
	if got := nav.currentPageButtons()[0].Label; got != "B" {
		t.Fatalf("expected second page, got %q", got)
	}

	changed, err = runBinding(&config.GestureBinding{
		Chain: []config.ChainStep{
			{Action: &config.Action{Type: "page_prev"}},
		},
	}, nav, nil)
	if err != nil {
		t.Fatalf("run chain binding: %v", err)
	}
	if !changed {
		t.Fatal("expected page change from chain")
	}
	if got := nav.currentPageButtons()[0].Label; got != "A" {
		t.Fatalf("expected first page after prev, got %q", got)
	}
}

func actionIndex(at [2]int) int {
	return at[1]*5 + at[0]
}
