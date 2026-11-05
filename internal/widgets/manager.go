package widgets

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
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

// pausable widgets stop polling while off screen (see widgetBase).
type pausable interface {
	setActive(on bool) (resumed bool)
}

// resumer widgets refresh stale data when shown again.
type resumer interface {
	resume()
}

type widgetDef struct {
	kind string
	btn  config.Button
}

var newSpotifyService = NewSpotifyService

type Manager struct {
	mu    sync.RWMutex
	defs  map[string]widgetDef
	items map[string]Widget
	// updates is a level-triggered wake-up signal; the IDs themselves are
	// accumulated in dirty so a burst of notifications is never dropped.
	updates  chan struct{}
	dirtyMu  sync.Mutex
	dirty    map[string]struct{}
	spotify  *SpotifyService
	settings config.WidgetSettings
	engine   *ig.Engine
	context  Context
	pageNav  *pageNavOverlay
}

func NewManager(cfg *config.Config) (*Manager, error) {
	SetFont(cfg.Widgets.Font)
	nav, err := newPageNavOverlay()
	if err != nil {
		return nil, fmt.Errorf("page nav overlay: %w", err)
	}
	m := &Manager{
		defs:     map[string]widgetDef{},
		items:    map[string]Widget{},
		updates:  make(chan struct{}, 1),
		dirty:    map[string]struct{}{},
		settings: cfg.Widgets,
		engine:   ig.New(),
		pageNav:  nav,
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
	if m.pageNav != nil {
		m.pageNav.close()
		m.pageNav = nil
	}
	return nil
}

// Updates fires when at least one widget has re-rendered its icon since
// the last TakeDirty call.
func (m *Manager) Updates() <-chan struct{} {
	return m.updates
}

// TakeDirty returns the IDs of widgets whose icons changed since the
// previous call and clears the set.
func (m *Manager) TakeDirty() []string {
	m.dirtyMu.Lock()
	defer m.dirtyMu.Unlock()
	if len(m.dirty) == 0 {
		return nil
	}
	ids := make([]string, 0, len(m.dirty))
	for id := range m.dirty {
		ids = append(ids, id)
	}
	clear(m.dirty)
	return ids
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

	pageNumber := m.context.PageIndex + 1

	for i := range resolved {
		if resolved[i].Widget == nil {
			// Overlay page number on page_next / page_prev buttons.
			if m.pageNav != nil && isPageNavButton(resolved[i]) {
				iconID := pageNavIconID(resolved[i])
				if path := m.pageNav.resolve(iconID, resolved[i].IconColor, pageNumber); path != "" {
					resolved[i].Icon = path
					resolved[i].Label = ""
				}
			}
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

	visible := make(map[string]bool, len(buttons))
	for _, btn := range buttons {
		if btn.Widget != nil {
			visible[btn.RuntimeID] = true
		}
	}
	for id, item := range m.items {
		p, ok := item.(pausable)
		if !ok {
			continue
		}
		if p.setActive(visible[id]) {
			debug.Logf("widget %s resumed", id)
			if r, ok := item.(resumer); ok {
				go r.resume()
			}
		}
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
	debug.Logf("widget %s gesture %s", id, kind)
	m.mu.RLock()
	item, ok := m.items[id]
	m.mu.RUnlock()
	if !ok {
		return true
	}
	item.HandleGesture(kind)
	return true
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
		item, err = NewWideDashboardWidget(id, def.btn.Widget, m.notify)
	case "clock":
		item, err = NewClockWidget(id, def.btn.Widget, m.notify)
	case "timer", "stopwatch", "pomodoro":
		item, err = NewTimerWidget(id, def.btn.Widget, m.notify)
	case "cpu", "disk", "network", "battery":
		item, err = NewSystemWidget(id, def.btn.Widget, m.notify)
	case "ticker":
		item, err = NewTickerWidget(id, def.btn.Widget, m.notify)
	case "volume":
		item, err = NewVolumeWidget(id, def.btn.Widget, m.notify)
	case "microphone":
		item, err = NewMicrophoneWidget(id, def.btn.Widget, m.notify)
	case "audio_output":
		item, err = NewAudioOutputWidget(id, def.btn.Widget, m.notify)
	case "media":
		item, err = NewMediaWidget(id, def.btn.Widget, m.notify)
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
	case "weather_day":
		item, err = NewWeatherDayWidget(id, def.btn.Widget, m.notify)
	case "site":
		item, err = NewSiteWidget(id, def.btn.Widget, m.notify)
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
	debug.Logf("widget %s icon changed", id)
	m.dirtyMu.Lock()
	m.dirty[id] = struct{}{}
	m.dirtyMu.Unlock()
	select {
	case m.updates <- struct{}{}:
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
	case "spotify_play_pause":
		return ig.Options{
			HasTap:    true,
			HasHold:   true,
			HoldDelay: projectHoldDuration(def.btn.Widget.HoldMS),
		}, true
	case "volume":
		return volumeGestures(def.btn.Widget), true
	case "timer", "stopwatch", "pomodoro":
		return timerGestures(), true
	case "memory", "freqtrade", "spotify_next", "spotify_artwork", "wide_dashboard", "weather_day", "site",
		"microphone", "audio_output", "media", "clock", "cpu", "disk", "network", "battery", "ticker":
		return ig.Options{HasTap: true}, true
	default:
		return ig.Options{}, false
	}
}
