package config

import (
	"path/filepath"
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

func TestValidateRejectsButtonWithActionAndDirectory(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At:        [2]int{0, 0},
				Action:    &Action{Type: "shell", Cmd: "echo hi"},
				Directory: &Directory{Buttons: []Button{{At: [2]int{1, 0}, Label: "Nested"}}},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "cannot combine action, directory, and widget") {
		t.Fatalf("expected action/directory validation error, got %v", err)
	}
}

func TestValidateRejectsDirectoryBackSlotConflict(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At:    [2]int{0, 0},
				Label: "Folder",
				Directory: &Directory{
					Buttons: []Button{
						{At: [2]int{3, 2}, Label: "Conflicts"},
					},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected back slot conflict validation error")
	}
	if !strings.Contains(err.Error(), "reserved Back button slot") {
		t.Fatalf("expected back slot conflict error, got %v", err)
	}
}

func TestValidateAcceptsNestedDirectory(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At:    [2]int{0, 0},
				Label: "Folder",
				Directory: &Directory{
					Buttons: []Button{
						{
							At:     [2]int{0, 0},
							Label:  "Nested",
							Action: &Action{Type: "key", Key: "space"},
						},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected nested directory to validate, got %v", err)
	}
}

func TestLoadExampleTestConfig(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "test-config.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected example test config to load, got %v", err)
	}
	if len(cfg.Buttons) == 0 {
		t.Fatal("expected example test config to contain buttons")
	}
}

func TestValidateRejectsHTTPActionWithoutURL(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At:     [2]int{0, 0},
				Action: &Action{Type: "http"},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected http action validation error")
	}
	if !strings.Contains(err.Error(), "action.url is empty") {
		t.Fatalf("expected missing url error, got %v", err)
	}
}

func TestValidateRejectsUnknownWidgetType(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At:     [2]int{0, 0},
				Widget: &Widget{Type: "unknown"},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected widget validation error")
	}
	if !strings.Contains(err.Error(), `widget.type "unknown" unknown`) {
		t.Fatalf("expected unknown widget error, got %v", err)
	}
}

func TestValidateRejectsCombiningWidgetAndAction(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At:     [2]int{0, 0},
				Action: &Action{Type: "shell", Cmd: "echo hi"},
				Widget: &Widget{Type: "memory"},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected widget/action validation error")
	}
	if !strings.Contains(err.Error(), "cannot combine action, directory, and widget") {
		t.Fatalf("expected widget/action combination error, got %v", err)
	}
}
