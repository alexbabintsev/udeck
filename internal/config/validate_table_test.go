package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadYAML(t *testing.T, yaml string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func btnYAML(body string) string {
	return "buttons:\n  - at: [0, 0]\n" + indent(body, "    ") + "\n"
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.Trim(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"brightness":         {"device:\n  brightness: 101\nbuttons: []\n", "brightness out of range"},
		"image quality":      {"device:\n  image_quality: 101\nbuttons: []\n", "image_quality out of range"},
		"image format":       {"device:\n  image_format: gif\nbuttons: []\n", "image_format"},
		"device margins":     {"device:\n  icon_margins: {left: 100, right: 100}\nbuttons: []\n", "left+right"},
		"negative margins":   {btnYAML("icon_margins: {top: -1}"), "negative"},
		"tall margins":       {btnYAML("icon_margins: {top: 100, bottom: 100}"), "top+bottom"},
		"slot out of range":  {"buttons:\n  - at: [5, 0]\n    label: x\n", "out of range"},
		"empty page":         {"pages:\n  - buttons: []\n", "buttons is empty"},
		"empty folder":       {btnYAML("folder:\n  buttons: []"), "folder.buttons is empty"},
		"on without actions": {btnYAML("on: {hold_ms: 500}"), "has no actions"},
		"two toggles": {btnYAML(`on:
  tap: {toggle: [{action: {type: shell, cmd: a}}, {action: {type: shell, cmd: b}}]}
  hold: {toggle: [{action: {type: shell, cmd: a}}, {action: {type: shell, cmd: b}}]}`), "toggle for only one gesture"},
		"hold_ms":           {btnYAML("on: {tap: {type: shell, cmd: x}, hold_ms: 5}"), "hold_ms too small"},
		"double_tap_ms":     {btnYAML("on: {tap: {type: shell, cmd: x}, double_tap_ms: 5}"), "double_tap_ms too small"},
		"repeat_every_ms":   {btnYAML("on: {tap: {type: shell, cmd: x}, repeat_every_ms: 5}"), "repeat_every_ms too small"},
		"empty key":         {btnYAML("on: {tap: {type: key}}"), ".key is empty"},
		"empty cmd":         {btnYAML("on: {tap: {type: shell}}"), ".cmd is empty"},
		"unknown action":    {btnYAML("on: {double_tap: {type: launch}}"), "unknown"},
		"repeat no action":  {btnYAML("on: {repeat: {cmd: x}}"), "has no action"},
		"short toggle":      {btnYAML("on: {tap: {toggle: [{action: {type: shell, cmd: a}}]}}"), "at least 2 steps"},
		"toggle bad action": {btnYAML("on: {tap: {toggle: [{action: {type: shell}}, {action: {type: shell, cmd: b}}]}}"), "cmd is empty"},
		"toggle+chain": {btnYAML(`on:
  tap: {toggle: [{action: {type: shell, cmd: a}}, {action: {type: shell, cmd: b}}], chain: [{delay_ms: 20}]}`), "cannot combine"},
		"chain+direct":       {btnYAML("on: {tap: {type: shell, cmd: x, chain: [{delay_ms: 20}]}}"), "cannot combine"},
		"chain empty step":   {btnYAML("on: {tap: {chain: [{}]}}"), "either delay_ms or action"},
		"chain tiny delay":   {btnYAML("on: {tap: {chain: [{delay_ms: 5}]}}"), "delay_ms too small"},
		"chain bad action":   {btnYAML("on: {tap: {chain: [{action: {type: http}}]}}"), "url is empty"},
		"widget poll":        {btnYAML("widget: {type: memory, poll_ms: 10}"), "poll_ms too small"},
		"freqtrade poll":     {btnYAML("widget: {type: freqtrade, dsns: 'a:b@http://x', poll_ms: 1000}"), "poll_ms too small"},
		"site url":           {btnYAML("widget: {type: site}"), "url is empty"},
		"site scheme":        {btnYAML("widget: {type: site, url: 'ftp://x'}"), "must start with http"},
		"site poll":          {btnYAML("widget: {type: site, url: 'https://x', poll_ms: 5000}"), "poll_ms too small"},
		"weather city":       {btnYAML("widget: {type: weather_day}"), "city is empty"},
		"weather day":        {btnYAML("widget: {type: weather_day, city: Kyiv, day_index: 14}"), "day_index"},
		"project hold":       {btnYAML("widget: {type: project, project_dir: /p, hold_ms: 5}"), "hold_ms too small"},
		"project no gesture": {btnYAML("widget: {type: project, project_dir: /p, on: {}}"), "has no gestures"},
		"project bad double": {btnYAML("widget: {type: project, project_dir: /p, on: {double_tap: jump}}"), "unknown"},
		"project bad repeat": {btnYAML("widget: {type: project, project_dir: /p, on: {repeat: jump}}"), "unknown"},
		"toggle icon color":  {btnYAML("on: {tap: {toggle: [{icon_color: xyz, action: {type: shell, cmd: a}}, {action: {type: shell, cmd: b}}]}}"), "icon_color"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadYAML(t, tc.yaml)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("expected read error")
	}
	if _, err := loadYAML(t, "buttons: [\n"); err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestLoadResolvesRelativePathsAndDefaults(t *testing.T) {
	cfg, err := loadYAML(t, `
device:
  icon_margins: {top: 4}
widgets:
  font: ~/fonts/x.ttf
buttons:
  - at: [0, 0]
    icon: rel/icon.png
    on:
      tap:
        toggle:
          - {icon: rel/a.png, action: {type: shell, cmd: a}}
          - {icon: lucide/play, action: {type: shell, cmd: b}}
  - at: [0, 1]
    widget: {type: project, project_dir: proj}
  - at: [0, 2]
    folder:
      buttons:
        - at: [0, 0]
          icon: ~/x.png
          label: in
`)
	if err != nil {
		t.Fatal(err)
	}
	b := cfg.Buttons
	if !filepath.IsAbs(b[0].Icon) || !strings.HasSuffix(b[0].Icon, "rel/icon.png") {
		t.Fatalf("icon not resolved: %q", b[0].Icon)
	}
	if !filepath.IsAbs(b[0].On.Tap.Toggle[0].Icon) || b[0].On.Tap.Toggle[1].Icon != "lucide/play" {
		t.Fatal("toggle icons not resolved correctly")
	}
	if !filepath.IsAbs(b[1].Widget.ProjectDir) {
		t.Fatal("project_dir not resolved")
	}
	if b[2].Folder.Buttons[0].Icon != "~/x.png" || b[2].Folder.Buttons[0].IconMargins.Top != 4 {
		t.Fatal("folder defaults")
	}
	if b[0].IconMargins == cfg.Device.IconMargins {
		t.Fatal("margins must be copied, not shared")
	}
	if cfg.Widgets.Font != "~/fonts/x.ttf" {
		t.Fatal("widgets.font")
	}
	if b[0].HIDIndex() != 0 || (Button{At: [2]int{2, 2}}).HIDIndex() != 12 || (Button{At: [2]int{3, 2}}).HIDIndex() != 13 {
		t.Fatal("HIDIndex")
	}
}

func TestUsesKeyActionsAcrossNesting(t *testing.T) {
	for name, yaml := range map[string]string{
		"double tap": btnYAML("on: {double_tap: {type: key, key: a}}"),
		"repeat":     btnYAML("on: {repeat: {type: key, key: a}}"),
		"chain":      btnYAML("on: {tap: {chain: [{action: {type: key, key: a}}]}}"),
		"folder":     "pages:\n  - buttons:\n      - at: [0, 0]\n        folder:\n          buttons:\n            - at: [0, 0]\n              on: {hold: {type: key, key: a}}\n",
	} {
		cfg, err := loadYAML(t, yaml)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !cfg.UsesKeyActions() {
			t.Fatalf("%s: expected key actions", name)
		}
	}
	cfg, _ := loadYAML(t, btnYAML("on: {tap: {type: shell, cmd: x}}"))
	if cfg.UsesKeyActions() {
		t.Fatal("no key actions expected")
	}
	if (&GestureBinding{}).Action() != nil {
		t.Fatal("empty binding has no action")
	}
}
