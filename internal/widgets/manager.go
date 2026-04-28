package widgets

import (
	"fmt"
	"sync"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

type Widget interface {
	IconPath() string
	Press()
	Close() error
}

type widgetDef struct {
	kind string
	cfg  config.Widget
}

var newSpotifyService = NewSpotifyService

type Manager struct {
	mu      sync.RWMutex
	defs    map[string]widgetDef
	items   map[string]Widget
	updates chan string
	spotify *SpotifyService
}

func NewManager(buttons []config.Button) (*Manager, error) {
	m := &Manager{
		defs:    map[string]widgetDef{},
		items:   map[string]Widget{},
		updates: make(chan string, 32),
	}
	m.collectDefs(buttons)
	if pollMS, ok := m.spotifyPollMS(); ok {
		service, err := newSpotifyService(pollMS)
		if err != nil {
			return nil, err
		}
		m.spotify = service
	}
	return m, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, item := range m.items {
		_ = item.Close()
		delete(m.items, id)
	}
	if m.spotify != nil {
		_ = m.spotify.Close()
		m.spotify = nil
	}
	return nil
}

func (m *Manager) Updates() <-chan string {
	return m.updates
}

func (m *Manager) ResolveButtons(buttons []config.Button) ([]config.Button, error) {
	if err := m.SyncVisible(buttons); err != nil {
		return nil, err
	}

	resolved := make([]config.Button, len(buttons))
	copy(resolved, buttons)

	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range resolved {
		if resolved[i].Widget == nil {
			continue
		}
		// Widget visuals are rendered into the icon itself; suppress the
		// regular device label overlay so it doesn't obscure the widget.
		resolved[i].Label = ""
		if item, ok := m.items[resolved[i].RuntimeID]; ok {
			resolved[i].Icon = item.IconPath()
		}
	}
	return resolved, nil
}

func (m *Manager) SyncVisible(buttons []config.Button) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, btn := range buttons {
		if btn.Widget == nil {
			continue
		}
		if _, ok := m.items[btn.RuntimeID]; ok {
			continue
		}
		item, err := m.newWidgetLocked(btn.RuntimeID)
		if err != nil {
			return err
		}
		m.items[btn.RuntimeID] = item
	}
	return nil
}

func (m *Manager) Press(id string) bool {
	m.mu.RLock()
	item, ok := m.items[id]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	item.Press()
	return true
}

func (m *Manager) collectDefs(buttons []config.Button) {
	for _, btn := range buttons {
		if btn.Widget != nil {
			m.defs[btn.RuntimeID] = widgetDef{
				kind: btn.Widget.Type,
				cfg:  *btn.Widget,
			}
		}
		if btn.Directory != nil {
			m.collectDefs(btn.Directory.Buttons)
		}
	}
}

func (m *Manager) newWidgetLocked(id string) (Widget, error) {
	def, ok := m.defs[id]
	if !ok {
		return nil, fmt.Errorf("widget %q not found", id)
	}

	switch def.kind {
	case "memory":
		return NewMemoryWidget(id, &def.cfg, m.notify)
	case "spotify_play_pause", "spotify_next", "spotify_artwork":
		if m.spotify == nil {
			service, err := newSpotifyService(def.cfg.PollMS)
			if err != nil {
				return nil, err
			}
			m.spotify = service
		}
		switch def.kind {
		case "spotify_play_pause":
			return NewSpotifyPlayPauseWidget(id, m.spotify, m.notify)
		case "spotify_next":
			return NewSpotifyNextWidget(id, m.spotify, m.notify)
		case "spotify_artwork":
			return NewSpotifyArtworkWidget(id, m.spotify, m.notify)
		}
	}

	return nil, fmt.Errorf("widget %q type %q unsupported", id, def.kind)
}

func isSpotifyWidgetType(kind string) bool {
	switch kind {
	case "spotify_play_pause", "spotify_next", "spotify_artwork":
		return true
	default:
		return false
	}
}

func (m *Manager) spotifyPollMS() (int, bool) {
	for _, def := range m.defs {
		if isSpotifyWidgetType(def.kind) {
			return def.cfg.PollMS, true
		}
	}
	return 0, false
}

func (m *Manager) notify(id string) {
	select {
	case m.updates <- id:
	default:
	}
}
