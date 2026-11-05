//go:build !personal

package widgets

import (
	"fmt"

	ig "github.com/alexbabintsev/udeck/internal/gestures"
)

func (m *Manager) newPersonalWidget(id string, def widgetDef) (Widget, error) {
	return nil, fmt.Errorf("widget %q type %q unsupported", id, def.kind)
}

func personalGestures(widgetDef) (ig.Options, bool) { return ig.Options{}, false }

// CloseAllProjects does nothing: projects are personal-build only.
func (m *Manager) CloseAllProjects() error { return nil }
