package device

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	hid "github.com/sstallion/go-hid"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
)

const (
	VendorID  = 0x2207
	ProductID = 0x0019

	openRetries     = 20
	openRetryDelay  = 250 * time.Millisecond
	openRetryMax    = 2 * time.Second
	writeRetries    = 8
	writeRetryDelay = 60 * time.Millisecond

	// slowWriteThreshold flags HID writes the device is slow to accept
	// in debug logs; a healthy write takes a few milliseconds.
	slowWriteThreshold = 50 * time.Millisecond

	transferRetries    = 5
	transferRetryDelay = 150 * time.Millisecond

	// deviceResetThreshold separates a real USB re-enumeration (the D200
	// is off the bus for seconds while it reboots) from the transient
	// "no HID devices found" that macOS reports for a few hundred
	// milliseconds while a torn-down IOHIDDevice client is replaced.
	deviceResetThreshold = 2 * time.Second
)

// ErrDeviceReset reports that the D200 vanished from the bus and came
// back (USB re-enumeration, replug, firmware reset). Its screen and host
// mode state are gone, so the caller must upload the full page again.
var ErrDeviceReset = errors.New("device was reset")

// Device wraps a HID handle for the Ulanzi D200.
type Device struct {
	mu   sync.Mutex
	dev  hidHandle
	path string // device path of the open handle, "" if unknown
}

// Seams for tests: the real hidapi calls and sleeping.
var (
	hidInit = hid.Init
	// openFirst also returns the platform device path. On macOS it is
	// "DevSrvsID:<IORegistry entry ID>", which changes whenever the
	// device re-enumerates (reboot, replug).
	openFirst = func() (hidHandle, string, error) {
		dev, err := hid.OpenFirst(VendorID, ProductID)
		if err != nil {
			return nil, "", err
		}
		path := ""
		if info, err := dev.GetDeviceInfo(); err == nil {
			path = info.Path
		}
		return dev, path, nil
	}
	sleep = time.Sleep
	now   = time.Now
)

// hidHandle is the subset of *hid.Device used here; tests substitute it.
type hidHandle interface {
	Write(b []byte) (int, error)
	ReadWithTimeout(b []byte, timeout time.Duration) (int, error)
	Close() error
}

// Open initializes the hidapi runtime (idempotent) and opens the first
// matching D200. The device must not be claimed by Ulanzi Studio.
func Open() (*Device, error) {
	if err := hidInit(); err != nil {
		return nil, fmt.Errorf("hid init: %w", err)
	}
	dev, path, _, err := openWithRetry(openRetries)
	if err != nil {
		return nil, err
	}
	return &Device{dev: dev, path: path}, nil
}

// openWithRetry opens the first D200. missing reports that the device
// was absent from the bus for at least deviceResetThreshold, which means
// it was re-enumerated and lost its screen state.
func openWithRetry(attempts int) (dev hidHandle, path string, missing bool, err error) {
	var lastErr error
	var firstMissing time.Time
	for attempt := 0; attempt < attempts; attempt++ {
		dev, path, err := openFirst()
		if err == nil {
			missing = !firstMissing.IsZero() && now().Sub(firstMissing) >= deviceResetThreshold
			if attempt > 0 {
				debug.Logf("hid open succeeded after %d attempts (device reset=%v)", attempt+1, missing)
			}
			return dev, path, missing, nil
		}
		lastErr = err
		if isNotFoundErr(err) && firstMissing.IsZero() {
			firstMissing = now()
		}
		debug.Logf("hid open attempt %d/%d failed: %v", attempt+1, attempts, err)
		if !shouldRetryOpen(err) || attempt == attempts-1 {
			break
		}
		sleep(openRetryBackoff(attempt))
	}
	return nil, "", !firstMissing.IsZero(), fmt.Errorf("hid open %04x:%04x: %w", VendorID, ProductID, lastErr)
}

func (d *Device) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	dev := d.dev
	d.dev = nil
	d.mu.Unlock()
	if dev == nil {
		return nil
	}
	return dev.Close()
}

// Detach marks the HID handle unusable without calling into hid_close.
// It leaks the handle — and because macOS hidapi opens devices
// exclusively, a leaked handle blocks every later open with a privilege
// violation. Only use it when hid_close itself is unsafe to call.
func (d *Device) Detach() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.dev = nil
	d.mu.Unlock()
}

// Reconnect closes the current handle and opens a fresh one to the same
// VID/PID. reset reports that the device disappeared from the bus in the
// meantime and must be re-initialized with a full page upload; when it is
// false the device kept its screen and host mode, and only the request
// that failed needs to be repeated.
func (d *Device) Reconnect() (reset bool, err error) {
	return d.reopenWith(openRetries)
}

// TryReconnect is Reconnect with a single open attempt, for polling a
// device that is unplugged without blocking the caller for long.
func (d *Device) TryReconnect() (reset bool, err error) {
	return d.reopenWith(1)
}

// reopen swaps the underlying hid handle for a freshly opened one.
// macOS hidapi occasionally tears down an IOHIDDevice client ("Device is
// disconnected") while the physical device stays connected and in host
// mode — the screen contents survive, only the kernel-side handle is
// dead. Reopening in place lets I/O resume without re-uploading the page
// ZIP, which would visibly flash all keys.
func (d *Device) reopen() (missing bool, err error) {
	return d.reopenWith(openRetries)
}

func (d *Device) reopenWith(attempts int) (missing bool, err error) {
	d.mu.Lock()
	old := d.dev
	d.dev = nil
	d.mu.Unlock()
	if old != nil {
		// Close is required: the open is exclusive on macOS, so a leaked
		// handle would make the reopen below fail with 0xE00002C1.
		if cerr := old.Close(); cerr != nil {
			debug.Logf("hid close before reopen: %v", cerr)
		}
	}
	fresh, path, missing, err := openWithRetry(attempts)
	if err != nil {
		debug.Logf("hid reopen failed: %v", err)
		return missing, err
	}
	d.mu.Lock()
	// A new IORegistry path means the device re-enumerated: it rebooted
	// and lost its page even if it was back within milliseconds.
	if d.path != "" && path != "" && path != d.path {
		missing = true
	}
	d.dev = fresh
	d.path = path
	d.mu.Unlock()
	debug.Logf("hid handle reopened (path %s, device reset=%v)", path, missing)
	return missing, nil
}

func isStaleHandleErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "disconnected") ||
		strings.Contains(msg, "no such device")
}

// SendPacket writes a single 1024-byte HID frame to the device, retrying
// transient macOS write errors. Use it only for self-contained
// single-packet commands: retrying a frame whose failed write may still
// have reached the device is harmless there, but corrupts a multi-packet
// transfer (see SendPackets).
func (d *Device) SendPacket(pkt [protocol.PacketSize]byte) error {
	reopenUsed := false
	var lastErr error
	for attempt := 0; attempt < writeRetries; attempt++ {
		err := d.writeFrame(pkt)
		if err == nil {
			return nil
		}
		lastErr = err
		debug.Logf("hid write attempt %d/%d failed: %v", attempt+1, writeRetries, err)
		if isStaleHandleErr(err) && !reopenUsed {
			reopenUsed = true
			if err := d.reopenAfter(err); err != nil {
				return err
			}
			continue
		}
		if !shouldRetryWrite(err) || attempt == writeRetries-1 {
			break
		}
		sleep(writeRetryDelay)
	}
	return fmt.Errorf("hid write: %w", lastErr)
}

// SendPackets writes a multi-packet transfer (OUT_SET_BUTTONS and
// friends). A failed write is never repeated in place: macOS can report
// an error for a frame the device did receive, and a duplicated or
// missing continuation chunk silently corrupts the ZIP, so the device
// drops the whole page and keeps showing the old one. Instead the whole
// transfer is restarted from its header frame, which resynchronises the
// device-side parser.
func (d *Device) SendPackets(pkts [][protocol.PacketSize]byte) error {
	if len(pkts) == 1 {
		return d.SendPacket(pkts[0])
	}
	reopenUsed := false
	var lastErr error
	for attempt := 0; attempt < transferRetries; attempt++ {
		if attempt > 0 {
			// Give the device time to finish whatever made it reject the
			// frame (typically still unpacking the previous page).
			sleep(time.Duration(attempt) * transferRetryDelay)
		}
		i, err := d.writeTransfer(pkts)
		if err == nil {
			if attempt > 0 {
				debug.Logf("%d-packet transfer succeeded on attempt %d", len(pkts), attempt+1)
			}
			return nil
		}
		lastErr = err
		debug.Logf("transfer attempt %d/%d failed at packet %d/%d: %v",
			attempt+1, transferRetries, i+1, len(pkts), err)
		if isStaleHandleErr(err) && !reopenUsed {
			reopenUsed = true
			if err := d.reopenAfter(err); err != nil {
				return err
			}
		} else if !shouldRetryWrite(err) {
			break
		}
		if i > 0 {
			d.completeInterrupted(len(pkts) - i)
		}
	}
	return fmt.Errorf("hid write: %w", lastErr)
}

// completeInterrupted feeds the device the n packets it still expects
// from an interrupted transfer. The D200 does not resynchronise on a new
// header mid-transfer: it keeps counting bytes, so a resend would be
// swallowed as the tail of the broken upload and the device then crawls
// at ~1 packet/s (measured with `ulanzi-deck probe --recovery`: resend
// right away or after 1-3s stalled 9/9 times; filler first, 0/3). The
// failed packet is assumed lost; if it did arrive, one filler packet
// lands while the device is idle, which it ignores.
func (d *Device) completeInterrupted(n int) {
	if n <= 0 {
		return
	}
	var filler [protocol.PacketSize]byte
	for k := 0; k < n; k++ {
		if err := d.writeFrame(filler); err != nil {
			debug.Logf("filler packet %d/%d: %v", k+1, n, err)
			sleep(writeRetryDelay)
		}
	}
	debug.Logf("sent %d filler packets to finish the interrupted transfer", n)
}

func (d *Device) writeTransfer(pkts [][protocol.PacketSize]byte) (int, error) {
	for i, pkt := range pkts {
		if err := d.writeFrame(pkt); err != nil {
			return i, err
		}
	}
	return 0, nil
}

// writeFrame performs one HID write. macOS hidapi requires a leading 0x00
// report-ID byte for devices without numbered reports, so the wire write
// is 1025 bytes.
func (d *Device) writeFrame(pkt [protocol.PacketSize]byte) error {
	d.mu.Lock()
	dev := d.dev
	d.mu.Unlock()
	if dev == nil {
		return errors.New("device detached")
	}
	var buf [1 + protocol.PacketSize]byte
	copy(buf[1:], pkt[:])
	start := now()
	n, err := dev.Write(buf[:])
	if took := now().Sub(start); took > slowWriteThreshold {
		debug.Logf("hid write took %s (device busy?)", took.Round(time.Millisecond))
	}
	if err != nil {
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("short hid write: %d/%d", n, len(buf))
	}
	return nil
}

// reopenAfter replaces a stale handle after cause. It returns nil when
// the caller may retry, or the error to give up with.
func (d *Device) reopenAfter(cause error) error {
	missing, err := d.reopen()
	if err != nil {
		return fmt.Errorf("hid write: %w (reopen: %v)", cause, err)
	}
	if missing {
		return fmt.Errorf("hid write: %w: %w", cause, ErrDeviceReset)
	}
	return nil
}

// ReadPacket reads up to PacketSize bytes from the device. Returns 0 and
// no error on timeout.
func (d *Device) ReadPacket(timeout time.Duration) ([]byte, error) {
	d.mu.Lock()
	dev := d.dev
	d.mu.Unlock()
	if dev == nil {
		return nil, errors.New("device detached")
	}
	buf := make([]byte, protocol.PacketSize)
	n, err := dev.ReadWithTimeout(buf, timeout)
	if err != nil {
		if errors.Is(err, hid.ErrTimeout) {
			return nil, nil
		}
		// macOS hidapi sometimes returns -1 from cond_wait with the
		// message "hid_read_timeout: error waiting for more data" when
		// the wait is interrupted (e.g. during power transitions or
		// contention). The device is still healthy — treat it as a
		// soft poll miss rather than tearing down and reopening.
		if strings.Contains(err.Error(), "error waiting for more data") {
			return nil, nil
		}
		// "Device is disconnected" / similar can be returned by
		// macOS hidapi when the IOHIDDevice client was torn down even
		// though the physical device is still attached and in host
		// mode. Reopen the handle in place and treat the read as a
		// soft poll miss — the next loop iteration will read again.
		if isStaleHandleErr(err) {
			debug.Logf("hid read: stale handle: %v", err)
			missing, rerr := d.reopen()
			if rerr != nil {
				return nil, fmt.Errorf("hid read: %w (reopen: %v)", err, rerr)
			}
			if missing {
				return nil, fmt.Errorf("hid read: %w: %w", err, ErrDeviceReset)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("hid read: %w", err)
	}
	if n == 0 {
		return nil, nil
	}
	return buf[:n], nil
}

// Info reads up to 1 second of device info responses. Returns the first
// IN_DEVICE_INFO payload as a string, or an error if no info arrives.
func (d *Device) Info(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := d.ReadPacket(200 * time.Millisecond)
		if err != nil {
			return "", err
		}
		if len(raw) == 0 {
			continue
		}
		ev, err := protocol.ParseEvent(raw)
		if err != nil || ev.Kind != protocol.EventDeviceInfo {
			continue
		}
		return ev.Info, nil
	}
	return "", errors.New("no device info received within timeout")
}

func shouldRetryOpen(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "privilege violation") ||
		strings.Contains(msg, "resource busy") ||
		strings.Contains(msg, "busy") ||
		strings.Contains(msg, "no hid devices") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "0xe00002c1")
}

func isNotFoundErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no hid devices") || strings.Contains(msg, "not found")
}

func shouldRetryWrite(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "general error") ||
		strings.Contains(msg, "resource busy") ||
		strings.Contains(msg, "busy") ||
		strings.Contains(msg, "not responding") ||
		strings.Contains(msg, "not ready") ||
		strings.Contains(msg, "0xe00002ed") ||
		strings.Contains(msg, "0xe00002d8") ||
		strings.Contains(msg, "0xe0005000")
}

func openRetryBackoff(attempt int) time.Duration {
	delay := openRetryDelay + time.Duration(attempt)*150*time.Millisecond
	if delay > openRetryMax {
		return openRetryMax
	}
	return delay
}
