package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/device"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/widgets"
)

const (
	// deviceKeepaliveInterval is how long the link may stay idle before a
	// single no-op packet is written. It must never re-upload the page:
	// a full OUT_SET_BUTTONS visibly redraws every key.
	deviceKeepaliveInterval = 30 * time.Second
	deviceRecoverRetries    = 2
	wakeRecoverRetries      = 10
	offlinePollInterval     = 3 * time.Second
	readPollTimeout         = 100 * time.Millisecond
)

// pause is time.Sleep; tests replace it to run recovery paths instantly.
var pause = time.Sleep

// deck is the device as seen by the event loop; *device.Device
// implements it and tests substitute a fake.
type deck interface {
	SendPacket(pkt [protocol.PacketSize]byte) error
	SendPackets(pkts [][protocol.PacketSize]byte) error
	ReadPacket(timeout time.Duration) ([]byte, error)
	Reconnect() (reset bool, err error)
	TryReconnect() (reset bool, err error)
	Close() error
}

var _ deck = (*device.Device)(nil)

// deviceError marks a failed HID transfer, as opposed to a failure to
// build the page (unreadable icon, widget error) that reconnecting to the
// device cannot fix.
type deviceError struct{ err error }

func (e *deviceError) Error() string { return e.err.Error() }
func (e *deviceError) Unwrap() error { return e.err }

func isDeviceError(err error) bool {
	var de *deviceError
	return errors.As(err, &de)
}

// session owns the device and all runtime state of the event loop. Only
// the loop goroutine touches it.
type session struct {
	cfgPath  string
	cfg      *config.Config
	dev      deck
	nav      *navigator
	gestures *gestureManager
	toggles  *toggleManager
	widgets  *widgets.Manager
	actions  *actionRunner
	// power delivers macOS sleep/wake notifications; nil when
	// unavailable. powerAck lets a pending sleep proceed.
	power    <-chan powerEvent
	powerAck func()
	// out receives one line per device event (dump-events, --debug).
	out io.Writer
	// tx performs all device writes off the loop goroutine; nil in
	// dump-events mode.
	tx *outbox
	// firmware is the device's firmware version ("" if unknown); it
	// decides whether key images can be WebP.
	firmware string
	// pageMoves carries page switches (+1/-1) from chains running on
	// the action worker to the loop, which owns the navigator.
	pageMoves chan int

	sleeping bool
	// offline is set when recovery gave up; the loop then polls for the
	// device to come back instead of exiting.
	offline     bool
	lastOffline time.Time
}

func (s *session) run(ctx context.Context) error {
	var reloader *configReloader
	if s.cfgPath != "" {
		reloader = newConfigReloader(s.cfgPath, s.cfg.IconPaths())
		defer reloader.Close()
	}
	if s.out == nil {
		s.out = os.Stdout
	}
	var widgetUpdates <-chan struct{}

	for {
		select {
		case <-ctx.Done():
			// Shutdown, logout or a stopped service: dim the deck like
			// before sleep. Ctrl-C leaves it as it is.
			if s.nav != nil && terminatedBySystem(ctx) {
				s.handlePower(powerEventShutdown)
			}
			return nil
		default:
		}

		if s.power != nil {
			select {
			case evt, ok := <-s.power:
				if !ok {
					s.power = nil
					break
				}
				if done := s.handlePower(evt); done {
					return nil
				}
			default:
			}
		}
		if s.sleeping {
			pause(200 * time.Millisecond)
			continue
		}
		if s.offline {
			s.pollOffline()
			continue
		}

		if reloader != nil {
			select {
			case <-reloader.Changes():
				s.reloadConfig(reloader)
			default:
			}
		}

		if s.tx != nil {
			select {
			case f := <-s.tx.Failures():
				if err := s.recover("update", f.err, func() error { return s.tx.resend(f.job) }); err != nil {
					return err
				}
				continue
			default:
			}
			if s.tx.sinceWrite() >= deviceKeepaliveInterval {
				if err := s.do("keepalive", s.keepalive); err != nil {
					return err
				}
			}
		}

		select {
		case delta := <-s.pageMoves:
			if err := s.movePage(delta); err != nil {
				return err
			}
		default:
		}

		if s.widgets != nil {
			widgetUpdates = s.widgets.Updates()
		}
		select {
		case <-widgetUpdates:
			if ids := s.widgets.TakeDirty(); len(ids) > 0 && s.nav != nil {
				if err := s.do("update widgets", func() error { return s.updateButtons(ids) }); err != nil {
					return err
				}
			}
		default:
		}

		if s.gestures != nil && s.nav != nil {
			if evt := s.gestures.Tick(s.nav.currentButtons(), time.Now()); evt != nil {
				binding, changed := resolveBindingEvent(evt, s.toggles)
				if changed {
					if err := s.do("update toggle", s.updateButtonsFn(evt.ButtonID)); err != nil {
						return err
					}
				}
				if err := s.dispatch(binding); err != nil {
					return err
				}
			}
		}
		if s.widgets != nil && s.nav != nil {
			s.widgets.Tick(s.nav.currentButtons(), time.Now())
		}

		raw, err := s.dev.ReadPacket(readPollTimeout)
		if err != nil {
			if rerr := s.recover("read", &deviceError{err}, nil); rerr != nil {
				return rerr
			}
			continue
		}
		if len(raw) == 0 {
			continue
		}
		if err := s.handlePacket(raw); err != nil {
			return err
		}
	}
}

func (s *session) handlePacket(raw []byte) error {
	ev, err := protocol.ParseEvent(raw)
	if err != nil {
		debug.Logf("parse error: %v (raw len=%d, head=% x)", err, len(raw), raw[:min(len(raw), 16)])
		return nil
	}
	if ev.Kind == protocol.EventLabelStyleAck {
		return nil
	}
	if ev.Kind == protocol.EventDeviceInfo && s.nav != nil {
		debug.Logf("device info: %s", compactJSON(ev.Info))
		return nil
	}
	if s.nav == nil {
		fmt.Fprintln(s.out, ev.String())
	} else {
		debug.Logf("event %s", ev.String())
	}
	if ev.Kind != protocol.EventButton || s.nav == nil {
		return nil
	}

	btn := s.nav.findButton(int(ev.Index))
	if s.nav.handleEvent(int(ev.Index), ev.Pressed) {
		if s.gestures != nil {
			s.gestures.Reset()
		}
		return s.do("update page", s.queuePage)
	}
	if btn != nil && btn.SyntheticBack {
		return nil
	}

	var binding *config.GestureBinding
	if btn != nil && btn.On != nil && s.gestures != nil {
		evt := s.gestures.HandleEvent(btn, ev.Pressed, time.Now())
		if evt == nil {
			return nil
		}
		var changed bool
		binding, changed = resolveBindingEvent(evt, s.toggles)
		if changed {
			if err := s.do("update toggle", s.updateButtonsFn(evt.ButtonID)); err != nil {
				return err
			}
		}
		if binding == nil {
			return nil
		}
	}
	if s.widgets != nil && btn != nil && btn.Widget != nil {
		if s.widgets.HandleEvent(btn.RuntimeID, ev.Pressed, time.Now()) {
			return nil
		}
	}
	return s.dispatch(binding)
}

// dispatch runs a binding. Bindings that switch pages must run on the
// loop goroutine because they mutate the navigator; everything else (shell,
// keys, HTTP, delays) runs on the action worker so a slow HTTP target or a
// chain with delays never stalls HID reads and widget refreshes.
func (s *session) dispatch(binding *config.GestureBinding) error {
	if binding == nil {
		return nil
	}
	if s.actions != nil && !bindingNeedsLoop(binding) {
		mgr := s.widgets
		var p pager
		if s.pageMoves != nil {
			p = deferredPager(s.pageMoves)
		}
		s.actions.submit(func() error {
			_, err := runBinding(binding, p, mgr)
			return err
		})
		return nil
	}
	var p pager
	if s.nav != nil {
		p = s.nav
	}
	changed, err := runBinding(binding, p, s.widgets)
	if err != nil {
		log.Printf("action error: %v", err)
		return nil
	}
	if changed {
		return s.do("switch page", s.queuePage)
	}
	return nil
}

// movePage applies a page switch posted by the action worker.
func (s *session) movePage(delta int) error {
	if s.nav == nil {
		return nil
	}
	changed := false
	if delta > 0 {
		changed = s.nav.nextPage()
	} else {
		changed = s.nav.prevPage()
	}
	if !changed {
		return nil
	}
	if s.gestures != nil {
		s.gestures.Reset()
	}
	return s.do("switch page", s.queuePage)
}

// do runs a device operation. Build errors are logged and the loop keeps
// going with the previous screen contents; HID errors go through
// recovery. A non-nil return means the loop must stop.
func (s *session) do(what string, op func() error) error {
	err := op()
	if err == nil {
		return nil
	}
	if !isDeviceError(err) {
		log.Printf("%s: %v", what, err)
		return nil
	}
	return s.recover(what, err, op)
}

// recover reconnects the HID handle and repeats only the request that
// failed, so a dead handle on a device that kept its screen does not
// flash every key. A full page is uploaded only when the device was
// actually reset (it vanished from the bus) — its screen is gone anyway.
func (s *session) recover(what string, cause error, retry func() error) error {
	if s.nav == nil {
		return cause
	}
	log.Printf("device error during %s: %v; reconnecting", what, cause)
	needFull := errors.Is(cause, device.ErrDeviceReset)
	lastErr := cause
	for attempt := 1; attempt <= deviceRecoverRetries; attempt++ {
		reset, err := s.reconnect(s.dev.Reconnect)
		if err != nil {
			lastErr = err
			debug.Logf("recover attempt %d/%d: reconnect failed: %v", attempt, deviceRecoverRetries, err)
			pause(500 * time.Millisecond)
			continue
		}
		needFull = needFull || reset
		op := retry
		if needFull || op == nil {
			op = func() error { return nil }
			if needFull {
				op = s.refreshPage
			}
		}
		err = op()
		if err == nil {
			log.Printf("device recovered (full redraw=%v)", needFull)
			return nil
		}
		if !isDeviceError(err) {
			log.Printf("%s after reconnect: %v", what, err)
			return nil
		}
		lastErr = err
		needFull = needFull || errors.Is(err, device.ErrDeviceReset)
		debug.Logf("recover attempt %d/%d: %s failed: %v", attempt, deviceRecoverRetries, what, err)
		pause(500 * time.Millisecond)
	}
	log.Printf("device recovery failed: %v; waiting for the device to come back", lastErr)
	s.offline = true
	s.lastOffline = time.Time{}
	return nil
}

// pollOffline tries to reattach a device that recovery gave up on. The
// device may have been replugged or power-cycled, so a full page is sent.
func (s *session) pollOffline() {
	if time.Since(s.lastOffline) < offlinePollInterval {
		pause(200 * time.Millisecond)
		return
	}
	s.lastOffline = time.Now()
	if _, err := s.reconnect(s.dev.TryReconnect); err != nil {
		debug.Logf("offline: %v", err)
		return
	}
	if err := s.refreshPage(); err != nil {
		debug.Logf("offline: initial page failed: %v", err)
		return
	}
	s.offline = false
	log.Println("device reconnected")
}

func (s *session) handlePower(evt powerEvent) (stop bool) {
	switch evt {
	case powerEventSleep:
		log.Println("system sleep detected; dimming and closing device")
		s.sleeping = true
		if s.tx != nil {
			s.tx.discard()
		}
		s.withTx(func() { prepareDeviceForSleep(s.dev) })
		if s.powerAck != nil {
			s.powerAck()
		}
	case powerEventWake:
		log.Println("system wake detected; restoring device")
		s.sleeping = false
		s.offline = false
		if err := s.restoreAfterWake(); err != nil {
			log.Printf("wake recovery failed: %v; waiting for the device", err)
			s.offline = true
			s.lastOffline = time.Now()
		}
	case powerEventShutdown:
		log.Println("system shutdown detected; dimming and closing device")
		s.withTx(func() { prepareDeviceForSleep(s.dev) })
		return true
	}
	return false
}

// restoreAfterWake reopens the handle closed before sleep and redraws the
// page: the device was dimmed and may have dropped host mode while the
// host slept, so a full upload is required here.
func (s *session) restoreAfterWake() error {
	if s.nav == nil {
		_, err := s.dev.Reconnect()
		return err
	}
	var lastErr error
	for attempt := 1; attempt <= wakeRecoverRetries; attempt++ {
		if _, err := s.reconnect(s.dev.Reconnect); err != nil {
			lastErr = err
			pause(500 * time.Millisecond)
			continue
		}
		if err := s.refreshPage(); err != nil {
			lastErr = err
			if !isDeviceError(err) {
				return err
			}
			pause(500 * time.Millisecond)
			continue
		}
		log.Println("device restored after wake")
		return nil
	}
	return lastErr
}

func (s *session) reloadConfig(reloader *configReloader) {
	nextCfg, err := config.Load(s.cfgPath)
	if err != nil {
		log.Printf("config reload failed: %v", err)
		return
	}
	warnConfigPermissions(s.cfgPath)
	applyImageSettings(nextCfg, s.firmware)
	nextNav := newNavigator(nextCfg.PagesOrDefault())
	nextToggles := newToggleManager(nextCfg)
	nextWidgetMgr, err := widgets.NewManager(nextCfg)
	if err != nil {
		log.Printf("widget reload failed: %v", err)
		return
	}
	nextWidgetMgr.SetContext(nextNav.Context())
	buttons, err := nextWidgetMgr.ResolveButtons(nextToggles.ResolveButtons(nextNav.currentButtons()))
	if err != nil {
		_ = nextWidgetMgr.Close()
		log.Printf("widget resolve failed: %v", err)
		return
	}
	if err := s.sendPageSync(nextCfg, buttons); err != nil {
		_ = nextWidgetMgr.Close()
		log.Printf("config apply failed: %v", err)
		if isDeviceError(err) {
			// Keep the old config but make sure the device is usable.
			_ = s.recover("config reload", err, s.refreshPage)
		}
		return
	}
	if s.widgets != nil {
		_ = s.widgets.Close()
	}
	s.cfg = nextCfg
	s.nav = nextNav
	s.gestures = newGestureManager()
	s.toggles = nextToggles
	s.widgets = nextWidgetMgr
	reloader.UpdatePaths(s.cfgPath, nextCfg.IconPaths())
	log.Println("config reloaded")
}

// refreshPage uploads the current page and waits for the result.
func (s *session) refreshPage() error {
	buttons, err := resolvedCurrentButtons(s.nav, s.toggles, s.widgets)
	if err != nil {
		return err
	}
	return s.sendPageSync(s.cfg, buttons)
}

// queuePage schedules the current page for upload (navigation). Only
// building the button list can fail here; transfer errors come back
// through tx.Failures.
func (s *session) queuePage() error {
	if s.tx == nil {
		return s.refreshPage()
	}
	buttons, err := resolvedCurrentButtons(s.nav, s.toggles, s.widgets)
	if err != nil {
		return err
	}
	s.tx.queuePage(s.cfg, buttons)
	return nil
}

func (s *session) sendPageSync(cfg *config.Config, buttons []config.Button) error {
	if s.tx != nil {
		return s.tx.sendPage(cfg, buttons)
	}
	if err := sendPage(s.dev, buttons); err != nil {
		return err
	}
	extra, err := styleAndBrightness(cfg)
	if err != nil {
		return err
	}
	for _, pkt := range extra {
		if err := s.dev.SendPacket(pkt); err != nil {
			return &deviceError{err}
		}
	}
	return nil
}

// reconnect swaps the HID handle while no transfer is running.
func (s *session) reconnect(fn func() (bool, error)) (reset bool, err error) {
	s.withTx(func() { reset, err = fn() })
	return reset, err
}

func (s *session) withTx(fn func()) {
	if s.tx == nil {
		fn()
		return
	}
	s.tx.exclusive(fn)
}

func (s *session) updateButtonsFn(ids ...string) func() error {
	return func() error { return s.updateButtons(ids) }
}

// updateButtons redraws only the given buttons (by RuntimeID) with one
// OUT_PARTIALLY_UPDATE_BUTTONS transfer, so untouched keys do not flash.
// Buttons that are not on the current page are skipped: their latest
// state is picked up by the full upload that happens when the page is
// shown.
func (s *session) updateButtons(ids []string) error {
	buttons, err := resolvedCurrentButtons(s.nav, s.toggles, s.widgets)
	if err != nil {
		return err
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var changed []config.Button
	for _, btn := range buttons {
		if btn.RuntimeID != "" && want[btn.RuntimeID] {
			changed = append(changed, btn)
		}
	}
	if len(changed) == 0 {
		debug.Logf("update for off-screen %v skipped", ids)
		return nil
	}
	if s.tx != nil {
		s.tx.queueButtons(changed)
		return nil
	}
	pkts, err := protocol.PartiallyUpdateButtonsFromConfig(changed)
	if err != nil {
		return fmt.Errorf("build partial page zip: %w", err)
	}
	if err := s.dev.SendPackets(pkts); err != nil {
		return &deviceError{fmt.Errorf("send partial page zip: %w", err)}
	}
	return nil
}

// keepalive writes one packet that changes nothing on screen, so an idle
// link still surfaces dead handles and the device sees host traffic.
// keepalive writes one packet that changes nothing on screen, so an idle
// link still surfaces dead handles and the device sees host traffic. It
// is skipped while a transfer is running.
func (s *session) keepalive() error {
	sent, err := s.tx.trySendPacket(keepalivePacket())
	if err != nil {
		return fmt.Errorf("keepalive: %w", err)
	}
	if sent {
		debug.Logf("keepalive sent")
	}
	return nil
}

// keepalivePacket is the device-info request Ulanzi Studio uses to
// check the device is still connected; it changes nothing on screen and
// the reply shows up in the --debug log.
func keepalivePacket() [protocol.PacketSize]byte {
	return protocol.DeviceInfoRequestPacket()
}

func prepareDeviceForSleep(dev deck) {
	if dev == nil {
		return
	}
	pkt, err := protocol.SetBrightnessPacket(0)
	if err == nil {
		if err := dev.SendPacket(pkt); err != nil {
			debug.Logf("dim before sleep: %v", err)
		}
	}
	_ = dev.Close()
}

// sendPage uploads a full page. Pass nil buttons for an empty page (used
// by dump-events to enter host mode).
func sendPage(dev deck, buttons []config.Button) error {
	pkts, err := protocol.SetButtonsFromConfig(buttons)
	if err != nil {
		return fmt.Errorf("build page zip: %w", err)
	}
	debug.Logf("sending full page: %d buttons, %d packets", len(buttons), len(pkts))
	if err := dev.SendPackets(pkts); err != nil {
		return &deviceError{fmt.Errorf("send page zip: %w", err)}
	}
	return nil
}

func resolvedCurrentButtons(nav *navigator, toggles *toggleManager, widgetMgr *widgets.Manager) ([]config.Button, error) {
	buttons := nav.currentButtons()
	if toggles != nil {
		buttons = toggles.ResolveButtons(buttons)
	}
	if widgetMgr != nil {
		widgetMgr.SetContext(nav.Context())
		resolved, err := widgetMgr.ResolveButtons(buttons)
		if err != nil {
			return nil, fmt.Errorf("resolve widgets: %w", err)
		}
		buttons = resolved
	}
	return buttons, nil
}

// close stops the writer and the current widget manager (the session
// swaps managers on config reload).
func (s *session) close() {
	if s.actions != nil {
		s.actions.close()
		s.actions = nil
	}
	if s.tx != nil {
		s.tx.close()
	}
	if s.widgets != nil {
		_ = s.widgets.Close()
	}
}

// compactJSON squeezes the device's pretty-printed JSON onto one line.
func compactJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		return s
	}
	return buf.String()
}
