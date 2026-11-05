//go:build personal

package config

import (
	"strings"
	"testing"
)

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

func TestValidateRejectsPersonalWidgets(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"freqtrade poll":     {btnYAML("widget: {type: freqtrade, dsns: 'a:b@http://x', poll_ms: 1000}"), "poll_ms too small"},
		"site url":           {btnYAML("widget: {type: site}"), "url is empty"},
		"site scheme":        {btnYAML("widget: {type: site, url: 'ftp://x'}"), "must start with http"},
		"site poll":          {btnYAML("widget: {type: site, url: 'https://x', poll_ms: 5000}"), "poll_ms too small"},
		"project hold":       {btnYAML("widget: {type: project, project_dir: /p, hold_ms: 5}"), "hold_ms too small"},
		"project no gesture": {btnYAML("widget: {type: project, project_dir: /p, on: {}}"), "has no gestures"},
		"project bad repeat": {btnYAML("widget: {type: project, project_dir: /p, on: {repeat: jump}}"), "unknown"},
		"project bad double": {btnYAML("widget: {type: project, project_dir: /p, on: {double_tap: jump}}"), "unknown"},
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

func TestValidateAcceptsCloseAllProjects(t *testing.T) {
	if _, err := loadYAML(t, btnYAML("on: {tap: {type: close_all_projects}}")); err != nil {
		t.Fatal(err)
	}
}
