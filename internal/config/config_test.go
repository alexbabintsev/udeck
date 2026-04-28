package config

import (
	"os"
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
		Buttons: []Button{{At: [2]int{0, 0}, Label: "A"}},
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
		Buttons: []Button{{At: [2]int{0, 0}, Label: "A"}},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid color, got %v", err)
	}
}

func TestValidateRejectsInvalidIconMargins(t *testing.T) {
	cfg := &Config{
		Device: DeviceConfig{
			IconMargins: &Insets{Left: 120, Right: 80},
		},
		Buttons: []Button{{At: [2]int{0, 0}, Label: "A"}},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected icon margins validation error")
	}
	if !strings.Contains(err.Error(), "device.icon_margins") {
		t.Fatalf("expected device icon margins error, got %v", err)
	}
}

func TestValidateAcceptsWideButtonIconMargins(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At:          [2]int{3, 2},
				Label:       "Wide",
				IconMargins: &Insets{Left: 200, Right: 200, Top: 20, Bottom: 20},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected wide button margins to validate, got %v", err)
	}
}

func TestValidateRejectsMissingBottomRightSlot(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{At: [2]int{4, 2}, Label: "Ghost"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected invalid slot validation error")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected out of range error, got %v", err)
	}
}

func TestButtonHIDIndexUsesWideBottomButton(t *testing.T) {
	btn := Button{At: [2]int{3, 2}}
	if got := btn.HIDIndex(); got != 13 {
		t.Fatalf("expected HID index 13 for [3,2], got %d", got)
	}
}

func TestValidateRejectsButtonWithActionAndFolder(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At:     [2]int{0, 0},
				On:     &Handlers{Tap: &GestureBinding{Type: "shell", Cmd: "echo hi"}},
				Folder: &Folder{Buttons: []Button{{At: [2]int{1, 0}, Label: "Nested"}}},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "cannot combine on, folder, and widget") {
		t.Fatalf("expected action/folder validation error, got %v", err)
	}
}

func TestValidateRejectsFolderBackSlotConflict(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At:    [2]int{0, 0},
				Label: "Folder",
				Folder: &Folder{
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

func TestValidateAcceptsNestedFolder(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At:    [2]int{0, 0},
				Label: "Folder",
				Folder: &Folder{
					Buttons: []Button{
						{
							At:    [2]int{0, 0},
							Label: "Nested",
							On:    &Handlers{Tap: &GestureBinding{Type: "key", Key: "space"}},
						},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected nested folder to validate, got %v", err)
	}
}

func TestLoadExampleTestConfig(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "test-config.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected example test config to load, got %v", err)
	}
	if len(cfg.PagesOrDefault()) == 0 || len(cfg.PagesOrDefault()[0].Buttons) == 0 {
		t.Fatal("expected example test config to contain buttons")
	}
}

func TestValidateRejectsMixingPagesAndButtons(t *testing.T) {
	cfg := &Config{
		Pages:   []Page{{Buttons: []Button{{At: [2]int{0, 0}, Label: "A"}}}},
		Buttons: []Button{{At: [2]int{1, 0}, Label: "B"}},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "both pages and buttons") {
		t.Fatalf("expected pages/buttons conflict, got %v", err)
	}
}

func TestValidateAcceptsPages(t *testing.T) {
	cfg := &Config{
		Pages: []Page{
			{Buttons: []Button{{At: [2]int{0, 0}, Label: "One"}}},
			{Buttons: []Button{{At: [2]int{1, 0}, Label: "Two"}}},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected pages config to validate, got %v", err)
	}
}

func TestLoadAppliesGlobalIconMarginsToButtons(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte(`
device:
  icon_margins:
    top: 10
    right: 11
    bottom: 12
    left: 13
buttons:
  - at: [0, 0]
    label: "One"
    icon: "one.png"
  - at: [1, 0]
    label: "Two"
    icon: "two.png"
    icon_margins:
      top: 1
      right: 2
      bottom: 3
      left: 4
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected config to load, got %v", err)
	}
	got := cfg.Buttons[0].IconMargins
	if got == nil || got.Top != 10 || got.Right != 11 || got.Bottom != 12 || got.Left != 13 {
		t.Fatalf("unexpected inherited icon margins: %#v", got)
	}
	got = cfg.Buttons[1].IconMargins
	if got == nil || got.Top != 1 || got.Right != 2 || got.Bottom != 3 || got.Left != 4 {
		t.Fatalf("unexpected overridden icon margins: %#v", got)
	}
}

func TestValidateRejectsHTTPActionWithoutURL(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At: [2]int{0, 0},
				On: &Handlers{Tap: &GestureBinding{Type: "http"}},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected http action validation error")
	}
	if !strings.Contains(err.Error(), "on.tap.url is empty") {
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
				On:     &Handlers{Tap: &GestureBinding{Type: "shell", Cmd: "echo hi"}},
				Widget: &Widget{Type: "memory"},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected widget/action validation error")
	}
	if !strings.Contains(err.Error(), "cannot combine on, folder, and widget") {
		t.Fatalf("expected widget/action combination error, got %v", err)
	}
}

func TestValidateAcceptsOnHandlers(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At: [2]int{0, 0},
				On: &Handlers{
					Tap:           &GestureBinding{Type: "shell", Cmd: "echo tap"},
					DoubleTap:     &GestureBinding{Type: "key", Key: "cmd t"},
					Hold:          &GestureBinding{Type: "http", URL: "http://127.0.0.1/test"},
					Repeat:        &GestureBinding{Type: "key", Key: "space"},
					HoldMS:        500,
					DoubleTapMS:   300,
					RepeatEveryMS: 120,
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected on handlers to validate, got %v", err)
	}
}

func TestValidateAcceptsToggleBinding(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At: [2]int{0, 0},
				On: &Handlers{
					Tap: &GestureBinding{
						Toggle: []ToggleStep{
							{Label: "One", Icon: "/tmp/one.png", Action: Action{Type: "shell", Cmd: "echo 1"}},
							{Label: "Two", Icon: "/tmp/two.png", Action: Action{Type: "shell", Cmd: "echo 2"}},
						},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected toggle binding to validate, got %v", err)
	}
}

func TestValidateAcceptsChainBinding(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At: [2]int{0, 0},
				On: &Handlers{
					Tap: &GestureBinding{
						Chain: []ChainStep{
							{Action: &Action{Type: "shell", Cmd: "echo 1"}},
							{DelayMS: 250},
							{Action: &Action{Type: "http", URL: "http://127.0.0.1/test"}},
						},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected chain binding to validate, got %v", err)
	}
}

func TestValidateRejectsGestureBindingCombiningChainAndDirectAction(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At: [2]int{0, 0},
				On: &Handlers{
					Tap: &GestureBinding{
						Type: "shell",
						Cmd:  "echo direct",
						Chain: []ChainStep{
							{Action: &Action{Type: "shell", Cmd: "echo chain"}},
						},
					},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected invalid mixed chain/direct binding")
	}
	if !strings.Contains(err.Error(), "cannot combine direct action fields, toggle, and chain") {
		t.Fatalf("expected mixed chain/direct binding error, got %v", err)
	}
}

func TestValidateRejectsChainStepWithBothDelayAndAction(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At: [2]int{0, 0},
				On: &Handlers{
					Tap: &GestureBinding{
						Chain: []ChainStep{
							{DelayMS: 250, Action: &Action{Type: "shell", Cmd: "echo bad"}},
						},
					},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected invalid chain step error")
	}
	if !strings.Contains(err.Error(), "cannot define both delay_ms and action") {
		t.Fatalf("expected invalid chain step error, got %v", err)
	}
}

func TestConfigUsesKeyActions(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At: [2]int{0, 0},
				On: &Handlers{
					Tap: &GestureBinding{
						Toggle: []ToggleStep{
							{Action: Action{Type: "shell", Cmd: "echo a"}},
							{Action: Action{Type: "key", Key: "cmd t"}},
						},
					},
				},
			},
			{
				At: [2]int{1, 0},
				Folder: &Folder{
					Buttons: []Button{
						{
							At: [2]int{0, 0},
							On: &Handlers{
								Hold: &GestureBinding{
									Chain: []ChainStep{
										{DelayMS: 100},
										{Action: &Action{Type: "http", URL: "http://example.test"}},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if !cfg.UsesKeyActions() {
		t.Fatal("expected config to report key actions")
	}
}

func TestValidateAcceptsRuntimeActions(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At: [2]int{0, 0},
				On: &Handlers{
					Tap:       &GestureBinding{Type: "page_next"},
					DoubleTap: &GestureBinding{Type: "page_prev"},
					Hold:      &GestureBinding{Type: "close_all_projects"},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected runtime actions to validate, got %v", err)
	}
}

func TestValidateRejectsMultipleToggleGesturesOnOneButton(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At: [2]int{0, 0},
				On: &Handlers{
					Tap: &GestureBinding{
						Toggle: []ToggleStep{
							{Action: Action{Type: "shell", Cmd: "echo 1"}},
							{Action: Action{Type: "shell", Cmd: "echo 2"}},
						},
					},
					DoubleTap: &GestureBinding{
						Toggle: []ToggleStep{
							{Action: Action{Type: "shell", Cmd: "echo 3"}},
							{Action: Action{Type: "shell", Cmd: "echo 4"}},
						},
					},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected multiple toggle gesture validation error")
	}
	if !strings.Contains(err.Error(), "can define toggle for only one gesture") {
		t.Fatalf("expected multiple toggle gesture error, got %v", err)
	}
}

func TestValidateRejectsEmptyOnHandlers(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{At: [2]int{0, 0}, On: &Handlers{}},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected empty triggers validation error")
	}
	if !strings.Contains(err.Error(), "on has no actions") {
		t.Fatalf("expected empty on error, got %v", err)
	}
}

func TestValidateAcceptsSpotifyWidgets(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{At: [2]int{0, 0}, Widget: &Widget{Type: "spotify_play_pause"}},
			{At: [2]int{1, 0}, Widget: &Widget{Type: "spotify_next"}},
			{At: [2]int{2, 0}, Widget: &Widget{Type: "spotify_artwork", PollMS: 2000}},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected spotify widgets to validate, got %v", err)
	}
}

func TestValidateAcceptsFreqtradeWidget(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At: [2]int{0, 0},
				Widget: &Widget{
					Type:   "freqtrade",
					DSNs:   "trader:secret@http://127.0.0.1:9910",
					Title:  "BTC",
					PollMS: 5000,
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected freqtrade widget to validate, got %v", err)
	}
}

func TestValidateRejectsFreqtradeWidgetWithoutDSNs(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At:     [2]int{0, 0},
				Widget: &Widget{Type: "freqtrade", PollMS: 5000},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected missing dsns validation error")
	}
	if !strings.Contains(err.Error(), "widget.dsns is empty") {
		t.Fatalf("expected missing dsns error, got %v", err)
	}
}

func TestValidateAcceptsProjectWidget(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At: [2]int{0, 0},
				Widget: &Widget{
					Type:       "project",
					ProjectDir: "/tmp/demo-project",
					On: &WidgetOn{
						Tap:       "start",
						DoubleTap: "focus",
						Hold:      "stop",
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected project widget to validate, got %v", err)
	}
}

func TestValidateRejectsUnknownProjectWidgetGestureAction(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At: [2]int{0, 0},
				Widget: &Widget{
					Type:       "project",
					ProjectDir: "/tmp/demo-project",
					On: &WidgetOn{
						Tap: "launch",
					},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected invalid project widget gesture action error")
	}
	if !strings.Contains(err.Error(), `widget.on.tap "launch" unknown`) {
		t.Fatalf("expected invalid project widget gesture action error, got %v", err)
	}
}

func TestValidateRejectsProjectWidgetWithoutDir(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{
				At:     [2]int{0, 0},
				Widget: &Widget{Type: "project"},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected missing project_dir validation error")
	}
	if !strings.Contains(err.Error(), "widget.project_dir is empty") {
		t.Fatalf("expected missing project_dir error, got %v", err)
	}
}

func TestIconPathsCollectsNestedUniqueIcons(t *testing.T) {
	cfg := &Config{
		Buttons: []Button{
			{At: [2]int{0, 0}, Icon: "/tmp/root.png"},
			{
				At: [2]int{2, 0},
				On: &Handlers{
					Tap: &GestureBinding{
						Toggle: []ToggleStep{
							{Icon: "/tmp/toggle-a.png", Action: Action{Type: "shell", Cmd: "echo 1"}},
							{Icon: "/tmp/toggle-b.png", Action: Action{Type: "shell", Cmd: "echo 2"}},
						},
					},
				},
			},
			{
				At: [2]int{1, 0},
				Folder: &Folder{
					Buttons: []Button{
						{At: [2]int{0, 0}, Icon: "/tmp/nested.png"},
						{At: [2]int{1, 0}, Icon: "/tmp/root.png"},
					},
				},
			},
		},
	}

	got := cfg.IconPaths()
	if len(got) != 4 {
		t.Fatalf("expected 4 unique icon paths, got %d: %v", len(got), got)
	}
	if got[0] != "/tmp/root.png" || got[1] != "/tmp/toggle-a.png" || got[2] != "/tmp/toggle-b.png" || got[3] != "/tmp/nested.png" {
		t.Fatalf("unexpected icon paths order/content: %v", got)
	}
}
