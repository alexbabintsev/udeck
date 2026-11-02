package widgets

import (
	"errors"
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

const (
	defaultVolumePoll = 500 * time.Millisecond
	defaultVolumeStep = 5
	// Volume up/down repeat while held: first after volumeHoldDelay,
	// then every volumeRepeatEvery.
	volumeHoldDelay   = 400 * time.Millisecond
	volumeRepeatEvery = 120 * time.Millisecond
)

// VolumeWidget shows the output volume of the default device on a ring
// and mutes it (action: mute, the default) or steps it (up, down).
type VolumeWidget struct {
	*widgetBase
	sys    audioSystem
	action string
	step   float64

	mu      sync.Mutex
	state   macos.AudioState
	err     error
	restore float64
}

func NewVolumeWidget(id string, cfg *config.Widget, notify func(string)) (*VolumeWidget, error) {
	base, err := newWidgetBase(id, "volume", notify)
	if err != nil {
		return nil, err
	}
	step := cfg.Step
	if step == 0 {
		step = defaultVolumeStep
	}
	action := cfg.Action
	if action == "" {
		action = "mute"
	}
	w := &VolumeWidget{widgetBase: base, sys: systemAudio, action: action, step: float64(step) / 100}
	w.read()
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write volume icon: %w", err)
	}
	w.every(pollOr(cfg.PollMS, defaultVolumePoll), func(time.Time) { w.refresh() })
	return w, nil
}

// volumeGestures: up/down repeat while held; mute is a plain tap.
func volumeGestures(cfg *config.Widget) ig.Options {
	if cfg.Action == "up" || cfg.Action == "down" {
		return ig.Options{HasTap: true, HasHold: true, HasRepeat: true, HoldDelay: volumeHoldDelay, RepeatEvery: volumeRepeatEvery}
	}
	return ig.Options{HasTap: true}
}

func (w *VolumeWidget) resume() { w.refresh() }

func (w *VolumeWidget) HandleGesture(kind ig.Kind) {
	if w.isClosed() || kind == ig.None || kind == ig.DoubleTap {
		return
	}
	w.mu.Lock()
	st, err := w.state, w.err
	w.mu.Unlock()
	if err != nil {
		w.refresh() // the device may have changed meanwhile
		return
	}
	switch w.action {
	case "mute":
		if kind == ig.Tap {
			w.mu.Lock()
			err = toggleMute(w.sys, macos.Output, st, &w.restore)
			w.mu.Unlock()
		}
	case "up", "down":
		delta := w.step
		if w.action == "down" {
			delta = -delta
		}
		if st.CanMute && st.Muted && delta > 0 {
			// Like the keyboard: turning it up unmutes.
			err = w.sys.setMuted(macos.Output, false)
		}
		if err == nil {
			// Snap to the step grid so repeated presses land on round values.
			next := math.Round((st.Volume+delta)/w.step) * w.step
			err = w.sys.setVolume(macos.Output, next)
		}
	}
	if err != nil {
		debug.Logf("volume %s: %v", w.action, err)
	}
	w.refresh()
}

func (w *VolumeWidget) read() {
	st, err := w.sys.read(macos.Output)
	w.mu.Lock()
	w.state, w.err = st, err
	w.mu.Unlock()
}

func (w *VolumeWidget) refresh() {
	w.read()
	if _, err := w.publish(w.renderImage()); err != nil {
		debug.Logf("widget %s: %v", w.id, err)
	}
}

func (w *VolumeWidget) renderImage() *image.RGBA {
	w.mu.Lock()
	st, err := w.state, w.err
	w.mu.Unlock()
	return renderVolume(st, err, w.action)
}

// renderVolume draws the mute key as a level ring with the percentage,
// and the up/down keys as a large plus or minus with the percentage as a
// small label, so the three keys of a volume row read apart at a glance.
func renderVolume(st macos.AudioState, err error, action string) *image.RGBA {
	img := newCard()
	pct := fmt.Sprintf("%d%%", int(math.Round(st.Volume*100)))
	if action == "up" || action == "down" {
		iconClr, label, labelClr := styleText, pct, styleDim
		switch {
		case errors.Is(err, macos.ErrFixedVolume):
			iconClr, label = styleDim, "fixed"
		case err != nil:
			iconClr, label = styleDim, "no audio"
		case isMuted(st):
			label, labelClr = "muted", styleAlert
		}
		drawVolumeStepIcon(img, action, iconClr)
		drawCardLabel(img, label, labelClr)
		return img
	}
	switch {
	case errors.Is(err, macos.ErrFixedVolume):
		drawIconCard(img, "lucide/volume-off", styleDim, "fixed", styleDim)
	case err != nil:
		drawIconCard(img, "lucide/volume-x", styleDim, "no audio", styleDim)
	case isMuted(st):
		drawGaugeCard(img, 0, styleTrack, "", nil, "lucide/volume-x", styleAlert)
	default:
		drawGaugeCard(img, st.Volume, styleAccent, strings.TrimSuffix(pct, "%"), styleText, "lucide/volume-2", styleDim)
	}
	return img
}

// drawVolumeStepIcon draws a speaker with a smaller plus (up) or minus
// (down) beside it, in the icon area of an icon card. The sign is
// centred on the speaker vertically and drawn with the speaker's line
// weight; the offsets centre the visible
// speaker (x 2..11 of Lucide's 24-unit box) and sign (5..19) as a pair.
func drawVolumeStepIcon(img *image.RGBA, action string, clr color.Color) {
	const signSize = 72
	sign := "lucide/plus"
	if action == "down" {
		sign = "lucide/minus"
	}
	drawIcon(img, "lucide/volume", CenterX(-10), FromTop(styleIconTop), styleIconSize, styleIconSize, clr)
	signTop := styleIconTop + (styleIconSize-signSize)/2
	drawIconStroke(img, sign, CenterX(29), FromTop(signTop), signSize, signSize, clr, styleIconSize)
}

// pollOr converts poll_ms, falling back to def.
func pollOr(ms int, def time.Duration) time.Duration {
	if ms <= 0 {
		return def
	}
	return time.Duration(ms) * time.Millisecond
}
