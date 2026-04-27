package config

import (
	"strings"
	"testing"
)

func TestValidateRejectsDuplicateButtonCoordinates(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{At: [2]int{0, 0}, Label: "A"},
			{At: [2]int{0, 0}, Label: "B"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected duplicate coordinate validation error")
	}
	if !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("expected duplicate coordinate error, got %v", err)
	}
}

func TestValidateRejectsInvalidLabelColor(t *testing.T) {
	cfg := &Config{
		Device: DeviceConfig{
			LabelStyle: &LabelStyle{Color: "GGGGGG"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected invalid color validation error")
	}
	if !strings.Contains(err.Error(), "device.label_style.color") {
		t.Fatalf("expected color validation error, got %v", err)
	}
}

func TestValidateAcceptsHashPrefixedLabelColor(t *testing.T) {
	cfg := &Config{
		Device: DeviceConfig{
			LabelStyle: &LabelStyle{Color: "#A1B2C3"},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid color, got %v", err)
	}
}
