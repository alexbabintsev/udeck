package widgets

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
)

type Widget interface {
	IconPath() string
	HandleGesture(kind ig.Kind)
	Close() error
}

type contextualWidget interface {
	SetContext(Context)
}

type widgetDef struct {
	kind string
	btn  config.Button
}

var newSpotifyService = NewSpotifyService

type Manager struct {
	mu       sync.RWMutex
	defs     map[string]widgetDef
	items    map[string]Widget
	updates  chan string
	spotify  *SpotifyService
	settings config.WidgetSettings
	engine   *ig.Engine
	context  Context
}

func NewManager(cfg *config.Config) (*Manager, error) {
	m := &Manager{
		defs:     map[string]widgetDef{},
		items:    map[string]Widget{},
		updates:  make(chan string, 32),
		settings: cfg.Widgets,
		engine:   ig.New(),
	}
	for _, page := range cfg.PagesOrDefault() {
		m.collectDefs(page.Buttons)
	}
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

func (m *Manager) SetContext(ctx Context) {
	m.mu.Lock()
	m.context = ctx
	for _, item := range m.items {
		if contextual, ok := item.(contextualWidget); ok {
			contextual.SetContext(ctx)
		}
	}
	m.mu.Unlock()
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

func (m *Manager) HandleEvent(id string, pressed bool, now time.Time) bool {
	opts, ok := m.gestureOptions(id)
	if !ok {
		return false
	}
	kind := m.engine.HandleEvent(id, opts, pressed, now)
	if kind == ig.None {
		return true
	}
	m.mu.RLock()
	item, ok := m.items[id]
	m.mu.RUnlock()
	if !ok {
		return true
	}
	item.HandleGesture(kind)
	return true
}

func (m *Manager) CloseAllProjects() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for id, def := range m.defs {
		if def.kind != "project" {
			continue
		}

		item, ok := m.items[id]
		if !ok {
			var err error
			item, err = m.newWidgetLocked(id)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", id, err))
				continue
			}
			m.items[id] = item
		}

		project, ok := item.(*ProjectWidget)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: project widget type assertion failed", id))
			continue
		}
		if err := project.stopNow(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
		}
	}

	return errors.Join(errs...)
}

func (m *Manager) Tick(buttons []config.Button, now time.Time) bool {
	var bindings []ig.Binding
	for _, btn := range buttons {
		if btn.Widget == nil {
			continue
		}
		opts, ok := m.gestureOptions(btn.RuntimeID)
		if !ok {
			continue
		}
		bindings = append(bindings, ig.Binding{ID: btn.RuntimeID, Options: opts})
	}
	id, kind := m.engine.Tick(bindings, now)
	if kind == ig.None {
		return false
	}
	m.mu.RLock()
	item := m.items[id]
	m.mu.RUnlock()
	if item == nil {
		return false
	}
	item.HandleGesture(kind)
	return true
}

func (m *Manager) collectDefs(buttons []config.Button) {
	for _, btn := range buttons {
		if btn.Widget != nil {
			m.defs[btn.RuntimeID] = widgetDef{
				kind: btn.Widget.Type,
				btn:  btn,
			}
		}
		if btn.Folder != nil {
			m.collectDefs(btn.Folder.Buttons)
		}
	}
}

func (m *Manager) newWidgetLocked(id string) (Widget, error) {
	def, ok := m.defs[id]
	if !ok {
		return nil, fmt.Errorf("widget %q not found", id)
	}

	var (
		item Widget
		err  error
	)

	switch def.kind {
	case "memory":
		item, err = NewMemoryWidget(id, def.btn.Widget, m.notify)
	case "freqtrade":
		item, err = NewFreqtradeWidget(id, def.btn.Widget, m.notify)
	case "wide_dashboard":
		if m.spotify == nil {
			service, err := newSpotifyService(defaultSpotifyPollMS)
			if err != nil {
				return nil, err
			}
			m.spotify = service
		}
		item, err = NewWideDashboardWidget(id, def.btn.Widget, m.spotify, m.notify)
	case "spotify_play_pause", "spotify_next", "spotify_artwork":
		if m.spotify == nil {
			service, err := newSpotifyService(def.btn.Widget.PollMS)
			if err != nil {
				return nil, err
			}
			m.spotify = service
		}
		switch def.kind {
		case "spotify_play_pause":
			item, err = NewSpotifyPlayPauseWidget(id, m.spotify, m.notify)
		case "spotify_next":
			item, err = NewSpotifyNextWidget(id, m.spotify, m.notify)
		case "spotify_artwork":
			item, err = NewSpotifyArtworkWidget(id, m.spotify, m.notify)
		}
	case "project":
		item, err = NewProjectWidget(id, def.btn, m.settings.Project, m.notify)
	default:
		return nil, fmt.Errorf("widget %q type %q unsupported", id, def.kind)
	}

	if err != nil {
		return nil, err
	}
	if contextual, ok := item.(contextualWidget); ok {
		contextual.SetContext(m.context)
	}
	return item, nil
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
			return def.btn.Widget.PollMS, true
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

func (m *Manager) gestureOptions(id string) (ig.Options, bool) {
	def, ok := m.defs[id]
	if !ok || def.btn.Widget == nil {
		return ig.Options{}, false
	}
	switch def.kind {
	case "project":
		opts := ig.Options{HoldDelay: projectHoldDuration(def.btn.Widget.HoldMS)}
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
	case "memory", "freqtrade", "spotify_play_pause", "spotify_next", "spotify_artwork", "wide_dashboard":
		return ig.Options{HasTap: true}, true
	default:
		return ig.Options{}, false
	}
}
