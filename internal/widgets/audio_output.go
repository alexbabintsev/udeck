package widgets

import (
	"fmt"
	"image"
	"strings"
	"sync"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
	"github.com/alexbabintsev/udeck/internal/debug"
	ig "github.com/alexbabintsev/udeck/internal/gestures"
	"github.com/alexbabintsev/udeck/internal/macos"
)

const defaultAudioOutputPoll = time.Second

// AudioOutputWidget shows the default output device and switches to the
// next one on tap: through the configured devices, or all of them.
type AudioOutputWidget struct {
	*widgetBase
	sys     audioSystem
	devices []string // name patterns, lower case

	mu   sync.Mutex
	name string
	err  error
}

func NewAudioOutputWidget(id string, cfg *config.Widget, notify func(string)) (*AudioOutputWidget, error) {
	base, err := newWidgetBase(id, "audio-output", notify)
	if err != nil {
		return nil, err
	}
	w := &AudioOutputWidget{widgetBase: base, sys: systemAudio}
	for _, d := range cfg.Devices {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			w.devices = append(w.devices, d)
		}
	}
	w.read()
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write audio output icon: %w", err)
	}
	w.every(pollOr(cfg.PollMS, defaultAudioOutputPoll), func(time.Time) { w.refresh() })
	return w, nil
}

func (w *AudioOutputWidget) resume() { w.refresh() }

func (w *AudioOutputWidget) HandleGesture(kind ig.Kind) {
	if kind != ig.Tap || w.isClosed() {
		return
	}
	if err := w.switchNext(); err != nil {
		debug.Logf("audio output: %v", err)
	}
	w.refresh()
}

// switchNext makes the device after the current one the default.
func (w *AudioOutputWidget) switchNext() error {
	all, err := w.sys.devices(macos.Output)
	if err != nil {
		return err
	}
	candidates := pickDevices(all, w.devices)
	if len(candidates) == 0 {
		return fmt.Errorf("none of the devices %q is connected", w.devices)
	}
	w.mu.Lock()
	current := w.name
	w.mu.Unlock()
	next := candidates[0]
	for i, d := range candidates {
		if d.Name == current {
			next = candidates[(i+1)%len(candidates)]
			break
		}
	}
	return w.sys.setDefault(macos.Output, next.ID)
}

// pickDevices keeps, in the order of patterns, the devices whose name
// contains a pattern; with no patterns it keeps all of them.
func pickDevices(all []macos.Device, patterns []string) []macos.Device {
	if len(patterns) == 0 {
		return all
	}
	var out []macos.Device
	seen := map[uint32]bool{}
	for _, p := range patterns {
		for _, d := range all {
			if !seen[d.ID] && strings.Contains(strings.ToLower(d.Name), p) {
				seen[d.ID] = true
				out = append(out, d)
			}
		}
	}
	return out
}

func (w *AudioOutputWidget) read() {
	st, err := w.sys.read(macos.Output)
	if st.Device != "" {
		// The name is known even when the volume is fixed.
		err = nil
	}
	w.mu.Lock()
	w.name, w.err = st.Device, err
	w.mu.Unlock()
}

func (w *AudioOutputWidget) refresh() {
	w.read()
	if _, err := w.publish(w.renderImage()); err != nil {
		debug.Logf("widget %s: %v", w.id, err)
	}
}

func (w *AudioOutputWidget) renderImage() *image.RGBA {
	w.mu.Lock()
	name, err := w.name, w.err
	w.mu.Unlock()
	return renderAudioOutput(name, err)
}

func renderAudioOutput(name string, err error) *image.RGBA {
	img := newCard()
	if err != nil || name == "" {
		drawIconCard(img, "lucide/speaker", styleDim, "no output", styleDim)
		return img
	}
	drawIconCard(img, audioDeviceIcon(name), styleAccent, name, styleText)
	return img
}

// audioDeviceIcon picks headphones for headsets and earbuds.
func audioDeviceIcon(name string) string {
	n := strings.ToLower(name)
	for _, k := range []string{"airpods", "headphone", "headset", "buds", "beats", "наушник"} {
		if strings.Contains(n, k) {
			return "lucide/headphones"
		}
	}
	return "lucide/speaker"
}
