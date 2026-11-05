//go:build personal

package widgets

import (
	"errors"
	"fmt"
	"time"

	ig "github.com/alexbabintsev/udeck/internal/gestures"
)

// newPersonalWidget builds the widgets that only `-tags personal` includes.
func (m *Manager) newPersonalWidget(id string, def widgetDef) (Widget, error) {
	switch def.kind {
	case "freqtrade":
		return NewFreqtradeWidget(id, def.btn.Widget, m.notify)
	case "site":
		return NewSiteWidget(id, def.btn.Widget, m.notify)
	case "project":
		return NewProjectWidget(id, def.btn, m.settings.Project, m.notify)
	default:
		return nil, fmt.Errorf("widget %q type %q unsupported", id, def.kind)
	}
}

func personalGestures(def widgetDef) (ig.Options, bool) {
	switch def.kind {
	case "freqtrade", "site":
		return ig.Options{HasTap: true}, true
	case "project":
		opts := ig.Options{HoldDelay: holdDuration(def.btn.Widget.HoldMS)}
		on := def.btn.Widget.On
		if on == nil {
			opts.HasTap = true
			opts.HasDoubleTap = true
			opts.HasHold = true
			return opts, true
		}
		opts.HasTap = on.Tap != ""
		opts.HasDoubleTap = on.DoubleTap != ""
		opts.HasHold = on.Hold != ""
		opts.HasRepeat = on.Repeat != ""
		if opts.HasRepeat {
			opts.RepeatEvery = opts.HoldDelay
		}
		return ig.Options{
			HasTap:         opts.HasTap,
			HasDoubleTap:   opts.HasDoubleTap,
			HasHold:        opts.HasHold,
			HasRepeat:      opts.HasRepeat,
			HoldDelay:      opts.HoldDelay,
			DoubleTapDelay: 300 * time.Millisecond,
			RepeatEvery:    opts.RepeatEvery,
		}, true
	default:
		return ig.Options{}, false
	}
}

// CloseAllProjects stops every configured project, including ones on
// pages that were never shown. The stop scripts run without holding the
// manager lock: they can take seconds each, and the event loop needs the
// lock for every page switch and widget update.
func (m *Manager) CloseAllProjects() error {
	var (
		errs     []error
		projects []*ProjectWidget
	)
	m.mu.Lock()
	for id, def := range m.defs {
		if def.kind != "project" {
			continue
		}
		item, ok := m.items[id]
		if !ok {
			created, err := m.newWidgetLocked(id)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", id, err))
				continue
			}
			// Off screen: no polling until its page is shown.
			if p, ok := created.(pausable); ok {
				p.setActive(false)
			}
			m.items[id] = created
			item = created
		}
		project, ok := item.(*ProjectWidget)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: project widget type assertion failed", id))
			continue
		}
		projects = append(projects, project)
	}
	m.mu.Unlock()

	for _, project := range projects {
		if err := project.stopNow(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", project.id, err))
		}
	}
	return errors.Join(errs...)
}
