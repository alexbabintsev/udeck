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

	got, err := m.ResolveButtons(buttons)
	if err != nil {
		t.Fatalf("resolve buttons: %v", err)
	}
	if got[0].Icon == "" {
		t.Fatal("expected resolved widget icon path")
	}
	if got[0].Label != "" {
		t.Fatalf("expected widget label to be suppressed, got %q", got[0].Label)
	}
}

func TestManagerPrewarmsSpotifyAndKeepsItCached(t *testing.T) {
	oldFactory := newSpotifyService
	defer func() { newSpotifyService = oldFactory }()

	var created int
	newSpotifyService = func(pollMS int) (*SpotifyService, error) {
		created++
		s := &SpotifyService{}
		s.cancel = func() {}
		return s, nil
	}

	buttons := []config.Button{
		{
			At:        [2]int{0, 0},
			RuntimeID: "buttons[0]",
			Widget:    &config.Widget{Type: "memory"},
		},
		{
			At:        [2]int{1, 0},
			RuntimeID: "buttons[1]",
			Widget:    &config.Widget{Type: "spotify_play_pause"},
		},
	}

	m, err := NewManager(buttons)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer m.Close()

	if created != 1 || m.spotify == nil {
		t.Fatalf("expected spotify runtime to prewarm once, created=%d spotify=%v", created, m.spotify)
	}

	if err := m.SyncVisible(buttons[:1]); err != nil {
		t.Fatalf("sync visible memory page: %v", err)
	}
	if created != 1 || m.spotify == nil {
		t.Fatalf("expected spotify runtime to stay warm on non-spotify page, created=%d spotify=%v", created, m.spotify)
	}

	if err := m.SyncVisible(buttons[1:]); err != nil {
		t.Fatalf("sync visible spotify page: %v", err)
	}
	if created != 1 || m.spotify == nil {
		t.Fatalf("expected spotify runtime to reuse warm instance, created=%d spotify=%v", created, m.spotify)
	}

	if err := m.SyncVisible(buttons[:1]); err != nil {
		t.Fatalf("sync back to memory page: %v", err)
	}
	if m.spotify == nil {
		t.Fatal("expected spotify runtime to remain cached after leaving page")
	}
}
