package widgets

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

const (
	memoryWidgetW       = 196
	memoryWidgetH       = 196
	defaultMemoryPollMS = 2000
)

var execFile = exec.Command

type MemoryWidget struct {
	id       string
	apps     []string
	pollMS   time.Duration
	iconPath string
	notify   func(string)

	mu      sync.RWMutex
	percent int
	busy    bool
	closed  bool
}

func NewMemoryWidget(id string, cfg *config.Widget, notify func(string)) (*MemoryWidget, error) {
	dir, err := os.MkdirTemp("", "ulanzi-memory-widget-")
	if err != nil {
		return nil, fmt.Errorf("create widget temp dir: %w", err)
	}
	w := &MemoryWidget{
		id:       id,
		apps:     append([]string(nil), cfg.Apps...),
		pollMS:   pollDuration(cfg.PollMS),
		iconPath: filepath.Join(dir, "memory.png"),
		notify:   notify,
	}
	if err := w.updateMemory(); err != nil {
		_ = w.render()
	}
	go w.loop()
	return w, nil
}

func (w *MemoryWidget) IconPath() string {
	return w.iconPath
}

func (w *MemoryWidget) Press() {
	w.mu.Lock()
	if w.busy || w.closed {
		w.mu.Unlock()
		return
	}
	w.busy = true
	w.mu.Unlock()

	go func() {
		for _, app := range w.apps {
			_ = restartApp(app)
		}
		_ = w.updateMemory()
		w.mu.Lock()
		w.busy = false
		w.mu.Unlock()
		_ = w.render()
		w.notify(w.id)
	}()
}

func (w *MemoryWidget) Close() error {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	return os.RemoveAll(filepath.Dir(w.iconPath))
}

func (w *MemoryWidget) loop() {
	ticker := time.NewTicker(w.pollMS)
	defer ticker.Stop()
	for range ticker.C {
		w.mu.RLock()
		closed := w.closed
		w.mu.RUnlock()
		if closed {
			return
		}
		if err := w.updateMemory(); err == nil {
			w.notify(w.id)
		}
	}
}

func (w *MemoryWidget) updateMemory() error {
	percent, err := readMemoryPercent()
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.percent = percent
	w.mu.Unlock()
	return w.render()
}

func (w *MemoryWidget) render() error {
	w.mu.RLock()
	percent := w.percent
	busy := w.busy
	hasApps := len(w.apps) > 0
	w.mu.RUnlock()

	img := image.NewRGBA(image.Rect(0, 0, memoryWidgetW, memoryWidgetH))
	drawMemoryGradient(img)

	col := memoryColor(percent)
	drawCenteredFace(img, "RAM", memoryWidgetW/2, 24, color.RGBA{0x9a, 0x9a, 0x9a, 0xff}, basicfont.Face7x13)
	drawScaledCenteredText(img, fmt.Sprintf("%d%%", percent), image.Rect(14, 30, memoryWidgetW-14, 120), col)
	drawProgressBar(img, percent, col)
	if busy {
		drawCenteredFace(img, "RESTARTING", memoryWidgetW/2, memoryWidgetH-18, color.RGBA{0xe0, 0xa0, 0x20, 0xff}, basicfont.Face7x13)
	} else if hasApps {
		drawCenteredFace(img, "TAP TO RESTART", memoryWidgetW/2, memoryWidgetH-18, color.RGBA{0x90, 0x90, 0x90, 0xff}, basicfont.Face7x13)
	} else {
		drawCenteredFace(img, "MEMORY", memoryWidgetW/2, memoryWidgetH-18, color.RGBA{0x90, 0x90, 0x90, 0xff}, basicfont.Face7x13)
	}

	if err := writePNGAtomic(w.iconPath, img); err != nil {
		return fmt.Errorf("write memory widget icon: %w", err)
	}
	return nil
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

func drawMemoryGradient(img *image.RGBA) {
	top := color.RGBA{0x08, 0x0b, 0x10, 0xff}
	bottom := color.RGBA{0x12, 0x18, 0x21, 0xff}
	for y := 0; y < memoryWidgetH; y++ {
		ratio := float64(y) / float64(memoryWidgetH-1)
		c := color.RGBA{
			R: uint8(float64(top.R)*(1-ratio) + float64(bottom.R)*ratio),
			G: uint8(float64(top.G)*(1-ratio) + float64(bottom.G)*ratio),
			B: uint8(float64(top.B)*(1-ratio) + float64(bottom.B)*ratio),
			A: 0xff,
		}
		draw.Draw(img, image.Rect(0, y, memoryWidgetW, y+1), &image.Uniform{c}, image.Point{}, draw.Src)
	}
}

func drawCenteredFace(img *image.RGBA, text string, centerX, baselineY int, clr color.Color, face font.Face) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(clr), Face: face}
	width := d.MeasureString(text).Round()
	d.Dot = fixed.P(centerX-width/2, baselineY)
	d.DrawString(text)
}

func drawProgressBar(img *image.RGBA, percent int, clr color.Color) {
	bg := color.RGBA{0x22, 0x22, 0x22, 0xff}
	x := 22
	y := memoryWidgetH - 42
	w := memoryWidgetW - 44
	h := 12
	draw.Draw(img, image.Rect(x, y, x+w, y+h), &image.Uniform{bg}, image.Point{}, draw.Src)
	fill := w * percent / 100
	if fill < 4 {
		fill = 4
	}
	draw.Draw(img, image.Rect(x, y, x+fill, y+h), &image.Uniform{clr}, image.Point{}, draw.Src)
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

func readMemoryPercent() (int, error) {
	memsizeOut, err := execOut("/usr/sbin/sysctl", []string{"-n", "hw.memsize"}, 4*time.Second)
	if err != nil {
		return 0, err
	}
	vmstatOut, err := execOut("/usr/bin/vm_stat", nil, 4*time.Second)
	if err != nil {
		return 0, err
	}

	totalBytes, err := parseInt(strings.TrimSpace(memsizeOut))
	if err != nil {
		return 0, fmt.Errorf("parse memsize: %w", err)
	}

	pageSize := int64(16384)
	if matches := regexpMustCompile(`page size of (\d+) bytes`).FindStringSubmatch(vmstatOut); len(matches) == 2 {
		if v, err := parseInt(matches[1]); err == nil {
			pageSize = v
		}
	}

	stats := map[string]int64{}
	lineRE := regexpMustCompile(`^"?([^"]+)"?:\s+(\d+)\.?$`)
	for _, line := range strings.Split(vmstatOut, "\n") {
		m := lineRE.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		v, err := parseInt(m[2])
		if err != nil {
			continue
		}
		stats[strings.TrimSpace(m[1])] = v
	}

	free := stats["Pages free"] * pageSize
	inactive := stats["Pages inactive"] * pageSize
	speculative := stats["Pages speculative"] * pageSize
	purgeable := stats["Pages purgeable"] * pageSize
	trulyFree := free + inactive + speculative + purgeable
	used := totalBytes - trulyFree
	if used < 0 {
		used = 0
	}
	return int((used*100 + totalBytes/2) / totalBytes), nil
}

func execOut(cmd string, args []string, timeout time.Duration) (string, error) {
	c := execFile(cmd, args...)
	c.Env = os.Environ()
	_ = timeout
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseInt(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}

var reCache sync.Map

func regexpMustCompile(expr string) *regexp.Regexp {
	if v, ok := reCache.Load(expr); ok {
		return v.(*regexp.Regexp)
	}
	re := regexp.MustCompile(expr)
	reCache.Store(expr, re)
	return re
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
	_, err = execFile("/usr/bin/open", "-g", "-a", appName).Output()
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
	out, err := execFile("/usr/bin/osascript", "-e", `application "`+escapeAS(appName)+`" is running`).Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

func osa(script string) error {
	_, err := execFile("/usr/bin/osascript", "-e", script).Output()
	return err
}

func escapeAS(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
