package widgets

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
	"github.com/alexbabintsev/udeck/internal/debug"
	ig "github.com/alexbabintsev/udeck/internal/gestures"
)

// The timer, stopwatch and pomodoro widgets share one engine: tap starts
// and pauses, hold resets. A countdown that runs out plays a system sound
// and turns red (timer) or moves on to the next phase (pomodoro).

const (
	timerTick      = 200 * time.Millisecond
	timerHoldDelay = 600 * time.Millisecond
)

// Timer engine hooks; each widget copies them when created, so tests can
// substitute them.
var (
	timerNow   = time.Now
	timerAfter = func(d time.Duration, f func()) func() {
		t := time.AfterFunc(d, f)
		return func() { t.Stop() }
	}
	timerSound = playSystemSound
)

// timerPhase is one countdown of a timer or pomodoro.
type timerPhase struct {
	name  string // pomodoro label: "work", "break", "long break"
	dur   time.Duration
	clr   color.RGBA
	round int // pomodoro work round, from 1
}

type timerState int

const (
	timerIdle timerState = iota
	timerRunning
	timerPaused
	timerDone // a timer ran out; tap resets it
)

// TimerWidget is a countdown timer, a stopwatch or a pomodoro.
type TimerWidget struct {
	*widgetBase
	kind   string       // timer | stopwatch | pomodoro
	phases []timerPhase // none for the stopwatch
	rounds int
	sound  string
	now    func() time.Time
	after  func(time.Duration, func()) func()
	play   func(string)

	mu        sync.Mutex
	state     timerState
	phase     int
	acc       time.Duration // run time before since
	since     time.Time     // start of the current run
	stopAlarm func()
	lastKey   string
}

func NewTimerWidget(id string, cfg *config.Widget, notify func(string)) (*TimerWidget, error) {
	phases, err := timerPhases(cfg)
	if err != nil {
		return nil, err
	}
	base, err := newWidgetBase(id, cfg.Type, notify)
	if err != nil {
		return nil, err
	}
	sound := strings.TrimSpace(cfg.Sound)
	if sound == "" {
		sound = config.DefaultTimerSound
	}
	w := &TimerWidget{
		widgetBase: base,
		kind:       cfg.Type,
		phases:     phases,
		rounds:     max(cfg.Rounds, 0),
		sound:      sound,
		now:        timerNow,
		after:      timerAfter,
		play:       timerSound,
	}
	if w.rounds == 0 {
		w.rounds = config.DefaultPomodoroRounds
	}
	if _, err := w.writeIcon(w.render()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write %s icon: %w", cfg.Type, err)
	}
	w.every(timerTick, func(time.Time) { w.refresh() })
	return w, nil
}

// timerPhases builds the countdowns: one for a timer, and for a pomodoro
// rounds × (work, break) with the last break a long one.
func timerPhases(cfg *config.Widget) ([]timerPhase, error) {
	switch cfg.Type {
	case "timer":
		d, err := config.TimerDuration(cfg.Duration, config.DefaultTimerDuration)
		if err != nil {
			return nil, fmt.Errorf("timer duration: %w", err)
		}
		return []timerPhase{{dur: d, clr: styleAccent}}, nil
	case "pomodoro":
		work, err1 := config.TimerDuration(cfg.Work, config.DefaultPomodoroWork)
		brk, err2 := config.TimerDuration(cfg.Break, config.DefaultPomodoroBreak)
		long, err3 := config.TimerDuration(cfg.LongBreak, config.DefaultPomodoroLong)
		for _, err := range []error{err1, err2, err3} {
			if err != nil {
				return nil, fmt.Errorf("pomodoro: %w", err)
			}
		}
		rounds := cfg.Rounds
		if rounds <= 0 {
			rounds = config.DefaultPomodoroRounds
		}
		var out []timerPhase
		for r := 1; r <= rounds; r++ {
			out = append(out, timerPhase{name: "work", dur: work, clr: styleAlert, round: r})
			if r < rounds {
				out = append(out, timerPhase{name: "break", dur: brk, clr: styleLive, round: r})
			} else {
				out = append(out, timerPhase{name: "long break", dur: long, clr: styleLive, round: r})
			}
		}
		return out, nil
	}
	return nil, nil
}

func timerGestures() ig.Options {
	return ig.Options{HasTap: true, HasHold: true, HoldDelay: timerHoldDelay}
}

func (w *TimerWidget) resume() { w.refresh() }

func (w *TimerWidget) HandleGesture(kind ig.Kind) {
	if w.isClosed() {
		return
	}
	w.mu.Lock()
	switch kind {
	case ig.Tap:
		switch w.state {
		case timerIdle, timerPaused:
			w.startLocked()
		case timerRunning:
			w.pauseLocked()
		case timerDone:
			w.resetLocked()
		}
	case ig.Hold:
		w.resetLocked()
		w.phase = 0
	}
	w.mu.Unlock()
	w.refresh()
}

func (w *TimerWidget) Close() error {
	w.mu.Lock()
	w.cancelAlarmLocked()
	w.mu.Unlock()
	return w.widgetBase.Close()
}

func (w *TimerWidget) startLocked() {
	w.state = timerRunning
	w.since = w.now()
	if len(w.phases) == 0 {
		return
	}
	// The alarm fires on its own timer, not on render ticks, which stop
	// while the widget's page is hidden.
	left := w.phases[w.phase].dur - w.acc
	w.stopAlarm = w.after(left, w.phaseEnded)
}

func (w *TimerWidget) pauseLocked() {
	w.acc += w.now().Sub(w.since)
	w.state = timerPaused
	w.cancelAlarmLocked()
}

func (w *TimerWidget) resetLocked() {
	w.cancelAlarmLocked()
	w.state, w.acc = timerIdle, 0
}

func (w *TimerWidget) cancelAlarmLocked() {
	if w.stopAlarm != nil {
		w.stopAlarm()
		w.stopAlarm = nil
	}
}

// phaseEnded runs when a countdown reaches zero.
func (w *TimerWidget) phaseEnded() {
	w.mu.Lock()
	if w.state != timerRunning || w.isClosed() {
		w.mu.Unlock()
		return
	}
	w.stopAlarm = nil
	if w.kind == "pomodoro" {
		// The next phase waits for a tap: a break starts when you leave
		// the desk, not when the timer says so.
		w.phase = (w.phase + 1) % len(w.phases)
		w.state, w.acc = timerIdle, 0
	} else {
		w.state, w.acc = timerDone, w.phases[0].dur
	}
	w.mu.Unlock()
	if w.sound != "none" {
		go w.play(w.sound)
	}
	w.refresh()
}

// elapsedLocked is the run time of the current phase.
func (w *TimerWidget) elapsedLocked(now time.Time) time.Duration {
	if w.state == timerRunning {
		return w.acc + now.Sub(w.since)
	}
	return w.acc
}

// refresh redraws when what the key shows changed.
func (w *TimerWidget) refresh() {
	img := w.draw(false)
	if img == nil {
		return
	}
	if _, err := w.publish(img); err != nil {
		debug.Logf("widget %s: %v", w.id, err)
	}
}

func (w *TimerWidget) render() *image.RGBA { return w.draw(true) }

// draw renders the key, or returns nil when nothing it shows changed
// and force is false.
func (w *TimerWidget) draw(force bool) *image.RGBA {
	w.mu.Lock()
	v := w.viewLocked(w.now())
	key := v.key()
	if key == w.lastKey && !force {
		w.mu.Unlock()
		return nil
	}
	w.lastKey = key
	w.mu.Unlock()
	return renderTimer(v)
}

// timerView is everything the key shows.
type timerView struct {
	kind  string
	state timerState
	text  string  // the time
	frac  float64 // ring fill
	phase timerPhase
	of    int // pomodoro rounds
}

func (v timerView) key() string {
	return fmt.Sprintf("%d|%s|%.3f|%s|%d", v.state, v.text, v.frac, v.phase.name, v.phase.round)
}

// viewLocked derives the ring from the shown whole seconds, not the
// exact time, so a running key changes, and is re-sent to the deck, once
// a second at most.
func (w *TimerWidget) viewLocked(now time.Time) timerView {
	elapsed := w.elapsedLocked(now)
	v := timerView{kind: w.kind, state: w.state, of: w.rounds}
	if len(w.phases) == 0 {
		secs := int(elapsed / time.Second)
		v.text = formatTimer(elapsed, false)
		// The ring sweeps once a minute, like a stopwatch's second hand.
		v.frac = float64(secs%60) / 60
		return v
	}
	v.phase = w.phases[w.phase]
	left := max(v.phase.dur-elapsed, 0)
	v.text = formatTimer(left, true)
	v.frac = float64(ceilSeconds(left)) / float64(ceilSeconds(v.phase.dur))
	if w.state == timerDone {
		v.frac = 1
	}
	return v
}

func ceilSeconds(d time.Duration) int {
	return int((d + time.Second - 1) / time.Second)
}

// formatTimer writes m:ss, or h:mm:ss from an hour. A countdown rounds
// up, so it shows 5:00 when started and 0:01 in its last second.
func formatTimer(d time.Duration, roundUp bool) string {
	secs := int(d / time.Second)
	if roundUp {
		secs = ceilSeconds(d)
	}
	if secs >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", secs/3600, secs/60%60, secs%60)
	}
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

// renderTimer draws the gauge card: the ring and time, and under the
// time the state (play/pause icon) or, for a pomodoro, the phase.
func renderTimer(v timerView) *image.RGBA {
	img := newCard()
	ringClr := color.RGBA(styleAccent)
	if v.kind == "pomodoro" {
		ringClr = v.phase.clr
	}
	stateIcon, stateClr := "lucide/timer", color.RGBA(styleDim)
	switch v.state {
	case timerRunning:
		stateIcon, stateClr = "lucide/play", ringClr
	case timerPaused:
		stateIcon, stateClr, ringClr = "lucide/pause", styleWarn, styleWarn
	case timerDone:
		stateIcon, stateClr, ringClr = "lucide/bell-ring", styleAlert, styleAlert
	}
	if v.kind == "pomodoro" && v.state != timerPaused {
		// Idle or running, the ring keeps the phase color.
		ringClr = v.phase.clr
	}
	frac := v.frac
	if v.state == timerIdle && v.kind != "stopwatch" {
		// Full but dim: set and waiting for a tap.
		frac, ringClr = 1, dimColor(ringClr)
	}
	if v.kind != "pomodoro" {
		drawGaugeCard(img, frac, ringClr, v.text, styleText, stateIcon, stateClr)
		return img
	}
	label := "break"
	switch v.phase.name {
	case "work":
		label = fmt.Sprintf("%d/%d", v.phase.round, v.of)
	case "long break":
		label = "long"
	}
	labelClr := v.phase.clr
	if v.state != timerRunning {
		labelClr = stateClr
	}
	drawGaugeCardText(img, frac, ringClr, v.text, styleText, label, labelClr)
	return img
}

// dimColor darkens c to about a third, for idle rings.
func dimColor(c color.RGBA) color.RGBA {
	return color.RGBA{c.R / 3, c.G / 3, c.B / 3, c.A}
}

// playSystemSound plays a sound from /System/Library/Sounds. Names are
// validated to letters when the config loads.
func playSystemSound(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := "/System/Library/Sounds/" + name + ".aiff"
	if err := execCommandContext(ctx, "/usr/bin/afplay", path).Run(); err != nil {
		debug.Logf("timer sound %s: %v", name, err)
	}
}
