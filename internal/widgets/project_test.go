package widgets

import (
	"os"
	"path/filepath"
	"strings"
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
			Type:       "project",
			ProjectDir: dir,
			HoldMS:     600,
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

func TestProjectAttachCommandQuotesPaths(t *testing.T) {
	cmd := projectAttachCommand("/tmp/my project", "demo:session")
	want := "cd '/tmp/my project' && exec tmux attach-session -t 'demo:session'"
	if cmd != want {
		t.Fatalf("unexpected attach command:\nwant: %s\ngot:  %s", want, cmd)
	}
}

func TestTerminalAttachInvocationSupportsKnownTerminals(t *testing.T) {
	tests := []struct {
		app        string
		wantMarker string
	}{
		{app: "iTerm", wantMarker: "create tab with default profile"},
		{app: "Terminal", wantMarker: `do script "tmux attach-session -t demo"`},
		{app: "Ghostty", wantMarker: `set initial input of cfg to "tmux attach-session -t demo" & linefeed`},
	}

	for _, tt := range tests {
		prog, args, ok := terminalAttachInvocation(tt.app, "tmux attach-session -t demo")
		if !ok {
			t.Fatalf("%s should be supported", tt.app)
		}
		if prog != "/usr/bin/osascript" {
			t.Fatalf("%s should use osascript, got %s", tt.app, prog)
		}
		if len(args) != 2 || args[0] != "-e" {
			t.Fatalf("%s should pass one AppleScript block, got %#v", tt.app, args)
		}
		if !strings.Contains(args[1], tt.wantMarker) {
			t.Fatalf("%s script missing marker %q:\n%s", tt.app, tt.wantMarker, args[1])
		}
	}

	if _, _, ok := terminalAttachInvocation("Alacritty", "tmux attach-session -t demo"); ok {
		t.Fatal("unknown terminals should fall back to plain open")
	}
}

func TestManagerCloseAllProjectsRunsStopForConfiguredWidgets(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	writeTestScript(t, filepath.Join(dirA, "start"), "touch \"$PROJECT_DIR/.running\"\n")
	writeTestScript(t, filepath.Join(dirA, "stop"), "rm -f \"$PROJECT_DIR/.running\"\ntouch \"$PROJECT_DIR/.stopped\"\n")
	writeTestScript(t, filepath.Join(dirB, "start"), "touch \"$PROJECT_DIR/.running\"\n")
	writeTestScript(t, filepath.Join(dirB, "stop"), "rm -f \"$PROJECT_DIR/.running\"\ntouch \"$PROJECT_DIR/.stopped\"\n")

	oldHasSession := projectHasSession
	defer func() { projectHasSession = oldHasSession }()
	projectHasSession = func(session string) (bool, error) {
		switch session {
		case projectSessionName(dirA, ""):
			_, err := os.Stat(filepath.Join(dirA, ".running"))
			return err == nil, nil
		case projectSessionName(dirB, ""):
			_, err := os.Stat(filepath.Join(dirB, ".running"))
			return err == nil, nil
		default:
			return false, nil
		}
	}

	if err := os.WriteFile(filepath.Join(dirA, ".running"), []byte("on"), 0o644); err != nil {
		t.Fatalf("seed project A running file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirB, ".running"), []byte("on"), 0o644); err != nil {
		t.Fatalf("seed project B running file: %v", err)
	}

	cfg := &config.Config{
		Pages: []config.Page{{
			Buttons: []config.Button{
				{
					RuntimeID: "buttons[0]",
					Widget: &config.Widget{
						Type:       "project",
						ProjectDir: dirA,
					},
				},
				{
					RuntimeID: "buttons[1]",
					Widget: &config.Widget{
						Type:       "project",
						ProjectDir: dirB,
					},
				},
			},
		}},
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	if err := mgr.CloseAllProjects(); err != nil {
		t.Fatalf("close all projects: %v", err)
	}

	waitForFile(t, filepath.Join(dirA, ".stopped"))
	waitForFile(t, filepath.Join(dirB, ".stopped"))
	waitForMissingFile(t, filepath.Join(dirA, ".running"))
	waitForMissingFile(t, filepath.Join(dirB, ".running"))
}

func TestManagerCloseAllProjectsDoesNotHoldLock(t *testing.T) {
	dir := t.TempDir()
	writeTestScript(t, filepath.Join(dir, "start"), "true\n")
	// The stop script signals it started, then waits to be released.
	writeTestScript(t, filepath.Join(dir, "stop"), "touch \"$PROJECT_DIR/.stopping\"\nwhile [ ! -f \"$PROJECT_DIR/.release\" ]; do sleep 0.02; done\n")

	oldHasSession := projectHasSession
	defer func() { projectHasSession = oldHasSession }()
	projectHasSession = func(string) (bool, error) { return true, nil }

	cfg := &config.Config{Pages: []config.Page{{Buttons: []config.Button{{
		RuntimeID: "buttons[0]",
		Widget:    &config.Widget{Type: "project", ProjectDir: dir},
	}}}}}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	done := make(chan error, 1)
	go func() { done <- mgr.CloseAllProjects() }()
	waitForFile(t, filepath.Join(dir, ".stopping"))

	// The event loop must still be able to switch pages while a stop
	// script runs.
	ctxDone := make(chan struct{})
	go func() {
		mgr.SetContext(Context{PageIndex: 1})
		close(ctxDone)
	}()
	select {
	case <-ctxDone:
	case <-time.After(time.Second):
		t.Fatal("SetContext blocked while a stop script was running")
	}

	if err := os.WriteFile(filepath.Join(dir, ".release"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("close all projects: %v", err)
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
