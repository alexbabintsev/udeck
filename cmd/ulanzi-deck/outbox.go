package main

import (
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
)

// Pacing between transfers. The D200 unpacks every page ZIP on its own
// MCU; while it is busy it stops accepting HID writes for seconds
// (IOHIDDeviceSetReport 0xE00002BC). Leaving it time proportional to the
// transfer size keeps bursts from piling up.
var (
	transferGapPerPacket = 15 * time.Millisecond
	transferGapMax       = 400 * time.Millisecond
)

// sendJob is one page (full) or set of keys (partial) to upload.
type sendJob struct {
	page    bool
	cfg     *config.Config // page jobs: label style and brightness
	buttons []config.Button
}

type sendFailure struct {
	job sendJob
	err error
}

// outbox owns all writes to the device. The event loop queues work and
// keeps reading button events while a transfer is in flight, so a
// device that stalls on writes no longer delays key presses.
//
// Queued work is coalesced: a newer page replaces an older one, a newer
// image of a key replaces the queued one, and a key whose content is
// already on the device is not sent again. Fast repeated presses thus
// result in one transfer of the final state.
type outbox struct {
	dev deck

	// txMu serialises transfers (packets of two uploads must never
	// interleave) and guards lastSent.
	txMu     sync.Mutex
	lastSent map[[2]int]string

	mu          sync.Mutex
	cond        *sync.Cond
	pendingPage *sendJob
	pending     map[[2]int]config.Button
	queuedAt    time.Time // oldest queued, not yet taken work (debug)
	busy        bool

	lastWrite atomic.Int64 // unix nanos of the last successful write
	wake      chan struct{}
	failures  chan sendFailure
	done      chan struct{}
	wg        sync.WaitGroup
}

func newOutbox(dev deck) *outbox {
	o := &outbox{
		dev:      dev,
		lastSent: map[[2]int]string{},
		pending:  map[[2]int]config.Button{},
		wake:     make(chan struct{}, 1),
		failures: make(chan sendFailure, 4),
		done:     make(chan struct{}),
	}
	o.cond = sync.NewCond(&o.mu)
	o.touch()
	o.wg.Add(1)
	go o.loop()
	return o
}

func (o *outbox) close() {
	select {
	case <-o.done:
	default:
		close(o.done)
	}
	o.wg.Wait()
}

func (o *outbox) touch()                       { o.lastWrite.Store(time.Now().UnixNano()) }
func (o *outbox) sinceWrite() time.Duration    { return time.Since(time.Unix(0, o.lastWrite.Load())) }
func (o *outbox) Failures() <-chan sendFailure { return o.failures }

// queuePage schedules a full page upload, superseding queued work.
func (o *outbox) queuePage(cfg *config.Config, buttons []config.Button) {
	o.mu.Lock()
	o.pendingPage = &sendJob{page: true, cfg: cfg, buttons: buttons}
	clear(o.pending)
	o.markQueuedLocked()
	o.mu.Unlock()
	o.signal()
}

// queueButtons schedules a partial update of the given keys.
func (o *outbox) queueButtons(buttons []config.Button) {
	o.mu.Lock()
	for _, b := range buttons {
		if o.pendingPage != nil {
			// The queued page will carry the newest state of this key.
			for i := range o.pendingPage.buttons {
				if o.pendingPage.buttons[i].At == b.At {
					o.pendingPage.buttons[i] = b
				}
			}
			continue
		}
		o.pending[b.At] = b
	}
	o.markQueuedLocked()
	if o.busy {
		debug.Logf("queued %s behind a running transfer", slotList(buttons))
	}
	o.mu.Unlock()
	o.signal()
}

// discard drops queued work and stale failures (e.g. before sleep).
func (o *outbox) discard() {
	o.mu.Lock()
	o.pendingPage = nil
	clear(o.pending)
	o.mu.Unlock()
	for {
		select {
		case <-o.failures:
		default:
			return
		}
	}
}

// sendPage uploads a page synchronously, dropping queued work it makes
// obsolete. Used where the caller must know the outcome (startup,
// recovery, wake, config reload).
func (o *outbox) sendPage(cfg *config.Config, buttons []config.Button) error {
	o.mu.Lock()
	o.pendingPage = nil
	clear(o.pending)
	o.mu.Unlock()
	_, err := o.send(sendJob{page: true, cfg: cfg, buttons: buttons})
	return err
}

// resend repeats a failed job synchronously.
func (o *outbox) resend(j sendJob) error {
	_, err := o.send(j)
	return err
}

// trySendPacket writes one packet unless a transfer is in progress
// (keepalive: an active link needs none).
func (o *outbox) trySendPacket(pkt [protocol.PacketSize]byte) (bool, error) {
	if !o.txMu.TryLock() {
		return false, nil
	}
	defer o.txMu.Unlock()
	if err := o.dev.SendPacket(pkt); err != nil {
		return true, &deviceError{err}
	}
	o.touch()
	return true, nil
}

// exclusive runs fn with transfers blocked (handle reconnects).
func (o *outbox) exclusive(fn func()) {
	o.txMu.Lock()
	defer o.txMu.Unlock()
	fn()
}

func (o *outbox) markQueuedLocked() {
	if o.queuedAt.IsZero() {
		o.queuedAt = time.Now()
	}
}

func (o *outbox) signal() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *outbox) loop() {
	defer o.wg.Done()
	for {
		select {
		case <-o.done:
			return
		case <-o.wake:
		}
		for {
			j, ok := o.take()
			if !ok {
				break
			}
			start := time.Now()
			packets, err := o.send(j)
			o.finish()
			if packets > 0 || err != nil {
				debug.Logf("transfer of %s done in %s (err=%v)", slotList(j.buttons), time.Since(start).Round(time.Millisecond), err)
			}
			if err != nil {
				o.report(j, err)
				continue
			}
			gap := min(time.Duration(packets)*transferGapPerPacket, transferGapMax)
			if gap > 0 {
				select {
				case <-o.done:
					return
				case <-time.After(gap):
				}
			}
		}
	}
}

func (o *outbox) take() (sendJob, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.queuedAt.IsZero() && (o.pendingPage != nil || len(o.pending) > 0) {
		debug.Logf("starting transfer after %s in queue", time.Since(o.queuedAt).Round(time.Millisecond))
		o.queuedAt = time.Time{}
	}
	if o.pendingPage != nil {
		j := *o.pendingPage
		o.pendingPage = nil
		o.busy = true
		return j, true
	}
	if len(o.pending) == 0 {
		o.cond.Broadcast()
		return sendJob{}, false
	}
	buttons := make([]config.Button, 0, len(o.pending))
	for _, b := range o.pending {
		buttons = append(buttons, b)
	}
	sort.Slice(buttons, func(i, k int) bool { return buttons[i].HIDIndex() < buttons[k].HIDIndex() })
	clear(o.pending)
	o.busy = true
	return sendJob{buttons: buttons}, true
}

func (o *outbox) finish() {
	o.mu.Lock()
	o.busy = false
	o.cond.Broadcast()
	o.mu.Unlock()
}

func (o *outbox) report(j sendJob, err error) {
	if !isDeviceError(err) {
		log.Printf("update device: %v", err)
		return
	}
	select {
	case o.failures <- sendFailure{job: j, err: err}:
	default:
		// A recovery is already queued; it redraws what is needed.
	}
}

// send performs one job and returns the number of packets written.
func (o *outbox) send(j sendJob) (int, error) {
	o.txMu.Lock()
	defer o.txMu.Unlock()
	if j.page {
		return o.sendPageLocked(j.cfg, j.buttons)
	}
	return o.sendButtonsLocked(j.buttons)
}

func (o *outbox) sendPageLocked(cfg *config.Config, buttons []config.Button) (int, error) {
	pkts, err := protocol.SetButtonsFromConfig(buttons)
	if err != nil {
		return 0, fmt.Errorf("build page zip: %w", err)
	}
	extra, err := styleAndBrightness(cfg)
	if err != nil {
		return 0, err
	}
	debug.Logf("sending full page: %d buttons, %d packets", len(buttons), len(pkts))
	if err := o.dev.SendPackets(pkts); err != nil {
		return 0, &deviceError{fmt.Errorf("send page zip: %w", err)}
	}
	for _, pkt := range extra {
		if err := o.dev.SendPacket(pkt); err != nil {
			return 0, &deviceError{fmt.Errorf("apply label style/brightness: %w", err)}
		}
	}
	clear(o.lastSent)
	for _, b := range buttons {
		o.lastSent[b.At] = buttonSignature(b)
	}
	o.touch()
	return len(pkts) + len(extra), nil
}

func (o *outbox) sendButtonsLocked(buttons []config.Button) (int, error) {
	var changed []config.Button
	for _, b := range buttons {
		if o.lastSent[b.At] != buttonSignature(b) {
			changed = append(changed, b)
		}
	}
	if len(changed) == 0 {
		debug.Logf("partial update skipped: %s already up to date", slotList(buttons))
		return 0, nil
	}
	pkts, err := protocol.PartiallyUpdateButtonsFromConfig(changed)
	if err != nil {
		return 0, fmt.Errorf("build partial page zip: %w", err)
	}
	debug.Logf("partial update: %s, %d packets", slotList(changed), len(pkts))
	if err := o.dev.SendPackets(pkts); err != nil {
		return 0, &deviceError{fmt.Errorf("send partial page zip: %w", err)}
	}
	for _, b := range changed {
		o.lastSent[b.At] = buttonSignature(b)
	}
	o.touch()
	return len(pkts), nil
}

// slotList renders keys for logs as "[index:label-or-icon ...]".
func slotList(buttons []config.Button) string {
	parts := make([]string, 0, len(buttons))
	for _, b := range buttons {
		name := b.RuntimeID
		if name == "" {
			name = b.Label
		}
		parts = append(parts, fmt.Sprintf("%d:%s", b.HIDIndex(), name))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// styleAndBrightness returns the packets re-applied after every full
// page (the device resets them with the page).
func styleAndBrightness(cfg *config.Config) ([][protocol.PacketSize]byte, error) {
	var style *config.LabelStyle
	var brightness *int
	if cfg != nil {
		style, brightness = cfg.Device.LabelStyle, cfg.Device.Brightness
	}
	pkt, err := protocol.SetLabelStylePacket(style)
	if err != nil {
		return nil, fmt.Errorf("set label style: %w", err)
	}
	out := [][protocol.PacketSize]byte{pkt}
	if brightness != nil {
		pkt, err := protocol.SetBrightnessPacket(*brightness)
		if err != nil {
			return nil, err
		}
		out = append(out, pkt)
	}
	return out, nil
}

// buttonSignature identifies what a key shows: its label, icon settings
// and the current bytes of a file icon (widgets rewrite the same path).
func buttonSignature(b config.Button) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%v\x00", b.Label, b.Icon, b.IconColor, b.IconMargins)
	if b.Icon != "" && !icons.IsPackIcon(b.Icon) {
		if data, err := os.ReadFile(b.Icon); err == nil {
			h.Write(data)
		}
	}
	return fmt.Sprintf("%x", h.Sum64())
}
