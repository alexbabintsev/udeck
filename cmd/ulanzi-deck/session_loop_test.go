package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/device"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
)

// YAML coordinates are [row, col]; HID index = row*5 + col.
const loopConfig = `
device:
  brightness: 50
pages:
  - buttons:
      - at: [0, 0]
        label: Folder
        folder:
          buttons:
            - at: [0, 0]
              label: Inner
              on:
                tap: {type: shell, cmd: "echo inner"}
      - at: [1, 0]
        label: Run
        on:
          tap: {type: shell, cmd: "echo hi"}
      - at: [2, 0]
        label: Next
        on:
          tap: {type: page_next}
      - at: [0, 1]
        label: Toggle
        on:
          tap:
            toggle:
              - label: A
                action: {type: shell, cmd: "echo a"}
              - label: B
                action: {type: shell, cmd: "echo b"}
      - at: [0, 2]
        widget: {type: memory}
  - buttons:
      - at: [0, 0]
        label: Second
`

const (
	idxFolder = 0
	idxToggle = 1
	idxMemory = 2
	idxRun    = 5
	idxNext   = 10
	idxBack   = 13
)

func newTestSession(t *testing.T, yaml string) (*session, *fakeDeck) {
	t.Helper()
	noPause(t)
	path, cfg := writeConfig(t, yaml)
	dev := &fakeDeck{}
	s, err := newRunSession(path, cfg, dev)
	if err != nil {
		t.Fatalf("newRunSession: %v", err)
	}
	s.out = &bytes.Buffer{}
	t.Cleanup(s.close)
	dev.reset()
	return s, dev
}

// runReads feeds raw packets to the loop and stops it once they are
// consumed.
func runReads(t *testing.T, s *session, dev *fakeDeck, reads ...readStep) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dev.reads = append(dev.reads, reads...)
	dev.onIdle = cancel
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not stop")
	}
	if s.tx != nil {
		s.tx.drain()
	}
}

func tap(index int) []readStep {
	return []readStep{{raw: press(index, true)}, {raw: press(index, false)}}
}

func eventually(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestNewRunSessionUploadsInitialPage(t *testing.T) {
	noPause(t)
	path, cfg := writeConfig(t, loopConfig)
	dev := &fakeDeck{}
	s, err := newRunSession(path, cfg, dev)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	got := dev.cmds()
	want := []protocol.Cmd{protocol.CmdSetButtons, protocol.CmdSetLabelStyle, protocol.CmdSetBrightness}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("cmds = %v, want %v", got, want)
	}
}

func TestNewRunSessionFailsOnDeviceError(t *testing.T) {
	noPause(t)
	path, cfg := writeConfig(t, loopConfig)
	if _, err := newRunSession(path, cfg, &fakeDeck{sendErrs: []error{errHID}}); err == nil {
		t.Fatal("expected error")
	}
}

func TestFolderOpenAndBackRedrawFullPage(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	runReads(t, s, dev, tap(idxFolder)...)
	if len(s.nav.stack) != 1 {
		t.Fatal("expected to be inside the folder")
	}
	pages := dev.transfers(protocol.CmdSetButtons)
	if len(pages) != 1 {
		t.Fatalf("expected one full page, got %d", len(pages))
	}

	dev.reset()
	runReads(t, s, dev, tap(idxBack)...)
	if len(s.nav.stack) != 0 {
		t.Fatal("expected to be back at root")
	}
	if n := len(dev.transfers(protocol.CmdSetButtons)); n != 1 {
		t.Fatalf("expected one full page for back, got %d", n)
	}
}

func TestTapRunsExternalActionOffLoop(t *testing.T) {
	got := stubActions(t)
	s, dev := newTestSession(t, loopConfig)
	runReads(t, s, dev, tap(idxRun)...)
	eventually(t, func() bool { return len(got()) == 1 }, "shell action")
	if got()[0].Cmd != "echo hi" {
		t.Fatalf("unexpected action %+v", got()[0])
	}
	if n := len(dev.transfers(protocol.CmdSetButtons)); n != 0 {
		t.Fatalf("plain action must not redraw, got %d pages", n)
	}
}

func TestPageNextRedraws(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	runReads(t, s, dev, tap(idxNext)...)
	if s.nav.page != 1 {
		t.Fatalf("page = %d", s.nav.page)
	}
	if n := len(dev.transfers(protocol.CmdSetButtons)); n != 1 {
		t.Fatalf("expected full page after page switch, got %d", n)
	}
}

func TestToggleRedrawsOnlyThatKey(t *testing.T) {
	got := stubActions(t)
	s, dev := newTestSession(t, loopConfig)
	runReads(t, s, dev, tap(idxToggle)...)
	partial := dev.transfers(protocol.CmdPartiallyUpdateButtons)
	if len(partial) != 1 || fmt.Sprint(partial[0].slots) != "[1_0]" {
		t.Fatalf("expected one partial update of 1_0, got %+v", partial)
	}
	if n := len(dev.transfers(protocol.CmdSetButtons)); n != 0 {
		t.Fatalf("toggle must not redraw the page, got %d", n)
	}
	eventually(t, func() bool { return len(got()) == 1 }, "toggle action")
}

func TestWidgetTapSendsPartialUpdateForThatWidget(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	runReads(t, s, dev, append(tap(idxMemory),
		readStep{before: func() { time.Sleep(50 * time.Millisecond) }})...)
	partial := dev.transfers(protocol.CmdPartiallyUpdateButtons)
	if len(partial) == 0 {
		t.Fatal("expected a partial update after the widget re-rendered")
	}
	for _, p := range partial {
		if fmt.Sprint(p.slots) != "[2_0]" {
			t.Fatalf("partial update touched %v", p.slots)
		}
	}
	if n := len(dev.transfers(protocol.CmdSetButtons)); n != 0 {
		t.Fatalf("widget update must not redraw the page, got %d", n)
	}
}

func TestOffscreenUpdateIsSkipped(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	if err := s.updateButtons([]string{"pages[1].buttons[0]", "nope"}); err != nil {
		t.Fatal(err)
	}
	if len(dev.cmds()) != 0 {
		t.Fatalf("expected no transfer, got %v", dev.cmds())
	}
}

func TestBuildErrorDoesNotReconnect(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	if err := s.do("render", func() error { return errors.New("decode icon: bad png") }); err != nil {
		t.Fatal(err)
	}
	if dev.reconnectN != 0 || s.offline {
		t.Fatal("build errors must not touch the device")
	}
}

func TestDeviceErrorRetriesOnlyFailedRequest(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	calls := 0
	op := func() error {
		calls++
		if calls == 1 {
			return &deviceError{errHID}
		}
		return nil
	}
	if err := s.do("op", op); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || dev.reconnectN != 1 {
		t.Fatalf("calls=%d reconnects=%d", calls, dev.reconnectN)
	}
	if n := len(dev.transfers(protocol.CmdSetButtons)); n != 0 {
		t.Fatalf("handle reconnect must not redraw the page, got %d", n)
	}
}

func TestDeviceResetForcesFullRedraw(t *testing.T) {
	for name, tc := range map[string]struct {
		cause      error
		reconnects []reconnectStep
	}{
		"reset in cause":     {cause: &deviceError{fmt.Errorf("w: %w", device.ErrDeviceReset)}},
		"reset on reconnect": {cause: &deviceError{errHID}, reconnects: []reconnectStep{{reset: true}}},
	} {
		t.Run(name, func(t *testing.T) {
			s, dev := newTestSession(t, loopConfig)
			dev.reconnects = tc.reconnects
			retried := false
			if err := s.recover("op", tc.cause, func() error { retried = true; return nil }); err != nil {
				t.Fatal(err)
			}
			if retried {
				t.Fatal("a reset device needs the page, not the failed request")
			}
			if n := len(dev.transfers(protocol.CmdSetButtons)); n != 1 {
				t.Fatalf("expected full redraw, got %d", n)
			}
		})
	}
}

func TestRecoveryRetriesThenSucceeds(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	dev.reconnects = []reconnectStep{{err: errors.New("busy")}}
	calls := 0
	err := s.recover("op", &deviceError{errHID}, func() error { calls++; return nil })
	if err != nil || calls != 1 || dev.reconnectN != 2 {
		t.Fatalf("err=%v calls=%d reconnects=%d", err, calls, dev.reconnectN)
	}
}

func TestRecoveryRetryHitsResetOrBuildError(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	calls := 0
	op := func() error {
		calls++
		if calls == 1 {
			return &deviceError{fmt.Errorf("x: %w", device.ErrDeviceReset)}
		}
		return nil
	}
	if err := s.recover("op", &deviceError{errHID}, op); err != nil {
		t.Fatal(err)
	}
	if n := len(dev.transfers(protocol.CmdSetButtons)); n != 1 {
		t.Fatalf("expected full redraw on second attempt, got %d", n)
	}

	s, _ = newTestSession(t, loopConfig)
	if err := s.recover("op", &deviceError{errHID}, func() error { return errors.New("bad icon") }); err != nil || s.offline {
		t.Fatalf("build error after reconnect must be logged only: err=%v offline=%v", err, s.offline)
	}
}

func TestRecoveryFailureGoesOfflineAndComesBack(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	dev.reconnects = []reconnectStep{{err: errors.New("gone")}, {err: errors.New("gone")}}
	if err := s.recover("op", &deviceError{errHID}, nil); err != nil {
		t.Fatal(err)
	}
	if !s.offline {
		t.Fatal("expected offline")
	}

	// First poll: device still missing.
	dev.reconnects = []reconnectStep{{err: errors.New("gone")}}
	s.pollOffline()
	if !s.offline || dev.tryN != 1 {
		t.Fatalf("offline=%v tries=%d", s.offline, dev.tryN)
	}
	// Too soon: no new attempt.
	s.pollOffline()
	if dev.tryN != 1 {
		t.Fatal("expected poll interval to be respected")
	}
	// Device back but the page upload fails.
	s.lastOffline = time.Time{}
	dev.sendErrs = []error{errHID}
	s.pollOffline()
	if !s.offline {
		t.Fatal("expected to stay offline when the page cannot be sent")
	}
	// Device back.
	s.lastOffline = time.Time{}
	dev.reset()
	s.pollOffline()
	if s.offline {
		t.Fatal("expected online")
	}
	if n := len(dev.transfers(protocol.CmdSetButtons)); n != 1 {
		t.Fatalf("expected full page after reconnect, got %d", n)
	}
}

func TestRecoveryExhaustedOnRetryErrors(t *testing.T) {
	s, _ := newTestSession(t, loopConfig)
	if err := s.recover("op", &deviceError{errHID}, func() error { return &deviceError{errHID} }); err != nil {
		t.Fatal(err)
	}
	if !s.offline {
		t.Fatal("expected offline after repeated failures")
	}
}

func TestOfflineLoopPollsUntilCancelled(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	s.offline = true
	ctx, cancel := context.WithCancel(context.Background())
	dev.reconnects = []reconnectStep{{err: errors.New("gone")}}
	go func() {
		eventually(t, func() bool { dev.mu.Lock(); defer dev.mu.Unlock(); return dev.tryN > 0 }, "offline poll")
		cancel()
	}()
	if err := s.run(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestReadErrorReconnectsWithoutRedraw(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	runReads(t, s, dev, readStep{err: errors.New("hid read: boom")})
	if dev.reconnectN != 1 {
		t.Fatalf("reconnects = %d", dev.reconnectN)
	}
	if n := len(dev.transfers(protocol.CmdSetButtons)); n != 0 {
		t.Fatalf("read error on a live device must not redraw, got %d", n)
	}
}

func TestDumpModeStopsOnReadError(t *testing.T) {
	dev := &fakeDeck{reads: []readStep{{err: errors.New("boom")}}}
	s := &session{dev: dev, out: &bytes.Buffer{}}
	if err := s.run(context.Background()); err == nil {
		t.Fatal("expected error in dump mode")
	}
}

func TestDumpModePrintsEvents(t *testing.T) {
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	dev := &fakeDeck{onIdle: cancel}
	unknown := protocol.EncodePacket(protocol.Cmd(0x0999), []byte{1})
	ack := protocol.EncodePacket(protocol.CmdInLabelStyleAck, nil)
	dev.reads = []readStep{{raw: press(3, true)}, {raw: []byte{1, 2, 3}}, {raw: ack[:]}, {raw: unknown[:]}}
	s := &session{dev: dev, out: &out}
	if err := s.run(ctx); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "BUTTON index=3") || !strings.Contains(got, "UNKNOWN cmd=0999") || strings.Contains(got, "LABEL_STYLE_ACK") {
		t.Fatalf("unexpected output %q", got)
	}
}

func TestKeepaliveSendsSinglePacket(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	s.tx.setLastWrite(time.Now().Add(-time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	dev.onIdle = cancel
	if err := s.run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(dev.cmds()); got != fmt.Sprint([]protocol.Cmd{protocol.CmdSetBrightness}) {
		t.Fatalf("keepalive sent %v", got)
	}
}

func TestKeepaliveFailureRecovers(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	dev.sendErrs = []error{errHID}
	if err := s.do("keepalive", s.keepalive); err != nil {
		t.Fatal(err)
	}
	if dev.reconnectN != 1 || len(dev.transfers(protocol.CmdSetBrightness)) != 1 {
		t.Fatalf("reconnects=%d cmds=%v", dev.reconnectN, dev.cmds())
	}
}

func TestPowerSleepWakeShutdown(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	if s.handlePower(powerEventSleep) || !s.sleeping {
		t.Fatal("expected sleeping")
	}
	sent := dev.transfers(protocol.CmdSetBrightness)
	if len(sent) != 1 || string(sent[0].data) != "0" || dev.closed != 1 {
		t.Fatalf("expected dim + close, got %+v closed=%d", sent, dev.closed)
	}

	dev.reset()
	dev.reconnects = []reconnectStep{{err: errors.New("not yet")}}
	if s.handlePower(powerEventWake) || s.sleeping || s.offline {
		t.Fatalf("sleeping=%v offline=%v", s.sleeping, s.offline)
	}
	if n := len(dev.transfers(protocol.CmdSetButtons)); n != 1 {
		t.Fatalf("expected full page after wake, got %d", n)
	}

	if !s.handlePower(powerEventShutdown) {
		t.Fatal("shutdown must stop the loop")
	}
}

func TestWakeFailureGoesOffline(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	for i := 0; i < wakeRecoverRetries; i++ {
		dev.reconnects = append(dev.reconnects, reconnectStep{err: errors.New("gone")})
	}
	s.handlePower(powerEventWake)
	if !s.offline {
		t.Fatal("expected offline after failed wake")
	}
	if err := (&session{dev: &fakeDeck{}}).restoreAfterWake(); err != nil {
		t.Fatalf("dump mode wake: %v", err)
	}
}

func TestWakeRetriesPageUpload(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	dev.sendErrs = []error{errHID}
	if err := s.restoreAfterWake(); err != nil {
		t.Fatal(err)
	}
	if dev.reconnectN != 2 {
		t.Fatalf("reconnects = %d", dev.reconnectN)
	}
}

func TestPowerEventsDrivenByLoop(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	ch := make(chan powerEvent, 2)
	s.power = ch
	ch <- powerEventSleep
	ch <- powerEventShutdown
	done := make(chan error, 1)
	go func() { done <- s.run(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not stop the loop")
	}
	if dev.closed < 2 {
		t.Fatalf("expected device closed for sleep and shutdown, got %d", dev.closed)
	}

	// A closed power channel is ignored.
	s, dev = newTestSession(t, loopConfig)
	closed := make(chan powerEvent)
	close(closed)
	s.power = closed
	runReads(t, s, dev)
	if s.power != nil {
		t.Fatal("expected closed power channel to be dropped")
	}
}

func TestReloadConfigSwapsStateAndRedraws(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	reloader := newConfigReloader(s.cfgPath, nil)
	defer reloader.Close()

	next := strings.Replace(loopConfig, "label: Run", "label: Walk", 1)
	if err := os.WriteFile(s.cfgPath, []byte(next), 0o600); err != nil {
		t.Fatal(err)
	}
	oldWidgets := s.widgets
	s.reloadConfig(reloader)
	if s.widgets == oldWidgets {
		t.Fatal("expected a new widget manager")
	}
	if n := len(dev.transfers(protocol.CmdSetButtons)); n != 1 {
		t.Fatalf("expected full page after reload, got %d", n)
	}
	if s.cfg.PagesOrDefault()[0].Buttons[1].Label != "Walk" {
		t.Fatal("config not swapped")
	}
}

func TestReloadConfigKeepsOldStateOnErrors(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	reloader := newConfigReloader(s.cfgPath, nil)
	defer reloader.Close()
	oldCfg := s.cfg

	if err := os.WriteFile(s.cfgPath, []byte("pages: [ broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.reloadConfig(reloader)
	if s.cfg != oldCfg || len(dev.cmds()) != 0 {
		t.Fatal("invalid config must be ignored")
	}

	// Valid config, but the device fails: old config kept, device recovered.
	if err := os.WriteFile(s.cfgPath, []byte(loopConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	dev.sendErrs = []error{errHID}
	s.reloadConfig(reloader)
	if s.cfg != oldCfg {
		t.Fatal("config must not be swapped when the device rejects it")
	}
	if dev.reconnectN != 1 {
		t.Fatalf("expected recovery, reconnects=%d", dev.reconnectN)
	}
}

func TestReloadTriggeredByLoop(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deadline := time.Now().Add(5 * time.Second)
	wrote := false
	dev.onIdle = func() {
		if !wrote {
			wrote = true
			// Size changes too, so the reloader notices regardless of
			// mtime granularity.
			next := strings.Replace(loopConfig, "label: Run", "label: Walk", 1) + "\n# changed\n"
			if err := os.WriteFile(s.cfgPath, []byte(next), 0o600); err != nil {
				t.Error(err)
			}
		}
		if s.cfg.PagesOrDefault()[0].Buttons[1].Label == "Walk" || time.Now().After(deadline) {
			cancel()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.run(ctx); err != nil {
		t.Fatal(err)
	}
	if s.cfg.PagesOrDefault()[0].Buttons[1].Label != "Walk" {
		t.Fatal("loop did not reload the config")
	}
}

func TestDispatchSyncActionErrorIsLogged(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	old := runExternalAction
	runExternalAction = func(*config.Action) error { return errors.New("nope") }
	defer func() { runExternalAction = old }()
	s.actions = nil
	if err := s.dispatch(&config.GestureBinding{Type: "shell", Cmd: "x"}); err != nil {
		t.Fatal(err)
	}
	if len(dev.cmds()) != 0 {
		t.Fatal("failed action must not redraw")
	}
	if err := s.dispatch(nil); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareDeviceForSleepNil(t *testing.T) {
	prepareDeviceForSleep(nil)
}
