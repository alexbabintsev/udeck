package widgets

import (
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
	ig "github.com/alexbabintsev/udeck/internal/gestures"
	"github.com/alexbabintsev/udeck/internal/icons"
)

// writeWidgetScript writes widgets/<name>.js under a temp config dir and
// returns the widgets dir.
func writeWidgetScript(t *testing.T, name, src string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "widgets")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

type notifyCounter struct {
	mu sync.Mutex
	n  int
}

func (c *notifyCounter) notify(string) { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *notifyCounter) count() int    { c.mu.Lock(); defer c.mu.Unlock(); return c.n }

func newTestScriptWidget(t *testing.T, src string, btn config.Button, settings any) (*scriptWidget, *notifyCounter) {
	t.Helper()
	dir := writeWidgetScript(t, "test_widget", src)
	typ, err := loadScriptType(filepath.Join(dir, "test_widget.js"))
	if err != nil {
		t.Fatal(err)
	}
	if btn.Widget == nil {
		btn.Widget = &config.Widget{Type: "test_widget"}
	}
	counter := &notifyCounter{}
	w, err := newScriptWidget("k", typ, btn, settings, counter.notify)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, counter
}

func pixelAt(t *testing.T, w *scriptWidget, x, y int) color.RGBA {
	t.Helper()
	img, ok := icons.Image(w.IconPath())
	if !ok {
		t.Fatal("no icon")
	}
	r, g, b, a := img.At(x, y).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

func TestScriptWidgetDrawsParamsAndSettings(t *testing.T) {
	w, _ := newTestScriptWidget(t, `widget({
		create(w) {
			return {
				draw(c) {
					if (w.params.color !== "#ff0000" || w.settings.size !== 3 || w.button.label !== "L" || w.params.on.tap !== "go") {
						throw new Error("bad handle " + JSON.stringify([w.params, w.settings, w.button]));
					}
					c.rect(0, 0, c.width, 20, w.params.color);
					c.circle(100, 100, 10, "00ff00");
					c.rect(150, 150, 40, 40, "#0000ff", 8);
					c.text("hello", 0, 40, c.width, 40, {size: 30, align: "left"});
					c.icon("lucide/play", 50, 140, 40, 40, "#ffffff");
				},
			};
		},
	});`, config.Button{Label: "L", Widget: &config.Widget{Type: "test_widget", Params: map[string]any{
		"color": "#ff0000", "on": map[string]any{"tap": "go"},
	}}}, map[string]any{"size": 3})

	if w.failed {
		t.Fatal("create failed")
	}
	if got := pixelAt(t, w, 5, 5); got != (color.RGBA{0xff, 0, 0, 0xff}) {
		t.Fatalf("rect pixel %v", got)
	}
	if got := pixelAt(t, w, 100, 100); got != (color.RGBA{0, 0xff, 0, 0xff}) {
		t.Fatalf("circle pixel %v", got)
	}
	if got := pixelAt(t, w, 170, 170); got != (color.RGBA{0, 0, 0xff, 0xff}) {
		t.Fatalf("rounded rect pixel %v", got)
	}
	if got := pixelAt(t, w, 150, 150); got != (color.RGBA{0, 0, 0, 0xff}) {
		t.Fatalf("rounded corner must stay black, got %v", got)
	}
	if got := pixelAt(t, w, 100, 30); got != (color.RGBA{0, 0, 0, 0xff}) {
		t.Fatalf("background %v", got)
	}
}

func TestScriptWidgetPollsAsynchronously(t *testing.T) {
	var mu sync.Mutex
	value := "#00ff00"
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Basic "+"dTpw" {
			http.Error(rw, "no", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(rw, `{"color": %q, "items": [1, 2, 3]}`, value)
	}))
	defer srv.Close()

	w, counter := newTestScriptWidget(t, `widget({
		pollMS: 1000,
		minPollMS: 10,
		create(w) {
			let color = "#ff0000";
			return {
				async poll() {
					const data = await udeck.http.getJSON(w.params.url, {headers: {Authorization: "Basic " + udeck.base64("u:p")}});
					if (!Array.isArray(data.items) || data.items.map(x => x * 2)[2] !== 6) throw new Error("not a JS array");
					color = data.color;
				},
				draw(c) { c.clear(color); },
			};
		},
	});`, config.Button{Widget: &config.Widget{Type: "test_widget", PollMS: 20, Params: map[string]any{"url": srv.URL}}}, nil)

	waitFor(t, "polled color", func() bool { return pixelAt(t, w, 1, 1) == color.RGBA{0, 0xff, 0, 0xff} })
	mu.Lock()
	value = "#0000ff"
	mu.Unlock()
	waitFor(t, "next poll", func() bool { return pixelAt(t, w, 1, 1) == color.RGBA{0, 0, 0xff, 0xff} })
	if counter.count() < 2 {
		t.Fatalf("notified %d times", counter.count())
	}
}

func TestScriptWidgetGestureDuringSlowPoll(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		<-release
		fmt.Fprint(rw, `{}`)
	}))
	defer srv.Close()
	defer close(release)

	w, _ := newTestScriptWidget(t, `widget({
		create(w) {
			let on = false;
			return {
				async poll() { await udeck.http.getJSON(w.params.url, {timeout: 5000}); },
				gesture(kind) { if (kind === "tap") on = !on; },
				draw(c) { c.clear(on ? "#ffffff" : "#000000"); },
			};
		},
	});`, config.Button{Widget: &config.Widget{Type: "test_widget", Params: map[string]any{"url": srv.URL}}}, nil)

	// The poll is still waiting for the server: the tap must not wait.
	w.HandleGesture(ig.Tap)
	waitFor(t, "tap redraw", func() bool { return pixelAt(t, w, 1, 1) == color.RGBA{0xff, 0xff, 0xff, 0xff} })
}

func TestScriptWidgetExec(t *testing.T) {
	w, _ := newTestScriptWidget(t, `widget({
		create(w) {
			let text = "";
			return {
				async gesture() {
					const r = await udeck.exec("/bin/sh", ["-c", "echo $GREETING; exit 3"], {env: {GREETING: "hi"}});
					text = r.code + ":" + r.stdout.trim();
					const fail = await udeck.exec("/bin/sh", ["-c", "sleep 5"], {timeout: 50}).catch(e => "timeout " + e.message);
					text += "|" + fail;
				},
				draw(c) {
					if (!text) return;
					c.clear(text === "3:hi|timeout sh timed out after 50ms" ? "#ffffff" : "#ff0000");
					if (text[0] !== "3") udeck.log(text);
				},
			};
		},
	});`, config.Button{}, nil)

	w.HandleGesture(ig.Tap)
	waitFor(t, "exec result", func() bool { return pixelAt(t, w, 1, 1) != color.RGBA{0, 0, 0, 0xff} })
	if got := pixelAt(t, w, 1, 1); got != (color.RGBA{0xff, 0xff, 0xff, 0xff}) {
		t.Fatalf("exec results wrong (pixel %v)", got)
	}
}

func TestScriptWidgetErrorsShowAnErrorCard(t *testing.T) {
	w, _ := newTestScriptWidget(t, `widget({ create(w) { throw new Error("boom"); } });`, config.Button{}, nil)
	if !w.failed {
		t.Fatal("create error must mark the widget failed")
	}
	if w.lastErr["create"] == "" || !strings.Contains(w.lastErr["create"], "boom") {
		t.Fatalf("lastErr %v", w.lastErr)
	}
	w.HandleGesture(ig.Tap) // must not panic
	if _, ok := icons.Image(w.IconPath()); !ok {
		t.Fatal("error card missing")
	}
}

func TestScriptWidgetEndlessLoopIsInterrupted(t *testing.T) {
	prev := scriptCallTimeout
	scriptCallTimeout = 50 * time.Millisecond
	t.Cleanup(func() { scriptCallTimeout = prev })
	w, _ := newTestScriptWidget(t, `widget({ create(w) { return { draw(c) { for (;;) {} } }; } });`, config.Button{}, nil)
	if !strings.Contains(w.lastErr["draw"], "too long") {
		t.Fatalf("lastErr %v", w.lastErr)
	}
}

func TestScriptWidgetTimersAndClose(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "closed")
	w, _ := newTestScriptWidget(t, `widget({
		create(w) {
			let ticks = 0;
			const tick = () => { ticks++; w.redraw(); if (ticks < 3) setTimeout(tick, 5); };
			setTimeout(tick, 5);
			const never = setTimeout(() => { throw new Error("cleared timer ran"); }, 10);
			clearTimeout(never);
			return {
				draw(c) { c.clear(ticks >= 3 ? "#ffffff" : "#000000"); },
				close() { udeck.exec("/usr/bin/touch", [w.params.marker]); },
			};
		},
	});`, config.Button{Widget: &config.Widget{Type: "test_widget", Params: map[string]any{"marker": marker}}}, nil)
	waitFor(t, "timer ticks", func() bool { return pixelAt(t, w, 1, 1) == color.RGBA{0xff, 0xff, 0xff, 0xff} })
	_ = w.Close()
	waitFor(t, "close handler", func() bool { _, err := os.Stat(marker); return err == nil })
}

func TestScriptTypeValidateAndGestures(t *testing.T) {
	dir := writeWidgetScript(t, "checked", `widget({
		minPollMS: 500,
		validate(p) { if (!p.url) throw new Error("url is empty"); },
		gestures(p) { return p.on ? {tap: true, hold: true, hold_ms: 900} : ["tap", "double_tap"]; },
		create() { return { draw() {} }; },
	});`)
	path := filepath.Join(dir, "checked.js")
	if err := checkScriptWidget(path, &config.Widget{Params: map[string]any{}}); err == nil || err.Error() != "url is empty" {
		t.Fatalf("validate: %v", err)
	}
	if err := checkScriptWidget(path, &config.Widget{PollMS: 100, Params: map[string]any{"url": "x"}}); err == nil || !strings.Contains(err.Error(), "at least 500") {
		t.Fatalf("min poll: %v", err)
	}
	if err := checkScriptWidget(path, &config.Widget{Params: map[string]any{"url": "x"}}); err != nil {
		t.Fatal(err)
	}
	typ, _ := loadScriptType(path)
	opts, err := typ.gestures(&config.Widget{Params: map[string]any{"on": true}})
	if err != nil || !opts.HasTap || !opts.HasHold || opts.HasDoubleTap || opts.HoldDelay != 900*time.Millisecond {
		t.Fatalf("gestures with on: %+v %v", opts, err)
	}
	opts, err = typ.gestures(&config.Widget{HoldMS: 1200})
	if err != nil || !opts.HasTap || !opts.HasDoubleTap || opts.HasHold || opts.HoldDelay != 1200*time.Millisecond || opts.DoubleTapDelay != defaultScriptDoubleTap {
		t.Fatalf("gestures list: %+v %v", opts, err)
	}
}

func TestScriptTypeLoadErrors(t *testing.T) {
	for name, src := range map[string]string{
		"syntax":    `widget({`,
		"no widget": `const x = 1;`,
		"no create": `widget({})`,
		"twice":     `widget({create() {}}); widget({create() {}});`,
		"bad poll":  `widget({pollMS: "x", create() {}})`,
		"async api": `udeck.http.getJSON("http://x"); widget({create() {}})`,
	} {
		dir := writeWidgetScript(t, "bad", src)
		if _, err := loadScriptType(filepath.Join(dir, "bad.js")); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestScriptTypeCacheFollowsEdits(t *testing.T) {
	dir := writeWidgetScript(t, "edit", `widget({pollMS: 1000, create() { return {draw() {}} }})`)
	path := filepath.Join(dir, "edit.js")
	a, err := loadScriptType(path)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := loadScriptType(path); b != a {
		t.Fatal("unchanged script must come from the cache")
	}
	if err := os.WriteFile(path, []byte(`widget({pollMS: 2000, create() { return {draw() {}} }})`), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := loadScriptType(path)
	if err != nil || b == a || b.pollMS != 2000 {
		t.Fatalf("edited script: %+v %v", b, err)
	}
}

func TestManagerRunsScriptWidgets(t *testing.T) {
	dir := writeWidgetScript(t, "counter", `widget({
		gestures: {tap: true, hold: true},
		create(w) {
			let n = 0;
			return {
				gesture(kind) { if (kind === "tap") n++; },
				draw(c) { c.clear(n > 0 ? "#ffffff" : "#000000"); },
			};
		},
	});`)
	cfg := &config.Config{
		WidgetsDir: dir,
		Buttons:    []config.Button{{RuntimeID: "c", Widget: &config.Widget{Type: "counter"}}},
	}
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if o, ok := m.gestureOptions("c"); !ok || !o.HasTap || !o.HasHold {
		t.Fatalf("gestures %+v", o)
	}
	buttons, err := m.ResolveButtons(cfg.Buttons)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m.HandleEvent("c", true, now)
	m.HandleEvent("c", false, now.Add(10*time.Millisecond))
	m.Tick(cfg.Buttons, now.Add(time.Second))
	waitFor(t, "tap", func() bool {
		img, ok := icons.Image(buttons[0].Icon)
		return ok && img.At(1, 1) == color.RGBA{0xff, 0xff, 0xff, 0xff}
	})

	if _, err := NewManager(&config.Config{WidgetsDir: dir, Buttons: []config.Button{{RuntimeID: "x", Widget: &config.Widget{Type: "missing"}}}}); err == nil {
		t.Fatal("missing script must fail")
	}
}

func TestExampleScriptWidgets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	path := filepath.Join("..", "..", "examples", "widgets", "http_status.js")
	typ, err := loadScriptType(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := typ.validate(&config.Widget{Params: map[string]any{"url": "ftp://x"}}); err == nil {
		t.Fatal("bad url must be rejected")
	}
	btn := config.Button{Widget: &config.Widget{Type: "http_status", Params: map[string]any{"url": srv.URL}}}
	w, err := newScriptWidget("ex", typ, btn, map[string]any{"timeout": 2000}, func(string) {})
	if err != nil || w.failed {
		t.Fatalf("create: %v %v", err, w.lastErr)
	}
	defer w.Close()
	live := color.RGBA{0x2e, 0xcc, 0x40, 0xff}
	waitFor(t, "status check", func() bool {
		img, _ := icons.Image(w.IconPath())
		for y := 20; y < 116; y++ {
			for x := 50; x < 146; x++ {
				if img.At(x, y) == live {
					return true
				}
			}
		}
		return false
	})
}
