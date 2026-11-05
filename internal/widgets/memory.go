package widgets

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"sync"
	"time"

	ig "github.com/alexbabintsev/udeck/internal/gestures"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/alexbabintsev/udeck/internal/config"
)

const (
	memoryWidgetW       = 196
	memoryWidgetH       = 196
	defaultMemoryPollMS = 2000
)

type MemoryWidget struct {
	*widgetBase
	poll time.Duration
	// readPercent is a seam for tests.
	readPercent func() (int, error)

	mu      sync.RWMutex
	percent int
}

func NewMemoryWidget(id string, cfg *config.Widget, notify func(string)) (*MemoryWidget, error) {
	base, err := newWidgetBase(id, "memory-widget", notify)
	if err != nil {
		return nil, err
	}
	w := &MemoryWidget{
		widgetBase:  base,
		poll:        pollDuration(cfg.PollMS),
		readPercent: readMemoryPercent,
	}
	if percent, err := w.readPercent(); err == nil {
		w.percent = percent
	}
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write memory widget icon: %w", err)
	}
	w.every(w.poll, func(time.Time) { _, _ = w.updateMemory() })
	return w, nil
}

func (w *MemoryWidget) resume() { _, _ = w.updateMemory() }

func (w *MemoryWidget) HandleGesture(ig.Kind) {}

// updateMemory re-reads usage and publishes a new icon when it changed.
func (w *MemoryWidget) updateMemory() (bool, error) {
	percent, err := w.readPercent()
	if err != nil {
		return false, err
	}
	w.mu.Lock()
	if w.percent == percent {
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
	w.mu.RUnlock()

	img := newCard()
	col := memoryColor(percent)
	drawGaugeCard(img, clampPercent(percent)/100, col, fmt.Sprintf("%d", percent), styleText, "lucide/memory-stick", styleDim)
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
		return styleAlert
	case percent >= 70:
		return styleWarn
	default:
		return styleLive
	}
}

func drawCenteredFace(img *image.RGBA, text string, centerX, baselineY int, clr color.Color, face font.Face) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(clr), Face: face}
	width := d.MeasureString(text).Round()
	d.Dot = fixed.P(centerX-width/2, baselineY)
	d.DrawString(text)
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
