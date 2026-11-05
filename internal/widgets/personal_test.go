//go:build personal

package widgets

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
)

func TestProjectWidgetHelpersAndFallbacks(t *testing.T) {
	if projectSessionName("/x/My Proj.v2", "") != "My_Proj_v2" || projectSessionName("/x", " custom ") != "custom" {
		t.Fatalf("projectSessionName = %q", projectSessionName("/x/My Proj.v2", ""))
	}
	if projectEditorSubdir("www/", "") != "www" || projectEditorSubdir("www", "/app/") != "app" {
		t.Fatal("projectEditorSubdir")
	}
	if holdDuration(0) != defaultHoldMS*time.Millisecond || holdDuration(5) != 5*time.Millisecond {
		t.Fatal("holdDuration")
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

func TestManagerResolveButtonsInjectsFreqtradeWidgetIcon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/trades":
			if r.URL.Query().Get("limit") == "1" {
				_, _ = w.Write([]byte(`{"total_trades":1}`))
				return
			}
			_, _ = w.Write([]byte(`{"trades":[{"is_open":false,"profit_abs":1.25,"pair":"BTC/USDT","close_timestamp":100}]}`))
		case "/api/v1/status":
			_, _ = w.Write([]byte(`[{"profit_abs":0.75}]`))
		case "/api/v1/profit":
			_, _ = w.Write([]byte(`{"closed_trade_count":1,"profit_closed_coin":2.5}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	buttons := []config.Button{
		{
			At:        [2]int{0, 0},
			Label:     "Freqtrade",
			RuntimeID: "buttons[0]",
			Widget: &config.Widget{
				Type:   "freqtrade",
				DSNs:   "user:pass@" + srv.URL,
				Title:  "BTC",
				PollMS: 3000,
			},
		},
	}

	m, err := NewManager(&config.Config{Buttons: buttons})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer m.Close()

	got, err := m.ResolveButtons(buttons)
	if err != nil {
		t.Fatalf("resolve buttons: %v", err)
	}
	if got[0].Icon == "" {
		t.Fatal("expected resolved widget icon path")
	}
	if got[0].Label != "" {
		t.Fatalf("expected widget label to be suppressed, got %q", got[0].Label)
	}
}

// Regression: a freqtrade page opened once kept polling its (unreachable)
// API forever after the user left it.
func TestManagerPausesOffscreenWidgetsAndRefreshesOnReturn(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	ft := config.Button{At: [2]int{0, 0}, RuntimeID: "folder.ft", Widget: &config.Widget{
		Type: "freqtrade", DSNs: "u:p@" + srv.URL, PollMS: 3000,
	}}
	other := config.Button{At: [2]int{1, 0}, RuntimeID: "root.label", Label: "x"}
	m, err := NewManager(&config.Config{Buttons: []config.Button{
		{At: [2]int{0, 0}, RuntimeID: "root.folder", Folder: &config.Folder{Buttons: []config.Button{ft}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Open the folder: the widget is created and fetches once.
	if _, err := m.ResolveButtons([]config.Button{ft}); err != nil {
		t.Fatal(err)
	}
	w := m.items["folder.ft"].(*FreqtradeWidget)
	waitFor(t, "initial fetch", func() bool { return hits.Load() > 0 })

	// Leave the folder: the widget is paused and stops polling.
	if _, err := m.ResolveButtons([]config.Button{other}); err != nil {
		t.Fatal(err)
	}
	if w.isActive() {
		t.Fatal("off-screen widget must be paused")
	}
	time.Sleep(20 * time.Millisecond)
	paused := hits.Load()
	w.every(time.Millisecond, func(time.Time) { w.refresh() })
	time.Sleep(30 * time.Millisecond)
	if hits.Load() != paused {
		t.Fatal("paused widget kept polling")
	}

	// Come back: resumed and refreshed right away.
	if _, err := m.ResolveButtons([]config.Button{ft}); err != nil {
		t.Fatal(err)
	}
	if !w.isActive() {
		t.Fatal("visible widget must be active")
	}
	waitFor(t, "refresh on return", func() bool { return hits.Load() > paused })
}

func TestManagerPersonalGestureOptions(t *testing.T) {
	btn := func(id string, on *config.WidgetOn) config.Button {
		return config.Button{RuntimeID: id, Widget: &config.Widget{Type: "project", On: on, HoldMS: 900}}
	}
	m, err := NewManager(&config.Config{Buttons: []config.Button{
		btn("p-default", nil),
		btn("p-custom", &config.WidgetOn{Tap: "start", Repeat: "focus"}),
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
}

func TestManagerCreatesPersonalWidgets(t *testing.T) {
	dir := t.TempDir()
	writeTestScript(t, filepath.Join(dir, "start"), "true\n")
	oldHas := projectHasSession
	projectHasSession = func(string) (bool, error) { return false, nil }
	defer func() { projectHasSession = oldHas }()

	buttons := []config.Button{
		{RuntimeID: "f", Widget: &config.Widget{Type: "site", URL: "http://127.0.0.1:1"}},
		{RuntimeID: "h", Widget: &config.Widget{Type: "project", ProjectDir: dir}},
	}
	m, err := NewManager(&config.Config{Buttons: buttons})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResolveButtons(buttons); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}
