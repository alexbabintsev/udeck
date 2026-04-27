package widgets

import (
	"sync"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

type Widget interface {
	IconPath() string
	Press()
	Close() error
}

type Manager struct {
	mu      sync.RWMutex
	items   map[string]Widget
	updates chan string
}

func NewManager(buttons []config.Button) (*Manager, error) {
	m := &Manager{
		items:   map[string]Widget{},
		updates: make(chan string, 32),
	}
	if err := m.addButtons(buttons); err != nil {
		_ = m.Close()
		return nil, err
	}
	return m, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.items {
		_ = item.Close()
	}
	return nil
}

func (m *Manager) Updates() <-chan string {
	return m.updates
}

func (m *Manager) ResolveButtons(buttons []config.Button) []config.Button {
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
	return resolved
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

func (m *Manager) addButtons(buttons []config.Button) error {
	for _, btn := range buttons {
		if btn.Widget != nil {
			var (
				item Widget
				err  error
			)
			switch btn.Widget.Type {
			case "memory":
				item, err = NewMemoryWidget(btn.RuntimeID, btn.Widget, m.notify)
			}
			if err != nil {
				return err
			}
			m.items[btn.RuntimeID] = item
		}
		if btn.Directory != nil {
			if err := m.addButtons(btn.Directory.Buttons); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) notify(id string) {
	select {
	case m.updates <- id:
	default:
	}
}
