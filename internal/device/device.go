package device

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	hid "github.com/sstallion/go-hid"

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
)

// Device wraps a HID handle for the Ulanzi D200.
type Device struct {
	mu  sync.Mutex
	dev *hid.Device
}

// Open initializes the hidapi runtime (idempotent) and opens the first
// matching D200. The device must not be claimed by Ulanzi Studio.
func Open() (*Device, error) {
	if err := hid.Init(); err != nil {
		return nil, fmt.Errorf("hid init: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < openRetries; attempt++ {
		dev, err := hid.OpenFirst(VendorID, ProductID)
		if err == nil {
			return &Device{dev: dev}, nil
		}
		lastErr = err
		if !shouldRetryOpen(err) || attempt == openRetries-1 {
			break
		}
		time.Sleep(openRetryBackoff(attempt))
	}
	return nil, fmt.Errorf("hid open %04x:%04x: %w", VendorID, ProductID, lastErr)
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
// This is a defensive escape hatch for cases where macOS power
// transitions leave the underlying handle in a bad state and hid_close
// itself may crash inside cgo. The OS will reclaim the descriptor when
// the process exits.
func (d *Device) Detach() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.dev = nil
	d.mu.Unlock()
}

// reopen swaps the underlying hid handle for a freshly opened one to
// the same VID/PID. macOS hidapi occasionally tears down an
// IOHIDDevice client ("Device is disconnected") while the physical
// device stays connected and in host mode — the screen contents
// survive, only the kernel-side handle is dead. Reopening in place
// lets writes resume without going through the full recoverDevice
// path (which would re-upload the page ZIP and visibly flash all
// keys).
func (d *Device) reopen() error {
	fresh, err := hid.OpenFirst(VendorID, ProductID)
	if err != nil {
		return err
	}
	d.mu.Lock()
	old := d.dev
	d.dev = fresh
	d.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

func isStaleHandleErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "disconnected") ||
		strings.Contains(msg, "no such device")
}

// SendPacket writes a single 1024-byte HID frame to the device. macOS
// hidapi requires a leading 0x00 report-ID byte for devices that don't
// use numbered reports, so the wire write is 1025 bytes total.
func (d *Device) SendPacket(pkt [protocol.PacketSize]byte) error {
	buf := make([]byte, 1+protocol.PacketSize)
	// buf[0] is left at 0x00 (report ID).
	copy(buf[1:], pkt[:])

	reopenUsed := false
	var lastErr error
	for attempt := 0; attempt < writeRetries; attempt++ {
		d.mu.Lock()
		dev := d.dev
		d.mu.Unlock()
		if dev == nil {
			return errors.New("device detached")
		}
		n, err := dev.Write(buf)
		if err == nil {
			if n != len(buf) {
				return fmt.Errorf("short hid write: %d/%d", n, len(buf))
			}
			return nil
		}
		lastErr = err
		if isStaleHandleErr(err) && !reopenUsed {
			reopenUsed = true
			if rerr := d.reopen(); rerr != nil {
				return fmt.Errorf("hid write: %w (reopen: %v)", err, rerr)
			}
			continue
		}
		if !shouldRetryWrite(err) || attempt == writeRetries-1 {
			break
		}
		time.Sleep(writeRetryDelay)
	}
	return fmt.Errorf("hid write: %w", lastErr)
}

// SendPackets writes a sequence of HID frames (used for multi-packet
// transfers such as OUT_SET_BUTTONS).
func (d *Device) SendPackets(pkts [][protocol.PacketSize]byte) error {
	for _, pkt := range pkts {
		if err := d.SendPacket(pkt); err != nil {
			return err
		}
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
			if rerr := d.reopen(); rerr != nil {
				return nil, fmt.Errorf("hid read: %w (reopen: %v)", err, rerr)
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
		cmd, length, err := protocol.DecodeHeader(raw)
		if err != nil {
			continue
		}
		if cmd != protocol.CmdInDeviceInfo {
			continue
		}
		if int(length)+protocol.HeaderSize > len(raw) {
			length = uint32(len(raw) - protocol.HeaderSize)
		}
		payload := raw[protocol.HeaderSize : protocol.HeaderSize+length]
		// trim trailing zero bytes
		for i := len(payload) - 1; i >= 0; i-- {
			if payload[i] != 0 {
				payload = payload[:i+1]
				break
			}
		}
		return string(payload), nil
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
		strings.Contains(msg, "busy")
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
