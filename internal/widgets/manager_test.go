package widgets

import (
	"testing"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

func TestManagerResolveButtonsInjectsWidgetIcon(t *testing.T) {
	buttons := []config.Button{
		{
			At:        [2]int{0, 0},
			Label:     "Memory",
			RuntimeID: "buttons[0]",
			Widget:    &config.Widget{Type: "memory"},
		},
	}

	m, err := NewManager(buttons)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer m.Close()

	got := m.ResolveButtons(buttons)
	if got[0].Icon == "" {
		t.Fatal("expected resolved widget icon path")
	}
	if got[0].Label != "" {
		t.Fatalf("expected widget label to be suppressed, got %q", got[0].Label)
	}
}
