package widgets

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
)

func stubSpotifyFactory(t *testing.T) {
	t.Helper()
	old := newSpotifyService
	newSpotifyService = func(int) (*SpotifyService, error) { return &SpotifyService{cancel: func() {}}, nil }
	t.Cleanup(func() { newSpotifyService = old })
}

func TestManagerGestureOptionsPerWidgetType(t *testing.T) {
	stubSpotifyFactory(t)
	btn := func(id, typ string, on *config.WidgetOn) config.Button {
		return config.Button{RuntimeID: id, Widget: &config.Widget{Type: typ, On: on, HoldMS: 900}}
	}
	m, err := NewManager(&config.Config{Buttons: []config.Button{
		btn("p-default", "project", nil),
		btn("p-custom", "project", &config.WidgetOn{Tap: "start", Repeat: "focus"}),
		btn("spp", "spotify_play_pause", nil),
		btn("mem", "memory", nil),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if o, ok := m.gestureOptions("p-default"); !ok || !o.HasTap || !o.HasDoubleTap || !o.HasHold {
		t.Fatalf("default project options %+v", o)
	}
	o, _ := m.gestureOptions("p-custom")
	if !o.HasTap || o.HasDoubleTap || o.HasHold || !o.HasRepeat || o.RepeatEvery != o.HoldDelay {
		t.Fatalf("custom project options %+v", o)
	}
	if o, _ := m.gestureOptions("spp"); !o.HasHold || o.HoldDelay != 900*time.Millisecond {
		t.Fatalf("spotify options %+v", o)
	}
	if o, ok := m.gestureOptions("mem"); !ok || !o.HasTap || o.HasHold {
		t.Fatalf("memory options %+v", o)
	}
	if _, ok := m.gestureOptions("missing"); ok {
		t.Fatal("unknown id must have no options")
	}
}

// tapWidget records gestures.
type tapWidget struct {
	mu    sync.Mutex
	kinds []ig.Kind
}

func (w *tapWidget) IconPath() string { return "" }
func (w *tapWidget) Close() error     { return nil }
func (w *tapWidget) HandleGesture(k ig.Kind) {
	w.mu.Lock()
	w.kinds = append(w.kinds, k)
	w.mu.Unlock()
}

func TestManagerHandleEventAndTickDispatchGestures(t *testing.T) {
	stubSpotifyFactory(t)
	buttons := []config.Button{{RuntimeID: "spp", Widget: &config.Widget{Type: "spotify_play_pause", HoldMS: 50}}}
	m, err := NewManager(&config.Config{Buttons: buttons})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	w := &tapWidget{}
	m.items["spp"] = w

	now := time.Now()
	if !m.HandleEvent("spp", true, now) || !m.HandleEvent("spp", false, now.Add(10*time.Millisecond)) {
		t.Fatal("widget events must be consumed")
	}
	// Hold fires from Tick once the delay elapsed.
	m.HandleEvent("spp", true, now.Add(time.Second))
	if !m.Tick(buttons, now.Add(2*time.Second)) {
		t.Fatal("tick must fire the hold gesture")
	}
	if m.Tick(buttons, now.Add(3*time.Second)) {
		t.Fatal("hold must fire once")
	}
	m.HandleEvent("spp", false, now.Add(4*time.Second))
	w.mu.Lock()
	got := append([]ig.Kind(nil), w.kinds...)
	w.mu.Unlock()
	if len(got) != 2 || got[0] != ig.Tap || got[1] != ig.Hold {
		t.Fatalf("gestures = %v", got)
	}
	if m.HandleEvent("unknown", true, now) {
		t.Fatal("unknown id must not be consumed")
	}
	if m.Tick([]config.Button{{RuntimeID: "x"}}, now) {
		t.Fatal("no widgets: nothing to tick")
	}
}

func TestManagerResolveButtonsOverlaysPageNumber(t *testing.T) {
	buttons := []config.Button{
		{At: [2]int{0, 0}, Label: "Next", On: &config.Handlers{Tap: &config.GestureBinding{Type: "page_next"}}},
		{At: [2]int{1, 0}, Label: "Prev", IconColor: "ff0000", On: &config.Handlers{Hold: &config.GestureBinding{Type: "page_prev"}}},
		{At: [2]int{2, 0}, Label: "Plain"},
	}
	m, err := NewManager(&config.Config{Buttons: buttons})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.SetContext(Context{PageIndex: 2, TotalPages: 3})
	got, err := m.ResolveButtons(buttons)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Icon == "" || got[0].Label != "" || got[1].Icon == "" || got[1].Icon == got[0].Icon {
		t.Fatalf("expected distinct overlay icons: %+v", got[:2])
	}
	if got[2].Icon != "" || got[2].Label != "Plain" {
		t.Fatal("plain button must be untouched")
	}
	again, _ := m.ResolveButtons(buttons)
	if again[0].Icon != got[0].Icon {
		t.Fatal("overlay must be cached")
	}
	if pageNavIconID(buttons[0]) != "lucide/arrow-right" || pageNavIconID(buttons[1]) != "lucide/arrow-left" || pageNavIconID(buttons[2]) != "lucide/arrow-right" {
		t.Fatal("pageNavIconID")
	}
	if isPageNavButton(config.Button{Widget: &config.Widget{}}) || isPageNavButton(buttons[2]) {
		t.Fatal("isPageNavButton")
	}
	if sanitizeKey("a/b|1") != "a_b_1" {
		t.Fatal("sanitizeKey")
	}
}

func TestManagerRejectsUnknownWidget(t *testing.T) {
	m, err := NewManager(&config.Config{Buttons: []config.Button{{RuntimeID: "x", Widget: &config.Widget{Type: "nope"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := m.ResolveButtons([]config.Button{{RuntimeID: "x", Widget: &config.Widget{Type: "nope"}}}); err == nil {
		t.Fatal("expected unsupported widget error")
	}
	if _, err := m.ResolveButtons([]config.Button{{RuntimeID: "missing", Widget: &config.Widget{Type: "memory"}}}); err == nil {
		t.Fatal("expected unknown widget id error")
	}
}

func TestManagerCreatesEveryWidgetType(t *testing.T) {
	stubSpotifyFactory(t)
	newOpenMeteo(t)
	dir := t.TempDir()
	writeTestScript(t, filepath.Join(dir, "start"), "true\n")
	oldHas := projectHasSession
	projectHasSession = func(string) (bool, error) { return false, nil }
	defer func() { projectHasSession = oldHas }()

	buttons := []config.Button{
		{RuntimeID: "a", Widget: &config.Widget{Type: "spotify_play_pause"}},
		{RuntimeID: "b", Widget: &config.Widget{Type: "spotify_next"}},
		{RuntimeID: "c", Widget: &config.Widget{Type: "spotify_artwork"}},
		{RuntimeID: "d", At: [2]int{3, 2}, Widget: &config.Widget{Type: "wide_dashboard"}},
		{RuntimeID: "e", Widget: &config.Widget{Type: "weather_day", City: uniqueCity(t)}},
		{RuntimeID: "f", Widget: &config.Widget{Type: "site_views", URL: "http://127.0.0.1:1"}},
		{RuntimeID: "g", Widget: &config.Widget{Type: "site_online", URL: "http://127.0.0.1:1"}},
		{RuntimeID: "h", Widget: &config.Widget{Type: "project", ProjectDir: dir}},
	}
	m, err := NewManager(&config.Config{Buttons: buttons})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := m.ResolveButtons(buttons)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range resolved {
		if _, err := os.Stat(b.Icon); err != nil {
			t.Fatalf("%s: icon not rendered: %v", b.RuntimeID, err)
		}
	}
	m.SetContext(Context{FolderDepth: 1})
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectWidgetHelpersAndFallbacks(t *testing.T) {
	if projectSessionName("/x/My Proj.v2", "") != "My_Proj_v2" || projectSessionName("/x", " custom ") != "custom" {
		t.Fatalf("projectSessionName = %q", projectSessionName("/x/My Proj.v2", ""))
	}
	if projectEditorSubdir("www/", "") != "www" || projectEditorSubdir("www", "/app/") != "app" {
		t.Fatal("projectEditorSubdir")
	}
	if projectHoldDuration(0) != defaultProjectHoldMS*time.Millisecond || projectHoldDuration(5) != 5*time.Millisecond {
		t.Fatal("projectHoldDuration")
	}
	if resolveProjectScriptPath("/p", "", "start") != "/p/start" || resolveProjectScriptPath("/p", "/abs/s", "start") != "/abs/s" || resolveProjectScriptPath("/p", "bin/s", "start") != "/p/bin/s" {
		t.Fatal("resolveProjectScriptPath")
	}
	if home, _ := os.UserHomeDir(); expandWidgetPath("~/x") != filepath.Join(home, "x") || expandWidgetPath("/y") != "/y" {
		t.Fatal("expandWidgetPath")
	}
	if loadWidgetSeedIcon("/nonexistent.png") != nil {
		t.Fatal("missing seed icon must be nil")
	}
	bad := filepath.Join(t.TempDir(), "bad.png")
	os.WriteFile(bad, []byte("x"), 0o600)
	if loadWidgetSeedIcon(bad) != nil {
		t.Fatal("broken seed icon must be nil")
	}
	if projectTitle("", "Label", "/p/dir") != "Label" || projectTitle(" T ", "L", "/p") != "T" || projectTitle("", "", "/p/dir") != "dir" {
		t.Fatal("projectTitle")
	}
}

func TestProjectWidgetFocusFallbackAndTmux(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	run := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		mu.Lock()
		calls = append(calls, name+" "+strings.Join(args, " "))
		mu.Unlock()
		if strings.Contains(strings.Join(args, " "), "has-session") {
			return exec.CommandContext(ctx, "false")
		}
		return exec.CommandContext(ctx, "true")
	}
	w := &ProjectWidget{widgetBase: testBase(t, nil), projectDir: "/p", session: "s", terminal: "Ghostty", run: run}
	if err := w.focusFallback(); err != nil {
		t.Fatal(err)
	}
	w.terminal = "iTerm"
	if err := w.focusFallback(); err != nil {
		t.Fatal(err)
	}
	if ok, err := tmuxHasSession(run, "s"); ok || err != nil {
		t.Fatalf("has-session false must mean not running: %v %v", ok, err)
	}
	if ok, _ := tmuxHasSession(run, " "); ok {
		t.Fatal("blank session")
	}
	if err := tmuxKillSession(run, "s"); err != nil || tmuxKillSession(run, "") != nil {
		t.Fatal("kill-session")
	}
	failing := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "echo boom >&2; exit 1")
	}
	if err := tmuxKillSession(failing, "s"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
	w.run = failing
	if err := w.focusFallback(); err == nil {
		t.Fatal("expected focus fallback error")
	}
	if err := w.runAction(projectActionNone); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	n := len(calls)
	mu.Unlock()
	if n < 4 {
		t.Fatalf("calls = %v", calls)
	}
}
