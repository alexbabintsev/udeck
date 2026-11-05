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

const defaultMicrophonePoll = 500 * time.Millisecond

// MicrophoneWidget shows whether the default input device is live or
// muted and toggles it on tap, for every app at once (unlike in-app
// mute buttons of Zoom, Meet or Discord).
type MicrophoneWidget struct {
	*widgetBase
	sys   audioSystem
	title string

	mu      sync.Mutex
	state   macos.AudioState
	err     error
	restore float64
}

func NewMicrophoneWidget(id string, cfg *config.Widget, notify func(string)) (*MicrophoneWidget, error) {
	base, err := newWidgetBase(id, "microphone", notify)
	if err != nil {
		return nil, err
	}
	w := &MicrophoneWidget{widgetBase: base, sys: systemAudio, title: strings.TrimSpace(cfg.Title)}
	w.read()
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write microphone icon: %w", err)
	}
	w.every(pollOr(cfg.PollMS, defaultMicrophonePoll), func(time.Time) { w.refresh() })
	return w, nil
}

func (w *MicrophoneWidget) resume() { w.refresh() }

func (w *MicrophoneWidget) HandleGesture(kind ig.Kind) {
	if kind != ig.Tap || w.isClosed() {
		return
	}
	w.mu.Lock()
	if w.err == nil {
		if err := toggleMute(w.sys, macos.Input, w.state, &w.restore); err != nil {
			debug.Logf("microphone mute: %v", err)
		}
	}
	w.mu.Unlock()
	w.refresh()
}

func (w *MicrophoneWidget) read() {
	st, err := w.sys.read(macos.Input)
	w.mu.Lock()
	w.state, w.err = st, err
	w.mu.Unlock()
}

func (w *MicrophoneWidget) refresh() {
	w.read()
	if _, err := w.publish(w.renderImage()); err != nil {
		debug.Logf("widget %s: %v", w.id, err)
	}
}

func (w *MicrophoneWidget) renderImage() *image.RGBA {
	w.mu.Lock()
	st, err := w.state, w.err
	w.mu.Unlock()
	return renderMicrophone(st, err, w.title)
}

// renderMicrophone: green mic when live, red crossed-out mic when muted.
// A configured title replaces the state word; the color still tells.
func renderMicrophone(st macos.AudioState, err error, title string) *image.RGBA {
	img := newCard()
	label := func(state string) string {
		if title != "" {
			return title
		}
		return state
	}
	switch {
	case err != nil:
		drawIconCard(img, "lucide/mic-off", styleDim, label("no mic"), styleDim)
	case isMuted(st):
		drawIconCard(img, "lucide/mic-off", styleAlert, label("muted"), styleAlert)
	default:
		drawIconCard(img, "lucide/mic", styleLive, label("live"), styleText)
	}
	return img
}
