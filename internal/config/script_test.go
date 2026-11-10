package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeScriptConfig(t *testing.T, yaml string, scripts ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "widgets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range scripts {
		if err := os.WriteFile(filepath.Join(dir, "widgets", name+".js"), []byte("widget({create() { return {draw() {}} }})"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScriptWidgetNeedsItsScript(t *testing.T) {
	path := writeScriptConfig(t, btnYAML("widget: {type: site, url: https://x}"))
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `type "site" unknown`) || !strings.Contains(err.Error(), "site.js") {
		t.Fatalf("want missing script error, got %v", err)
	}
}

func TestScriptWidgetKeepsItsOptions(t *testing.T) {
	path := writeScriptConfig(t, `widgets:
  font: ~/f.ttf
  project: {terminal: Ghostty}
buttons:
  - at: [0, 0]
    widget:
      type: site
      url: https://x
      on: {tap: start}
      poll_ms: 5000
  - at: [0, 1]
    folder:
      buttons:
        - at: [0, 0]
          widget: {type: project}
        - at: [0, 1]
          widget: {type: site}
`, "site", "project")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	w := cfg.Buttons[0].Widget
	if w.PollMS != 5000 || w.Params["url"] != "https://x" || w.Params["poll_ms"] != 5000 {
		t.Fatalf("params: %+v", w)
	}
	if on, _ := w.Params["on"].(map[string]any); on["tap"] != "start" {
		t.Fatalf("nested params: %v", w.Params["on"])
	}
	if cfg.Widgets.Font != "~/f.ttf" {
		t.Fatalf("font: %q", cfg.Widgets.Font)
	}
	if project, _ := cfg.Widgets.Scripts["project"].(map[string]any); project["terminal"] != "Ghostty" {
		t.Fatalf("script settings: %v", cfg.Widgets.Scripts)
	}
	dir := filepath.Join(filepath.Dir(path), "widgets")
	want := []string{filepath.Join(dir, "site.js"), filepath.Join(dir, "project.js")}
	if got := cfg.ScriptPaths(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ScriptPaths = %v, want %v", got, want)
	}
}

func TestScriptWidgetCheckHook(t *testing.T) {
	path := writeScriptConfig(t, btnYAML("widget: {type: site}"), "site")
	prev := CheckScriptWidget
	t.Cleanup(func() { CheckScriptWidget = prev })
	var gotScript string
	CheckScriptWidget = func(script string, w *Widget) error {
		gotScript = script
		return errors.New("url is empty")
	}
	_, err := Load(path)
	if err == nil || err.Error() != "pages[0].buttons[0].widget: url is empty" {
		t.Fatalf("got %v", err)
	}
	if filepath.Base(gotScript) != "site.js" {
		t.Fatalf("script %q", gotScript)
	}
}

func TestScriptWidgetTypeStaysInWidgetsDir(t *testing.T) {
	for _, typ := range []string{"../x", "a/b", "X", "a.b"} {
		path := writeScriptConfig(t, btnYAML("widget: {type: \""+typ+"\"}"))
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("%s: got %v", typ, err)
		}
	}
}
