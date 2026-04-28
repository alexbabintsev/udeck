package device

import (
	"errors"
	"testing"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
)

func TestShouldRetryOpenPrivilegeViolation(t *testing.T) {
	if !shouldRetryOpen(errors.New("hid_open_path: failed to open IOHIDDevice from mach entry: privilege violation")) {
		t.Fatal("expected privilege violation to be retried")
	}
}

func TestShouldRetryOpenBusy(t *testing.T) {
	if !shouldRetryOpen(errors.New("resource busy")) {
		t.Fatal("expected busy error to be retried")
	}
}

func TestShouldRetryOpenOtherErrors(t *testing.T) {
	if shouldRetryOpen(errors.New("device not found")) {
		t.Fatal("did not expect device not found to be retried")
	}
}

func TestShouldRetryWriteGeneralError(t *testing.T) {
	if !shouldRetryWrite(errors.New("IOHIDDeviceSetReport failed: general error")) {
		t.Fatal("expected general write error to be retried")
	}
}

func TestShouldRetryWriteOtherErrors(t *testing.T) {
	if shouldRetryWrite(errors.New("permission denied")) {
		t.Fatal("did not expect permission denied to be retried")
	}
}

func TestDetachMakesCloseAndIOSafe(t *testing.T) {
	d := &Device{}
	d.Detach()
	if err := d.Close(); err != nil {
		t.Fatalf("expected close after detach to be safe, got %v", err)
	}
	var pkt [protocol.PacketSize]byte
	if err := d.SendPacket(pkt); err == nil {
		t.Fatal("expected detached device send to fail")
	}
	if _, err := d.ReadPacket(0); err == nil {
		t.Fatal("expected detached device read to fail")
	}
}

func TestOpenRetryBackoffCaps(t *testing.T) {
	if got := openRetryBackoff(0); got != openRetryDelay {
		t.Fatalf("unexpected initial backoff: %v", got)
	}
	if got := openRetryBackoff(100); got != openRetryMax {
		t.Fatalf("expected capped backoff %v, got %v", openRetryMax, got)
	}
}
