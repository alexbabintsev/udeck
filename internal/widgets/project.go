package widgets

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
)

const (
	defaultProjectPollMS = 3000
	defaultProjectHoldMS = 700
	projectScriptTimeout = 20 * time.Second
)

type commandRunner func(ctx context.Context, name string, args ...string) *exec.Cmd

// Package defaults, copied into each widget at construction so tests can
// replace them without racing the widget's goroutines.
var (
	projectExecCommandContext commandRunner = exec.CommandContext
	projectHasSession         func(session string) (bool, error)
)

type projectAction string

const (
	projectActionNone  projectAction = ""
	projectActionStart projectAction = "start"
	projectActionStop  projectAction = "stop"
	projectActionFocus projectAction = "focus"
)

type ProjectWidget struct {
	*widgetBase
	projectDir string
	session    string
	title      string
	poll       time.Duration

	startScript  string
	stopScript   string
	focusScript  string
	seedIcon     image.Image
	terminal     string
	editor       string
	editorSubdir string
	bindings     map[ig.Kind]projectAction
	run          commandRunner
	hasSession   func(session string) (bool, error)

	mu         sync.Mutex
	running    bool
	busy       bool
	lastAction projectAction
	lastError  string
}

func NewProjectWidget(id string, btn config.Button, settings config.ProjectWidgetSettings, notify func(string)) (*ProjectWidget, error) {
	wcfg := btn.Widget
	if wcfg == nil {
		return nil, fmt.Errorf("project widget config missing")
	}
	projectDir := expandWidgetPath(wcfg.ProjectDir)
	startScript := resolveProjectScriptPath(projectDir, wcfg.Scripts.Start, "start")
	if _, err := os.Stat(startScript); err != nil {
		return nil, fmt.Errorf("project start script %q: %w", startScript, err)
	}

	base, err := newWidgetBase(id, "project-widget", notify)
	if err != nil {
		return nil, err
	}
	w := &ProjectWidget{
		widgetBase:   base,
		projectDir:   projectDir,
		session:      projectSessionName(projectDir, wcfg.Session),
		title:        projectTitle(wcfg.Title, btn.Label, projectDir),
		poll:         pollDurationOrDefault(wcfg.PollMS, defaultProjectPollMS),
		startScript:  startScript,
		stopScript:   resolveProjectScriptPath(projectDir, wcfg.Scripts.Stop, "stop"),
		focusScript:  resolveProjectScriptPath(projectDir, wcfg.Scripts.Focus, "focus"),
		terminal:     strings.TrimSpace(settings.Terminal),
		editor:       strings.TrimSpace(settings.Editor),
		editorSubdir: projectEditorSubdir(settings.EditorSubdir, wcfg.EditorSubdir),
		bindings:     projectGestureBindings(wcfg),
		run:          projectExecCommandContext,
		hasSession:   projectHasSession,
	}
	if w.hasSession == nil {
		run := w.run
		w.hasSession = func(session string) (bool, error) { return tmuxHasSession(run, session) }
	}
	if btn.Icon != "" {
		w.seedIcon = loadWidgetSeedIcon(btn.Icon)
	}
	if running, err := w.hasSession(w.session); err == nil {
		w.running = running
	} else {
		w.lastError = err.Error()
	}
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write project widget icon: %w", err)
	}
	w.every(w.poll, func(time.Time) {
		w.mu.Lock()
		busy := w.busy
		w.mu.Unlock()
		if !busy {
			_, _ = w.refreshRunning()
		}
	})
	return w, nil
}

func (w *ProjectWidget) HandleGesture(kind ig.Kind) {
	w.mu.Lock()
	if w.isClosed() || w.busy {
		w.mu.Unlock()
		return
	}
	running := w.running
	w.mu.Unlock()

	switch kind {
	case ig.Tap, ig.DoubleTap, ig.Hold, ig.Repeat:
		action := w.bindings[kind]
		if action == projectActionNone {
			return
		}
		if action == projectActionStop && !running {
			return
		}
		w.trigger(action)
	}
}

func (w *ProjectWidget) resume() { _, _ = w.refreshRunning() }

func (w *ProjectWidget) trigger(action projectAction) {
	w.mu.Lock()
	if w.busy || w.isClosed() {
		w.mu.Unlock()
		return
	}
	w.busy = true
	w.lastAction = action
	w.lastError = ""
	w.mu.Unlock()
	_, _ = w.render()

	go func() {
		err := w.runAction(action)
		w.mu.Lock()
		w.busy = false
		if err != nil {
			w.lastError = err.Error()
		} else {
			w.lastError = ""
		}
		w.mu.Unlock()
		_, _ = w.refreshRunning()
	}()
}

func (w *ProjectWidget) runAction(action projectAction) error {
	switch action {
	case projectActionStart:
		return w.runScript(w.startScript)
	case projectActionStop:
		if fileExists(w.stopScript) {
			return w.runScript(w.stopScript)
		}
		return tmuxKillSession(w.run, w.session)
	case projectActionFocus:
		if fileExists(w.focusScript) {
			return w.runScript(w.focusScript)
		}
		return w.focusFallback()
	default:
		return nil
	}
}

func (w *ProjectWidget) stopNow() error {
	if w.isClosed() {
		return nil
	}
	w.mu.Lock()
	w.busy = true
	w.lastAction = projectActionStop
	w.lastError = ""
	w.mu.Unlock()
	_, _ = w.render()

	err := w.runAction(projectActionStop)

	w.mu.Lock()
	w.busy = false
	if err != nil {
		w.lastError = err.Error()
	} else {
		w.lastError = ""
	}
	w.mu.Unlock()
	_, _ = w.refreshRunning()
	return err
}

func (w *ProjectWidget) runScript(scriptPath string) error {
	if scriptPath == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), projectScriptTimeout)
	defer cancel()

	cmd := w.run(ctx, "/bin/sh", scriptPath)
	cmd.Dir = w.projectDir
	cmd.Env = append(os.Environ(),
		"PROJECT_DIR="+w.projectDir,
		"PROJECT_SESSION="+w.session,
		"PROJECT_TERMINAL="+w.terminal,
		"PROJECT_EDITOR="+w.editor,
		"PROJECT_EDITOR_SUBDIR="+w.editorSubdir,
		"PROJECT_EDITOR_TARGET="+w.editorTarget(),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

func (w *ProjectWidget) refreshRunning() (bool, error) {
	running, err := w.hasSession(w.session)
	w.mu.Lock()
	if err == nil {
		w.running = running
		if !running && w.lastAction == projectActionStop {
			w.lastAction = projectActionNone
		}
		if running && w.lastAction == projectActionStart {
			w.lastAction = projectActionNone
		}
	}
	w.mu.Unlock()
	changed, renderErr := w.render()
	if renderErr != nil {
		return false, renderErr
	}
	return changed, err
}

func (w *ProjectWidget) render() (bool, error) {
	changed, err := w.publish(w.renderImage())
	if err != nil {
		return false, fmt.Errorf("write project widget icon: %w", err)
	}
	return changed, nil
}

func (w *ProjectWidget) renderImage() *image.RGBA {
	w.mu.Lock()
	running := w.running
	title := w.title
	seed := w.seedIcon
	w.mu.Unlock()

	img := image.NewRGBA(image.Rect(0, 0, icons.IconWidth, icons.IconHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0, 0, 0, 0xff}}, image.Point{}, draw.Src)

	const iconSize = 96
	iconX := (icons.IconWidth - iconSize) / 2
	iconY := 24
	if seed != nil {
		dst := image.Rect(iconX, iconY, iconX+iconSize, iconY+iconSize)
		xdraw.CatmullRom.Scale(img, dst, seed, seed.Bounds(), draw.Over, nil)
	}

	drawText(img, strings.ToUpper(strings.TrimSpace(title)),
		FromLeft(8), FromTop(iconY+iconSize+12),
		icons.IconWidth-16, 40,
		AlignTopCenter, color.RGBA{0xf0, 0xf0, 0xf0, 0xff}, 28, true)

	if running {
		fillCircle(img, 28, 28, 8, color.RGBA{0x0e, 0xab, 0x2d, 0xff})
	}
	return img
}

func projectTitle(title, label, projectDir string) string {
	if strings.TrimSpace(title) != "" {
		return strings.TrimSpace(title)
	}
	if strings.TrimSpace(label) != "" {
		return strings.TrimSpace(label)
	}
	return filepath.Base(projectDir)
}

func projectSessionName(projectDir, override string) string {
	if strings.TrimSpace(override) != "" {
		return sanitizeSessionName(override)
	}
	return sanitizeSessionName(filepath.Base(projectDir))
}

func projectEditorSubdir(globalValue, localValue string) string {
	if strings.TrimSpace(localValue) != "" {
		return strings.Trim(strings.TrimSpace(localValue), "/")
	}
	return strings.Trim(strings.TrimSpace(globalValue), "/")
}

func projectGestureBindings(wcfg *config.Widget) map[ig.Kind]projectAction {
	if wcfg != nil && wcfg.On != nil {
		out := map[ig.Kind]projectAction{}
		if wcfg.On.Tap != "" {
			out[ig.Tap] = projectAction(strings.TrimSpace(wcfg.On.Tap))
		}
		if wcfg.On.DoubleTap != "" {
			out[ig.DoubleTap] = projectAction(strings.TrimSpace(wcfg.On.DoubleTap))
		}
		if wcfg.On.Hold != "" {
			out[ig.Hold] = projectAction(strings.TrimSpace(wcfg.On.Hold))
		}
		if wcfg.On.Repeat != "" {
			out[ig.Repeat] = projectAction(strings.TrimSpace(wcfg.On.Repeat))
		}
		if len(out) > 0 {
			return out
		}
	}
	return map[ig.Kind]projectAction{
		ig.Tap:       projectActionStart,
		ig.DoubleTap: projectActionFocus,
		ig.Hold:      projectActionStop,
	}
}

func projectHoldDuration(ms int) time.Duration {
	if ms <= 0 {
		ms = defaultProjectHoldMS
	}
	return time.Duration(ms) * time.Millisecond
}

func resolveProjectScriptPath(projectDir, override, defaultName string) string {
	raw := strings.TrimSpace(override)
	switch {
	case raw == "":
		return filepath.Join(projectDir, defaultName)
	case filepath.IsAbs(raw):
		return raw
	default:
		return filepath.Join(projectDir, raw)
	}
}

func expandWidgetPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func loadWidgetSeedIcon(path string) image.Image {
	f, err := os.Open(expandWidgetPath(path))
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

func sanitizeSessionName(name string) string {
	return strings.NewReplacer(".", "_", ":", "_", " ", "_").Replace(strings.TrimSpace(name))
}

func (w *ProjectWidget) focusFallback() error {
	app := strings.TrimSpace(w.terminal)
	if app == "" {
		app = "Terminal"
	}
	ctx, cancel := context.WithTimeout(context.Background(), projectScriptTimeout)
	defer cancel()

	if prog, args, ok := terminalAttachInvocation(app, projectAttachCommand(w.projectDir, w.session)); ok {
		cmd := w.run(ctx, prog, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				return err
			}
			return fmt.Errorf("%w: %s", err, msg)
		}
		return nil
	}

	cmd := w.run(ctx, "/usr/bin/open", "-a", app, w.projectDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (w *ProjectWidget) editorTarget() string {
	if w.editorSubdir == "" {
		return w.projectDir
	}
	return filepath.Join(w.projectDir, w.editorSubdir)
}

func projectAttachCommand(projectDir, session string) string {
	return fmt.Sprintf("cd %s && exec tmux attach-session -t %s", shellSingleQuote(projectDir), shellSingleQuote(session))
}

func terminalAttachInvocation(app, shellCmd string) (string, []string, bool) {
	switch lower := strings.ToLower(strings.TrimSpace(app)); lower {
	case "iterm", "iterm2":
		return "/usr/bin/osascript", []string{
			"-e", fmt.Sprintf(`tell application %q
	activate
	if (count of windows) = 0 then
		create window with default profile
	end if
	tell current window
		create tab with default profile
		tell current session of current tab to write text %q
	end tell
end tell`, app, shellCmd),
		}, true
	case "terminal", "apple terminal":
		return "/usr/bin/osascript", []string{
			"-e", fmt.Sprintf(`tell application "Terminal"
	activate
	if (count of windows) = 0 then
		do script %q
	else
		do script %q in front window
	end if
end tell`, shellCmd, shellCmd),
		}, true
	default:
		return "", nil, false
	}
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func tmuxHasSession(run commandRunner, session string) (bool, error) {
	if strings.TrimSpace(session) == "" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := run(ctx, "/usr/bin/env", "tmux", "has-session", "-t", session)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func tmuxKillSession(run commandRunner, session string) error {
	if strings.TrimSpace(session) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	cmd := run(ctx, "/usr/bin/env", "tmux", "kill-session", "-t", session)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}
