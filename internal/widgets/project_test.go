package widgets

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
)

func TestProjectWidgetLifecycleScripts(t *testing.T) {
	dir := t.TempDir()
	runningFile := filepath.Join(dir, ".running")
	focusFile := filepath.Join(dir, ".focus")
	envFile := filepath.Join(dir, ".env")

	writeTestScript(t, filepath.Join(dir, "start"), "touch \"$PROJECT_DIR/.running\"\nprintf '%s|%s|%s\\n' \"$PROJECT_TERMINAL\" \"$PROJECT_EDITOR\" \"$PROJECT_EDITOR_TARGET\" > \"$PROJECT_DIR/.env\"\n")
	writeTestScript(t, filepath.Join(dir, "stop"), "rm -f \"$PROJECT_DIR/.running\"\n")
	writeTestScript(t, filepath.Join(dir, "focus"), "touch \"$PROJECT_DIR/.focus\"\n")

	oldHasSession := projectHasSession
	defer func() { projectHasSession = oldHasSession }()
	projectHasSession = func(session string) (bool, error) {
		_, err := os.Stat(runningFile)
		return err == nil, nil
	}

	btn := config.Button{
		Label: "Demo",
		Widget: &config.Widget{
			Type:        "project",
			ProjectDir:  dir,
			HoldMS:      600,
			On: &config.WidgetOn{
				Tap:       "start",
				DoubleTap: "focus",
				Hold:      "stop",
			},
		},
	}
	w, err := NewProjectWidget("buttons[0]", btn, config.ProjectWidgetSettings{
		Terminal:     "Ghostty",
		Editor:       "PhpStorm",
		EditorSubdir: "www",
	}, func(string) {})
	if err != nil {
		t.Fatalf("new project widget: %v", err)
	}
	defer w.Close()

	w.HandleGesture(ig.Tap)
	waitForFile(t, runningFile)
	if got, err := os.ReadFile(envFile); err != nil {
		t.Fatalf("read env file: %v", err)
	} else if string(got) != "Ghostty|PhpStorm|"+filepath.Join(dir, "www")+"\n" {
		t.Fatalf("unexpected script env: %q", string(got))
	}

	w.HandleGesture(ig.DoubleTap)
	waitForFile(t, focusFile)

	w.HandleGesture(ig.Hold)
	waitForMissingFile(t, runningFile)
}

func TestProjectWidgetRequiresStartScript(t *testing.T) {
	dir := t.TempDir()
	btn := config.Button{
		Label: "Demo",
		Widget: &config.Widget{
			Type:       "project",
			ProjectDir: dir,
		},
	}
	if _, err := NewProjectWidget("buttons[0]", btn, config.ProjectWidgetSettings{}, func(string) {}); err == nil {
		t.Fatal("expected missing start script error")
	}
}

func writeTestScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o644); err != nil {
		t.Fatalf("write script %s: %v", path, err)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	waitForCondition(t, func() bool {
		_, err := os.Stat(path)
		return err == nil
	})
}

func waitForMissingFile(t *testing.T, path string) {
	t.Helper()
	waitForCondition(t, func() bool {
		_, err := os.Stat(path)
		return os.IsNotExist(err)
	})
}

func waitForCondition(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not reached before timeout")
}
