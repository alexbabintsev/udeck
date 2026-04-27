package device

import (
	"errors"
	"fmt"
	"time"

	hid "github.com/sstallion/go-hid"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
)

const (
	VendorID  = 0x2207
	ProductID = 0x0019
)

// Device wraps a HID handle for the Ulanzi D200.
type Device struct {
	dev *hid.Device
}

// Open initializes the hidapi runtime (idempotent) and opens the first
// matching D200. The device must not be claimed by Ulanzi Studio.
func Open() (*Device, error) {
	if err := hid.Init(); err != nil {
		return nil, fmt.Errorf("hid init: %w", err)
	}
	dev, err := hid.OpenFirst(VendorID, ProductID)
	if err != nil {
		return nil, fmt.Errorf("hid open %04x:%04x: %w", VendorID, ProductID, err)
	}
	return &Device{dev: dev}, nil
}

func (d *Device) Close() error {
	if d == nil || d.dev == nil {
		return nil
	}
	return d.dev.Close()
}

// SendPacket writes a single 1024-byte HID frame to the device. macOS
// hidapi requires a leading 0x00 report-ID byte for devices that don't
// use numbered reports, so the wire write is 1025 bytes total.
func (d *Device) SendPacket(pkt [protocol.PacketSize]byte) error {
	buf := make([]byte, 1+protocol.PacketSize)
	// buf[0] is left at 0x00 (report ID).
	copy(buf[1:], pkt[:])
	n, err := d.dev.Write(buf)
	if err != nil {
		return fmt.Errorf("hid write: %w", err)
	}
	if n != len(buf) {
		return fmt.Errorf("short hid write: %d/%d", n, len(buf))
	}
	return nil
}

// ReadPacket reads up to PacketSize bytes from the device. Returns 0 and
// no error on timeout.
func (d *Device) ReadPacket(timeout time.Duration) ([]byte, error) {
	buf := make([]byte, protocol.PacketSize)
	n, err := d.dev.ReadWithTimeout(buf, timeout)
	if err != nil {
		if errors.Is(err, hid.ErrTimeout) {
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
