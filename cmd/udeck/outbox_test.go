package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
	"github.com/alexbabintsev/udeck/internal/protocol"
)

func newTestOutbox(t *testing.T, dev *fakeDeck) *outbox {
	t.Helper()
	o := newOutbox(dev)
	t.Cleanup(o.close)
	return o
}

func key(col int, label string) config.Button {
	return config.Button{At: [2]int{col, 0}, Label: label}
}

func labelsSent(dev *fakeDeck) []string {
	var out []string
	for _, s := range dev.transfers(protocol.CmdPartiallyUpdateButtons) {
		out = append(out, fmt.Sprint(s.slots))
	}
	return out
}

func TestOutboxCoalescesUpdatesWhileDeviceIsBusy(t *testing.T) {
	gate := make(chan struct{})
	dev := &fakeDeck{gate: gate}
	o := newTestOutbox(t, dev)

	o.queueButtons([]config.Button{key(0, "first")})
	// The first transfer is stuck in the device; meanwhile the key
	// changes three times and another key once.
	time.Sleep(20 * time.Millisecond)
	o.queueButtons([]config.Button{key(0, "a")})
	o.queueButtons([]config.Button{key(0, "b")})
	o.queueButtons([]config.Button{key(0, "c"), key(1, "x")})
	close(gate)
	o.drain()

	got := labelsSent(dev)
	if len(got) != 2 || got[1] != "[0_0 1_0]" {
		t.Fatalf("expected the first upload plus one coalesced upload, got %v", got)
	}
}

func TestOutboxSkipsKeysAlreadyOnDevice(t *testing.T) {
	dev := &fakeDeck{}
	o := newTestOutbox(t, dev)
	o.queueButtons([]config.Button{key(0, "on")})
	o.drain()
	o.queueButtons([]config.Button{key(0, "on")})
	o.drain()
	if n := len(labelsSent(dev)); n != 1 {
		t.Fatalf("unchanged key must not be re-sent, got %d uploads", n)
	}

	// A widget icon file rewritten in place is a change.
	icon := filepath.Join(t.TempDir(), "w.png")
	writePNG(t, icon, 1)
	o.queueButtons([]config.Button{{At: [2]int{2, 0}, Icon: icon}})
	o.drain()
	writePNG(t, icon, 2)
	o.queueButtons([]config.Button{{At: [2]int{2, 0}, Icon: icon}})
	o.drain()
	if n := len(labelsSent(dev)); n != 3 {
		t.Fatalf("rewritten icon must be re-sent, got %d uploads", n)
	}

	// A full page resets what the device shows.
	if err := o.sendPage(&config.Config{}, []config.Button{key(0, "page")}); err != nil {
		t.Fatal(err)
	}
	o.queueButtons([]config.Button{key(0, "on")})
	o.drain()
	if n := len(labelsSent(dev)); n != 4 {
		t.Fatalf("key changed by the page must be re-sent, got %d", n)
	}
}

func TestOutboxPageSupersedesAndAbsorbsPartials(t *testing.T) {
	gate := make(chan struct{})
	dev := &fakeDeck{gate: gate}
	o := newTestOutbox(t, dev)

	o.queueButtons([]config.Button{key(0, "busy")})
	time.Sleep(20 * time.Millisecond)
	o.queueButtons([]config.Button{key(1, "stale")})
	o.queuePage(&config.Config{}, []config.Button{key(0, "p1")})
	o.queuePage(&config.Config{}, []config.Button{key(0, "p2"), key(2, "old")})
	o.queueButtons([]config.Button{key(2, "new")})
	close(gate)
	o.drain()

	if pages := dev.transfers(protocol.CmdSetButtons); len(pages) != 1 {
		t.Fatalf("expected only the newest page, got %d", len(pages))
	}
	if got := labelsSent(dev); len(got) != 1 {
		t.Fatalf("queued partials must be folded into the page, got %v", got)
	}
}

func TestOutboxReportsDeviceFailuresAndBuildErrors(t *testing.T) {
	dev := &fakeDeck{sendErrs: []error{errors.New("general error")}}
	o := newTestOutbox(t, dev)
	o.queueButtons([]config.Button{key(0, "x")})
	select {
	case f := <-o.Failures():
		if !isDeviceError(f.err) || len(f.job.buttons) != 1 {
			t.Fatalf("unexpected failure %+v", f)
		}
		if err := o.resend(f.job); err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("failure not reported")
	}

	// A broken icon is logged, not reported as a device failure.
	o.queueButtons([]config.Button{{At: [2]int{1, 0}, Icon: "/missing.png"}})
	o.drain()
	select {
	case f := <-o.Failures():
		t.Fatalf("build error reported as device failure: %v", f.err)
	default:
	}
}

func TestOutboxKeepaliveSkippedDuringTransferAndDiscard(t *testing.T) {
	gate := make(chan struct{})
	dev := &fakeDeck{gate: gate}
	o := newTestOutbox(t, dev)
	o.queueButtons([]config.Button{key(0, "x")})
	time.Sleep(20 * time.Millisecond)
	pkt, _ := protocol.SetBrightnessPacket(10)
	if sent, err := o.trySendPacket(pkt); sent || err != nil {
		t.Fatalf("keepalive must be skipped while busy: sent=%v err=%v", sent, err)
	}
	o.queueButtons([]config.Button{key(1, "dropped")})
	o.discard()
	close(gate)
	o.drain()
	if got := labelsSent(dev); len(got) != 1 {
		t.Fatalf("discarded work was sent: %v", got)
	}
	if sent, err := o.trySendPacket(pkt); !sent || err != nil {
		t.Fatal("idle keepalive must be sent")
	}
	if o.sinceWrite() > time.Second {
		t.Fatal("write time not tracked")
	}
}

// Regression: while the device refused writes for seconds, button
// presses were not read at all.
func TestButtonsAreHandledWhileDeviceWriteIsStalled(t *testing.T) {
	got := stubActions(t)
	s, dev := newTestSession(t, loopConfig)
	gate := make(chan struct{})
	dev.mu.Lock()
	dev.gate = gate
	dev.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A toggle press queues an upload that hangs in the device...
	dev.reads = append(tap(idxToggle), tap(idxRun)...)
	dev.onIdle = func() {
		// ...yet the next press still runs its action.
		if len(got()) >= 2 {
			cancel()
		}
		time.Sleep(time.Millisecond)
	}
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("button presses were blocked by a stalled device write")
	}
	close(gate)
	s.tx.drain()
	if acts := got(); len(acts) != 2 || acts[1].Cmd != "echo hi" {
		t.Fatalf("actions = %+v", acts)
	}
}

func TestLoopRecoversFromAsyncTransferFailure(t *testing.T) {
	s, dev := newTestSession(t, loopConfig)
	dev.sendErrs = []error{errHID}
	runReads(t, s, dev, append(tap(idxToggle), readStep{before: func() { time.Sleep(30 * time.Millisecond) }})...)
	if dev.reconnectN != 1 {
		t.Fatalf("reconnects = %d", dev.reconnectN)
	}
	if n := len(dev.transfers(protocol.CmdPartiallyUpdateButtons)); n != 1 {
		t.Fatalf("failed upload must be resent once, got %d", n)
	}
}

func writePNG(t *testing.T, path string, n byte) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{n, n, n, 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// drain waits until the queue is empty and no transfer is running.
func (o *outbox) drain() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for o.busy || o.pendingPage != nil || len(o.pending) > 0 {
		o.cond.Wait()
	}
}

func (o *outbox) setLastWrite(t time.Time) { o.lastWrite.Store(t.UnixNano()) }
