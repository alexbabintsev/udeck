package widgets

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/macos"
)

// The system monitor widgets: cpu, disk, network and battery (memory has
// its own widget). Each polls one reading, renders it and sends the key
// only when the picture changed.

// systemSource is the macOS API as the monitors use it; each widget
// copies systemStats when created, so tests can substitute it.
type systemSource struct {
	cpu     func() (macos.CPUTicks, error)
	net     func() ([]macos.NetCounter, error)
	disk    func(string) (macos.DiskSpace, error)
	battery func() (macos.BatteryState, error)
	now     func() time.Time
}

var systemStats = systemSource{
	cpu:     macos.ReadCPUTicks,
	net:     macos.NetCounters,
	disk:    macos.Disk,
	battery: macos.Battery,
	now:     time.Now,
}

var sysmonDefaultPoll = map[string]time.Duration{
	"cpu":     2 * time.Second,
	"network": 2 * time.Second,
	"disk":    time.Minute,
	"battery": 30 * time.Second,
}

// SystemWidget is one system monitor key.
type SystemWidget struct {
	*widgetBase
	mu    sync.Mutex
	frame func() *image.RGBA // reads the system and draws; holds the previous reading
}

func NewSystemWidget(id string, cfg *config.Widget, notify func(string)) (*SystemWidget, error) {
	frame, err := sysmonFrame(cfg, systemStats)
	if err != nil {
		return nil, err
	}
	base, err := newWidgetBase(id, cfg.Type, notify)
	if err != nil {
		return nil, err
	}
	w := &SystemWidget{widgetBase: base, frame: frame}
	if _, err := w.writeIcon(w.draw()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write %s icon: %w", cfg.Type, err)
	}
	w.every(pollOr(cfg.PollMS, sysmonDefaultPoll[cfg.Type]), func(time.Time) { w.refresh() })
	return w, nil
}

func (w *SystemWidget) HandleGesture(ig.Kind) {}

func (w *SystemWidget) resume() { w.refresh() }

func (w *SystemWidget) draw() *image.RGBA {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.frame()
}

func (w *SystemWidget) refresh() {
	if _, err := w.publish(w.draw()); err != nil {
		debug.Logf("widget %s: %v", w.id, err)
	}
}

// sysmonFrame returns the read-and-draw function of a monitor type.
func sysmonFrame(cfg *config.Widget, src systemSource) (func() *image.RGBA, error) {
	switch cfg.Type {
	case "cpu":
		var prev macos.CPUTicks
		return func() *image.RGBA {
			cur, err := src.cpu()
			if err != nil {
				return renderSysmonError("lucide/cpu", "no data")
			}
			// The first reading has no previous one and shows the
			// average since boot until the next poll.
			usage := cur.Usage(prev)
			prev = cur
			return renderPercentGauge(usage, cpuColor, "lucide/cpu", "")
		}, nil
	case "disk":
		path := strings.TrimSpace(cfg.Path)
		if path == "" {
			path = "/"
		}
		path = expandWidgetPath(path)
		title := strings.TrimSpace(cfg.Title)
		return func() *image.RGBA {
			d, err := src.disk(path)
			if err != nil || d.Total == 0 {
				return renderSysmonError("lucide/hard-drive", "no disk")
			}
			return renderDisk(d, title)
		}, nil
	case "network":
		prefixes := netPrefixes(cfg.Interface)
		var prevRx, prevTx uint64
		var prevAt time.Time
		return func() *image.RGBA {
			list, err := src.net()
			if err != nil {
				return renderSysmonError("lucide/arrow-down-up", "no data")
			}
			rx, tx := sumNet(list, prefixes)
			now := src.now()
			var down, up float64
			known := !prevAt.IsZero() && rx >= prevRx && tx >= prevTx
			if secs := now.Sub(prevAt).Seconds(); known && secs > 0 {
				down, up = float64(rx-prevRx)/secs, float64(tx-prevTx)/secs
			}
			prevRx, prevTx, prevAt = rx, tx, now
			return renderNetwork(down, up, known)
		}, nil
	case "battery":
		return func() *image.RGBA {
			b, err := src.battery()
			if err != nil || !b.Present {
				return renderSysmonError("lucide/battery", "no battery")
			}
			return renderBattery(b)
		}, nil
	}
	return nil, fmt.Errorf("unknown system monitor %q", cfg.Type)
}

// netPrefixes lists the interface name prefixes to count: the configured
// ones, or en* (Wi-Fi, Ethernet). VPN tunnels (utun*) carry traffic that
// en* already counts, and lo0 is local.
func netPrefixes(cfg string) []string {
	var out []string
	for _, p := range strings.Split(cfg, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = []string{"en"}
	}
	return out
}

func sumNet(list []macos.NetCounter, prefixes []string) (rx, tx uint64) {
	for _, c := range list {
		for _, p := range prefixes {
			if strings.HasPrefix(c.Name, p) {
				rx += c.RxByte
				tx += c.TxByte
				break
			}
		}
	}
	return rx, tx
}

func cpuColor(frac float64) color.RGBA {
	switch {
	case frac >= 0.85:
		return styleAlert
	case frac >= 0.6:
		return styleWarn
	default:
		return styleLive
	}
}

func diskColor(frac float64) color.RGBA {
	switch {
	case frac >= 0.9:
		return styleAlert
	case frac >= 0.8:
		return styleWarn
	default:
		return styleLive
	}
}

// renderPercentGauge draws frac as a ring and a percentage, with an icon
// or, when caption is set, a caption under it.
func renderPercentGauge(frac float64, band func(float64) color.RGBA, iconID, caption string) *image.RGBA {
	img := newCard()
	frac = min(max(frac, 0), 1)
	value := fmt.Sprintf("%d", int(math.Round(frac*100)))
	if caption != "" {
		drawGaugeCardText(img, frac, band(frac), value, styleText, caption, styleDim)
	} else {
		drawGaugeCard(img, frac, band(frac), value, styleText, iconID, styleDim)
	}
	return img
}

// renderDisk: the free space large, the ring how full the volume is.
func renderDisk(d macos.DiskSpace, title string) *image.RGBA {
	img := newCard()
	caption := "free"
	if title != "" {
		caption = title
	}
	used := d.Used()
	drawGaugeCardText(img, used, diskColor(used), formatSize(d.Free), styleText, caption, styleDim)
	return img
}

func renderSysmonError(iconID, label string) *image.RGBA {
	img := newCard()
	drawIconCard(img, iconID, styleDim, label, styleDim)
	return img
}

// renderNetwork draws download over upload, each an arrow and a rate.
func renderNetwork(down, up float64, known bool) *image.RGBA {
	img := newCard()
	rows := []struct {
		icon string
		rate float64
		clr  color.RGBA
		top  int
	}{
		{"lucide/arrow-down", down, styleAccent, 40},
		{"lucide/arrow-up", up, styleLive, 108},
	}
	for _, r := range rows {
		text := "–"
		if known {
			text = formatRate(r.rate)
		}
		drawIcon(img, r.icon, FromLeft(8), FromTop(r.top), 44, 44, r.clr)
		drawText(img, text, FromLeft(54), FromTop(r.top), img.Bounds().Dx()-60, 44, AlignMiddleLeft, styleText, 30, true)
	}
	return img
}

// renderBattery: the charge on a ring, green, amber from 20%, red from
// 10%; a bolt while charging, a plug when held on power.
func renderBattery(b macos.BatteryState) *image.RGBA {
	img := newCard()
	frac := float64(b.Percent) / 100
	clr := styleLive
	switch {
	case b.Percent <= 10 && !b.OnAC:
		clr = styleAlert
	case b.Percent <= 20 && !b.OnAC:
		clr = styleWarn
	}
	icon, iconClr := "lucide/battery", color.RGBA(styleDim)
	switch {
	case b.Charging:
		icon, iconClr = "lucide/battery-charging", styleLive
	case b.OnAC:
		icon = "lucide/plug-zap"
	}
	drawGaugeCard(img, frac, clr, fmt.Sprintf("%d", b.Percent), styleText, icon, iconClr)
	return img
}

// formatSize writes a size compactly in decimal units, as Finder counts
// them: 151G, 2.4T, 820M.
func formatSize(b uint64) string {
	v, unit := float64(b), ""
	for _, u := range []string{"K", "M", "G", "T"} {
		if v < 1000 {
			break
		}
		v, unit = v/1000, u
	}
	if unit == "" {
		return fmt.Sprintf("%.0f", v)
	}
	return compactUnit(v, unit)
}

// formatRate writes bytes per second: 0 B/s, 850 KB/s, 1.2 MB/s.
func formatRate(bps float64) string {
	switch {
	case bps < 1000:
		return fmt.Sprintf("%.0f B/s", bps)
	case bps < 1e6:
		return compactUnit(bps/1e3, " KB/s")
	case bps < 1e9:
		return compactUnit(bps/1e6, " MB/s")
	default:
		return compactUnit(bps/1e9, " GB/s")
	}
}

func compactUnit(v float64, unit string) string {
	if v < 10 {
		return fmt.Sprintf("%.1f%s", v, unit)
	}
	return fmt.Sprintf("%.0f%s", v, unit)
}
