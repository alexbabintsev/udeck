package widgets

import (
	"context"
	"errors"
	"image"
	"image/color"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/icons"

	"github.com/alexbabintsev/udeck/internal/config"
	ig "github.com/alexbabintsev/udeck/internal/gestures"
)

// recorder collects notify calls.
type recorder struct {
	mu  sync.Mutex
	ids []string
}

func (r *recorder) notify(id string) {
	r.mu.Lock()
	r.ids = append(r.ids, id)
	r.mu.Unlock()
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ids)
}

func testBase(t *testing.T, notify func(string)) *widgetBase {
	t.Helper()
	b, err := newWidgetBase("test", "test", notify)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// readIcon returns the image a widget rendered under ref.
func readIcon(t *testing.T, ref string) image.Image {
	t.Helper()
	img, ok := icons.Image(ref)
	if !ok {
		t.Fatalf("no icon %q", ref)
	}
	return img
}

func rgbaAt(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

func anyPixel(img image.Image, r image.Rectangle, pred func(color.RGBA) bool) bool {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if pred(rgbaAt(img, x, y)) {
				return true
			}
		}
	}
	return false
}

func TestMemoryWidgetRendersRingAndDigits(t *testing.T) {
	w := &MemoryWidget{widgetBase: testBase(t, nil), percent: 72}
	changed, err := w.render()
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	img := readIcon(t, w.iconPath)
	if bg := rgbaAt(img, 8, 8); bg.R > 4 || bg.G > 4 || bg.B > 4 {
		t.Fatalf("expected black background, got %#v", bg)
	}
	// The ring spans radii styleRingInner..styleRingOuter; sample its top.
	if ring := rgbaAt(img, memoryWidgetW/2, memoryWidgetH/2-66); ring.R < 100 || ring.G < 100 {
		t.Fatalf("expected colored ring pixel, got %#v", ring)
	}
	if !anyPixel(img, image.Rect(56, 68, 140, 128), func(c color.RGBA) bool { return c.R > 100 || c.G > 100 }) {
		t.Fatal("expected center digits")
	}
	if changed, _ := w.render(); changed {
		t.Fatal("identical render must not report a change")
	}
}

func TestMemoryWidgetUpdateMemory(t *testing.T) {
	rec := &recorder{}
	pct, failing := 50, false
	w := &MemoryWidget{widgetBase: testBase(t, rec.notify), readPercent: func() (int, error) {
		if failing {
			return 0, errors.New("vm_stat failed")
		}
		return pct, nil
	}}
	if changed, err := w.updateMemory(); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if changed, _ := w.updateMemory(); changed {
		t.Fatal("same percentage must not re-render")
	}
	pct = 90
	if changed, _ := w.updateMemory(); !changed || rec.count() != 2 {
		t.Fatalf("changed=%v notifications=%d", changed, rec.count())
	}
	failing = true
	if _, err := w.updateMemory(); err == nil {
		t.Fatal("expected read error")
	}
}

func TestNewMemoryWidgetPollsAndStopsOnClose(t *testing.T) {
	w, err := NewMemoryWidget("m", &config.Widget{Type: "memory", PollMS: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := icons.Image(w.IconPath()); !ok {
		t.Fatal("initial icon missing")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := icons.Image(w.IconPath()); ok {
		t.Fatal("Close must drop the icon")
	}
	if err := w.Close(); err != nil {
		t.Fatal("second Close must be safe")
	}
	w.HandleGesture(ig.Tap) // closed: ignored
}

func TestMemoryHelpers(t *testing.T) {
	if pollDuration(0) != defaultMemoryPollMS*time.Millisecond || pollDuration(5) != 5*time.Millisecond {
		t.Fatal("pollDuration")
	}
	if memoryColor(90) == memoryColor(75) || memoryColor(75) == memoryColor(10) {
		t.Fatal("memoryColor must have three bands")
	}
	if clampPercent(-1) != 0 || clampPercent(150) != 100 || clampPercent(42) != 42 {
		t.Fatal("clampPercent")
	}
	img := image.NewRGBA(image.Rect(0, 0, 100, 40))
	drawScaledCenteredText(img, "42", img.Bounds(), color.White)
	drawScaledCenteredText(img, "", img.Bounds(), color.White)
	if !anyPixel(img, img.Bounds(), func(c color.RGBA) bool { return c.R > 0 }) {
		t.Fatal("expected scaled text pixels")
	}
}

// fakeExec routes helper commands to canned outputs keyed by argv[0].
func fakeExec(t *testing.T, outputs map[string]string) *[]string {
	t.Helper()
	var calls []string
	old := execCommandContext
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, name)
		out, ok := outputs[name]
		if !ok {
			return exec.CommandContext(ctx, "false")
		}
		return exec.CommandContext(ctx, "printf", "%s", out)
	}
	t.Cleanup(func() { execCommandContext = old })
	return &calls
}

func TestVMStatsUsedPercent(t *testing.T) {
	// total 100 pages, 50 reclaimable -> 50% used.
	s := vmStats{total: 1638400, pageSize: 16384, free: 10, inactive: 20, speculative: 5, purgeable: 15}
	if got, err := s.usedPercent(); err != nil || got != 50 {
		t.Fatalf("got %d err=%v", got, err)
	}
	s.free = 1000 // more reclaimable than total clamps to 0
	if got, err := s.usedPercent(); err != nil || got != 0 {
		t.Fatalf("got %d err=%v", got, err)
	}
	if _, err := (vmStats{}).usedPercent(); err == nil {
		t.Fatal("expected error for zero total")
	}
}

func TestReadMemoryPercentLive(t *testing.T) {
	if runtime.GOOS != "darwin" {
		if _, err := readMemoryPercent(); err == nil {
			t.Fatal("expected unsupported error")
		}
		return
	}
	s, err := readVMStats()
	if err != nil {
		t.Fatal(err)
	}
	if s.total <= 0 || s.pageSize <= 0 {
		t.Fatalf("implausible stats %+v", s)
	}
	if got, err := readMemoryPercent(); err != nil || got < 1 || got > 100 {
		t.Fatalf("got %d err=%v", got, err)
	}
}
