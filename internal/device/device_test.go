package device

import (
	"errors"
	"testing"
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
