package widgets

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os/exec"
	"strings"
	"sync"
	"time"

	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
)

const (
	memoryWidgetW       = 196
	memoryWidgetH       = 196
	defaultMemoryPollMS = 2000
	appCommandTimeout   = 10 * time.Second
)

// execCommandContext runs helper binaries; tests replace it.
var execCommandContext = exec.CommandContext

type MemoryWidget struct {
	*widgetBase
	apps   []string
	pollMS time.Duration
	// restart and readPercent are seams for tests.
	restart     func(app string) error
	readPercent func() (int, error)

	mu      sync.RWMutex
	percent int
	busy    bool
}

func NewMemoryWidget(id string, cfg *config.Widget, notify func(string)) (*MemoryWidget, error) {
	base, err := newWidgetBase(id, "memory-widget", "memory.png", notify)
	if err != nil {
		return nil, err
	}
	w := &MemoryWidget{
		widgetBase:  base,
		apps:        append([]string(nil), cfg.Apps...),
		pollMS:      pollDuration(cfg.PollMS),
		restart:     restartApp,
		readPercent: readMemoryPercent,
	}
	if percent, err := w.readPercent(); err == nil {
		w.percent = percent
	}
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write memory widget icon: %w", err)
	}
	w.every(w.pollMS, func(time.Time) { _, _ = w.updateMemory() })
	return w, nil
}

func (w *MemoryWidget) resume() { _, _ = w.updateMemory() }

func (w *MemoryWidget) HandleGesture(kind ig.Kind) {
	if kind != ig.Tap {
		return
	}
	w.mu.Lock()
	if w.busy || w.isClosed() {
		w.mu.Unlock()
		return
	}
	w.busy = true
	w.mu.Unlock()
	_, _ = w.render()

	go func() {
		for _, app := range w.apps {
			_ = w.restart(app)
		}
		w.mu.Lock()
		w.busy = false
		w.mu.Unlock()
		if _, err := w.updateMemory(); err != nil {
			_, _ = w.render()
		}
	}()
}

// updateMemory re-reads usage and publishes a new icon when it changed.
func (w *MemoryWidget) updateMemory() (bool, error) {
	percent, err := w.readPercent()
	if err != nil {
		return false, err
	}
	w.mu.Lock()
	if w.percent == percent && !w.busy {
		w.mu.Unlock()
		return false, nil
	}
	w.percent = percent
	w.mu.Unlock()
	return w.render()
}

func (w *MemoryWidget) render() (bool, error) {
	changed, err := w.publish(w.renderImage())
	if err != nil {
		return false, fmt.Errorf("write memory widget icon: %w", err)
	}
	return changed, nil
}

func (w *MemoryWidget) renderImage() *image.RGBA {
	w.mu.RLock()
	percent := w.percent
	busy := w.busy
	w.mu.RUnlock()

	img := image.NewRGBA(image.Rect(0, 0, memoryWidgetW, memoryWidgetH))
	draw.Draw(img, img.Bounds(), image.Black, image.Point{}, draw.Src)

	col := memoryColor(percent)
	drawMemoryRing(img, percent, col)
	if busy {
		_ = icons.DrawPackIcon(img, "lucide/refresh-cw", image.Rect(58, 58, 138, 138), col)
	} else {
		drawDashboardCenteredText(img, fmt.Sprintf("%d", percent), image.Rect(28, 48, memoryWidgetW-28, 146), col, 60)
	}
	return img
}

func pollDuration(ms int) time.Duration {
	if ms <= 0 {
		return defaultMemoryPollMS * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

func memoryColor(percent int) color.RGBA {
	switch {
	case percent >= 85:
		return color.RGBA{0xd7, 0x32, 0x32, 0xff}
	case percent >= 70:
		return color.RGBA{0xe0, 0xa0, 0x20, 0xff}
	default:
		return color.RGBA{0x0e, 0xab, 0x2d, 0xff}
	}
}

func drawCenteredFace(img *image.RGBA, text string, centerX, baselineY int, clr color.Color, face font.Face) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(clr), Face: face}
	width := d.MeasureString(text).Round()
	d.Dot = fixed.P(centerX-width/2, baselineY)
	d.DrawString(text)
}

func drawMemoryRing(img *image.RGBA, percent int, clr color.Color) {
	const (
		cx       = memoryWidgetW / 2
		cy       = memoryWidgetH / 2
		outerR   = 65.0
		innerR   = 55.0
		trackDeg = 360.0
	)

	track := color.RGBA{0x1f, 0x1f, 0x1f, 0xff}
	fillDeg := trackDeg * clampPercent(percent) / 100.0

	for y := 0; y < memoryWidgetH; y++ {
		dy := float64(y-cy) + 0.5
		for x := 0; x < memoryWidgetW; x++ {
			dx := float64(x-cx) + 0.5
			dist := math.Hypot(dx, dy)
			if dist < innerR || dist > outerR {
				continue
			}

			ang := math.Atan2(dy, dx)*180/math.Pi + 90
			if ang < 0 {
				ang += 360
			}

			if ang <= fillDeg {
				img.Set(x, y, clr)
			} else {
				img.Set(x, y, track)
			}
		}
	}
}

func clampPercent(percent int) float64 {
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return float64(percent)
}

func drawScaledCenteredText(dst *image.RGBA, text string, target image.Rectangle, clr color.Color) {
	face := basicfont.Face7x13
	d := &font.Drawer{Face: face}
	w := d.MeasureString(text).Round()
	h := face.Metrics().Height.Ceil()
	if w <= 0 || h <= 0 {
		return
	}

	src := image.NewRGBA(image.Rect(0, 0, w, h))
	d = &font.Drawer{
		Dst:  src,
		Src:  image.NewUniform(clr),
		Face: face,
		Dot:  fixed.P(0, face.Metrics().Ascent.Ceil()),
	}
	d.DrawString(text)

	scaleX := float64(target.Dx()) / float64(w)
	scaleY := float64(target.Dy()) / float64(h)
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}
	if scale < 1 {
		scale = 1
	}
	scaledW := int(float64(w) * scale)
	scaledH := int(float64(h) * scale)
	rect := image.Rect(
		target.Min.X+(target.Dx()-scaledW)/2,
		target.Min.Y+(target.Dy()-scaledH)/2,
		target.Min.X+(target.Dx()-scaledW)/2+scaledW,
		target.Min.Y+(target.Dy()-scaledH)/2+scaledH,
	)
	xdraw.NearestNeighbor.Scale(dst, rect, src, src.Bounds(), draw.Over, nil)
}

// vmStats is a snapshot of system memory: total bytes and page counts.
type vmStats struct {
	total, pageSize                        int64
	free, inactive, speculative, purgeable int64
}

// readMemoryPercent reads memory counters in-process (readVMStats) rather
// than by running sysctl and vm_stat every poll.
func readMemoryPercent() (int, error) {
	s, err := readVMStats()
	if err != nil {
		return 0, err
	}
	return s.usedPercent()
}

// usedPercent counts everything the system can reclaim without paging
// (free, inactive, speculative, purgeable) as available.
func (s vmStats) usedPercent() (int, error) {
	if s.total <= 0 {
		return 0, fmt.Errorf("invalid total memory %d", s.total)
	}
	trulyFree := (s.free + s.inactive + s.speculative + s.purgeable) * s.pageSize
	used := max(s.total-trulyFree, 0)
	return int((used*100 + s.total/2) / s.total), nil
}

func execOut(cmd string, args []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := execCommandContext(ctx, cmd, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func restartApp(appName string) error {
	if appName == "" {
		return nil
	}
	running, err := isAppRunning(appName)
	if err != nil || !running {
		return err
	}
	if err := osa(`tell application "` + escapeAS(appName) + `" to quit`); err != nil {
		return err
	}
	if err := waitUntilQuit(appName, 15*time.Second); err != nil {
		return err
	}
	_, err = execOut("/usr/bin/open", []string{"-g", "-a", appName}, appCommandTimeout)
	return err
}

func waitUntilQuit(appName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := isAppRunning(appName)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s to quit", appName)
}

func isAppRunning(appName string) (bool, error) {
	out, err := execOut("/usr/bin/osascript", []string{"-e", `application "` + escapeAS(appName) + `" is running`}, appCommandTimeout)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "true", nil
}

func osa(script string) error {
	_, err := execOut("/usr/bin/osascript", []string{"-e", script}, appCommandTimeout)
	return err
}

func escapeAS(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
