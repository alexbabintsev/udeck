//go:build !personal

package config

import (
	"strings"
	"testing"
)

func TestPersonalTypesAreUnknown(t *testing.T) {
	for name, body := range map[string]string{
		"freqtrade": "widget: {type: freqtrade, dsns: 'a:b@http://x'}",
		"site":      "widget: {type: site, url: 'https://x'}",
		"project":   "widget: {type: project, project_dir: /p}",
		"action":    "on: {tap: {type: close_all_projects}}",
	} {
		_, err := loadYAML(t, btnYAML(body))
		if err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("%s: want unknown, got %v", name, err)
		}
	}
}
