package main

import (
	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/widgets"
)

// backButton is the synthetic key injected into every folder page.
var backButton = config.Button{
	At:    [2]int{3, 2},
	Label: "Back",
}

// navigator tracks the current top-level page and the stack of open
// folders. It is owned by the event loop goroutine.
type navigator struct {
	pages []config.Page
	page  int
	stack []*config.Folder
}

func newNavigator(pages []config.Page) *navigator {
	return &navigator{pages: append([]config.Page(nil), pages...)}
}

func (n *navigator) currentButtons() []config.Button {
	buttons := append([]config.Button(nil), n.currentPageButtons()...)
	if len(n.stack) > 0 {
		buttons = append(buttons, n.contextualBackButton())
	}
	return buttons
}

func (n *navigator) contextualBackButton() config.Button {
	for _, btn := range n.currentRootButtons() {
		if btn.At == backButton.At && btn.Widget != nil && btn.Widget.Type == "wide_dashboard" {
			btn.SyntheticBack = true
			return btn
		}
	}
	btn := backButton
	btn.SyntheticBack = true
	return btn
}

func (n *navigator) handleEvent(index int, pressed bool) (pageChanged bool) {
	for _, btn := range n.currentButtons() {
		if btn.HIDIndex() != index {
			continue
		}
		if len(n.stack) > 0 && btn.SyntheticBack {
			if !pressed {
				return false
			}
			n.stack = n.stack[:len(n.stack)-1]
			return true
		}
		if btn.Folder != nil {
			if !pressed {
				return false
			}
			n.stack = append(n.stack, btn.Folder)
			return true
		}
		return false
	}
	return false
}

func (n *navigator) currentPageButtons() []config.Button {
	if len(n.stack) == 0 {
		if len(n.pages) == 0 {
			return nil
		}
		return n.pages[n.page].Buttons
	}
	return n.stack[len(n.stack)-1].Buttons
}

func (n *navigator) currentRootButtons() []config.Button {
	if len(n.pages) == 0 {
		return nil
	}
	return n.pages[n.page].Buttons
}

func (n *navigator) nextPage() bool {
	if len(n.pages) < 2 {
		return false
	}
	n.page++
	if n.page >= len(n.pages) {
		n.page = 0
	}
	n.stack = nil
	return true
}

func (n *navigator) prevPage() bool {
	if len(n.pages) < 2 {
		return false
	}
	n.page--
	if n.page < 0 {
		n.page = len(n.pages) - 1
	}
	n.stack = nil
	return true
}

func (n *navigator) findButton(index int) *config.Button {
	for _, btn := range n.currentButtons() {
		if btn.HIDIndex() == index {
			b := btn
			return &b
		}
	}
	return nil
}

func (n *navigator) Context() widgets.Context {
	return widgets.Context{
		PageIndex:   n.page,
		TotalPages:  len(n.pages),
		FolderDepth: len(n.stack),
	}
}
