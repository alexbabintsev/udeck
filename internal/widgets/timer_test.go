package widgets

import (
	"sync"
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
	ig "github.com/alexbabintsev/udeck/internal/gestures"
)

// fakeTimerEnv drives timer widgets by hand: a settable clock, alarms
// that fire only when the test says so, and recorded sounds.
type fakeTimerEnv struct {
	mu      sync.Mutex
	now     time.Time
	alarm   func()
	alarmIn time.Duration
	stopped int
	sounds  []string
}

func useFakeTimerEnv(t *testing.T) *fakeTimerEnv {
	t.Helper()
	f := &fakeTimerEnv{now: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)}
	oldNow, oldAfter, oldSound := timerNow, timerAfter, timerSound
	timerNow = func() time.Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.now
	}
	timerAfter = func(d time.Duration, fn func()) func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.alarm, f.alarmIn = fn, d
		return func() {
			f.mu.Lock()
			f.alarm = nil
			f.stopped++
			f.mu.Unlock()
		}
	}
	timerSound = func(name string) {
		f.mu.Lock()
		f.sounds = append(f.sounds, name)
		f.mu.Unlock()
	}
	t.Cleanup(func() { timerNow, timerAfter, timerSound = oldNow, oldAfter, oldSound })
	return f
}

func (f *fakeTimerEnv) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

// fire runs the pending alarm, as the runtime timer would.
func (f *fakeTimerEnv) fire(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	fn := f.alarm
	f.alarm = nil
	f.mu.Unlock()
	if fn == nil {
		t.Fatal("no alarm pending")
	}
	fn()
}

func (f *fakeTimerEnv) pending() (bool, time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alarm != nil, f.alarmIn
}

func (f *fakeTimerEnv) soundCount(t *testing.T) int {
	t.Helper()
	// Sounds play on their own goroutine.
	deadline := time.Now().Add(time.Second)
	for {
		f.mu.Lock()
		n := len(f.sounds)
		f.mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			return n
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func newTestTimer(t *testing.T, cfg *config.Widget) (*TimerWidget, *recorder) {
	t.Helper()
	rec := &recorder{}
	w, err := NewTimerWidget("t", cfg, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, rec
}

func (w *TimerWidget) testView() timerView {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.viewLocked(w.now())
}

func TestTimerCountsDownPausesAndRings(t *testing.T) {
	env := useFakeTimerEnv(t)
	w, _ := newTestTimer(t, &config.Widget{Type: "timer", Duration: "2m"})
	if v := w.testView(); v.text != "2:00" || v.state != timerIdle {
		t.Fatalf("idle view %+v", v)
	}
	w.HandleGesture(ig.Tap)
	if ok, in := env.pending(); !ok || in != 2*time.Minute {
		t.Fatalf("alarm pending=%v in %v", ok, in)
	}
	env.advance(30*time.Second + 500*time.Millisecond)
	if v := w.testView(); v.text != "1:30" || v.state != timerRunning {
		t.Fatalf("running view %+v (a countdown rounds up)", v)
	}
	w.HandleGesture(ig.Tap) // pause
	env.advance(time.Hour)
	if ok, _ := env.pending(); ok {
		t.Fatal("pausing must cancel the alarm")
	}
	if v := w.testView(); v.text != "1:30" || v.state != timerPaused {
		t.Fatalf("paused view %+v", v)
	}
	w.HandleGesture(ig.Tap) // resume
	if _, in := env.pending(); in != 89*time.Second+500*time.Millisecond {
		t.Fatalf("resumed alarm in %v", in)
	}
	env.advance(alarmDelay(env))
	env.fire(t)
	if v := w.testView(); v.state != timerDone || v.text != "0:00" || v.frac != 1 {
		t.Fatalf("done view %+v", v)
	}
	if env.soundCount(t) != 1 || env.sounds[0] != config.DefaultTimerSound {
		t.Fatalf("sounds %v", env.sounds)
	}
	w.HandleGesture(ig.Tap) // a tap after the alarm resets
	if v := w.testView(); v.state != timerIdle || v.text != "2:00" {
		t.Fatalf("reset view %+v", v)
	}
}

func alarmDelay(env *fakeTimerEnv) time.Duration {
	_, d := env.pending()
	return d
}

func TestTimerHoldResetsAndSilentSound(t *testing.T) {
	env := useFakeTimerEnv(t)
	w, rec := newTestTimer(t, &config.Widget{Type: "timer", Duration: "10s", Sound: "none"})
	w.HandleGesture(ig.Tap)
	env.advance(4 * time.Second)
	before := rec.count()
	w.refresh()
	if rec.count() != before+1 {
		t.Fatal("a running timer redraws when the seconds change")
	}
	w.refresh()
	if rec.count() != before+1 {
		t.Fatal("no redraw while nothing changed")
	}
	w.HandleGesture(ig.Hold)
	if ok, _ := env.pending(); ok {
		t.Fatal("reset must cancel the alarm")
	}
	if v := w.testView(); v.state != timerIdle || v.text != "0:10" {
		t.Fatalf("view after hold %+v", v)
	}
	w.HandleGesture(ig.Tap)
	env.advance(10 * time.Second)
	env.fire(t)
	time.Sleep(10 * time.Millisecond)
	env.mu.Lock()
	n := len(env.sounds)
	env.mu.Unlock()
	if n != 0 {
		t.Fatal("sound: none must stay silent")
	}
}

func TestTimerStaleAlarmIsIgnored(t *testing.T) {
	env := useFakeTimerEnv(t)
	w, _ := newTestTimer(t, &config.Widget{Type: "timer", Duration: "10s"})
	w.HandleGesture(ig.Tap)
	env.mu.Lock()
	stale := env.alarm
	env.mu.Unlock()
	w.HandleGesture(ig.Hold)
	stale() // the runtime timer fired just as it was stopped
	if v := w.testView(); v.state != timerIdle {
		t.Fatalf("a stale alarm must not end an idle timer: %+v", v)
	}
}

func TestStopwatch(t *testing.T) {
	env := useFakeTimerEnv(t)
	w, _ := newTestTimer(t, &config.Widget{Type: "stopwatch"})
	if v := w.testView(); v.text != "0:00" || v.frac != 0 {
		t.Fatalf("idle %+v", v)
	}
	w.HandleGesture(ig.Tap)
	if ok, _ := env.pending(); ok {
		t.Fatal("a stopwatch has no alarm")
	}
	env.advance(time.Hour + 2*time.Minute + 15*time.Second + 900*time.Millisecond)
	v := w.testView()
	if v.text != "1:02:15" || v.frac < 0.25 || v.frac > 0.27 {
		t.Fatalf("running %+v (rounds down; ring = seconds)", v)
	}
	w.HandleGesture(ig.Tap)
	env.advance(time.Minute)
	if v := w.testView(); v.text != "1:02:15" || v.state != timerPaused {
		t.Fatalf("paused %+v", v)
	}
	w.HandleGesture(ig.Hold)
	if v := w.testView(); v.text != "0:00" || v.state != timerIdle {
		t.Fatalf("reset %+v", v)
	}
}

func TestPomodoroCyclesPhases(t *testing.T) {
	env := useFakeTimerEnv(t)
	w, _ := newTestTimer(t, &config.Widget{Type: "pomodoro", Work: "20m", Break: "3m", LongBreak: "10m", Rounds: 2, Sound: "Ping"})
	want := []struct {
		name  string
		round int
		text  string
	}{
		{"work", 1, "20:00"}, {"break", 1, "3:00"}, {"work", 2, "20:00"}, {"long break", 2, "10:00"}, {"work", 1, "20:00"},
	}
	for i, ph := range want {
		v := w.testView()
		if v.phase.name != ph.name || v.phase.round != ph.round || v.text != ph.text || v.state != timerIdle {
			t.Fatalf("step %d: view %+v, want %+v", i, v, ph)
		}
		if i == len(want)-1 {
			break
		}
		w.HandleGesture(ig.Tap)
		env.advance(alarmDelay(env))
		env.fire(t)
	}
	if env.soundCount(t) == 0 || env.sounds[0] != "Ping" {
		t.Fatalf("sounds %v", env.sounds)
	}
	// Hold goes back to the first work phase.
	w.HandleGesture(ig.Tap)
	env.advance(alarmDelay(env))
	env.fire(t)
	w.HandleGesture(ig.Hold)
	if v := w.testView(); v.phase.name != "work" || v.phase.round != 1 {
		t.Fatalf("hold must restart the cycle: %+v", v)
	}
}

func TestTimerPhasesDefaultsAndErrors(t *testing.T) {
	ph, err := timerPhases(&config.Widget{Type: "pomodoro"})
	if err != nil || len(ph) != 2*config.DefaultPomodoroRounds || ph[0].dur != config.DefaultPomodoroWork || ph[len(ph)-1].dur != config.DefaultPomodoroLong {
		t.Fatalf("defaults %+v %v", ph, err)
	}
	if ph, _ := timerPhases(&config.Widget{Type: "timer"}); ph[0].dur != config.DefaultTimerDuration {
		t.Fatal("timer default")
	}
	if _, err := NewTimerWidget("x", &config.Widget{Type: "timer", Duration: "soon"}, nil); err == nil {
		t.Fatal("expected a duration error")
	}
	if _, err := timerPhases(&config.Widget{Type: "pomodoro", Break: "0s"}); err == nil {
		t.Fatal("expected a break error")
	}
}

func TestFormatTimer(t *testing.T) {
	cases := []struct {
		d    time.Duration
		up   bool
		want string
	}{
		{0, true, "0:00"},
		{100 * time.Millisecond, true, "0:01"},
		{100 * time.Millisecond, false, "0:00"},
		{5 * time.Minute, true, "5:00"},
		{59*time.Minute + 59*time.Second + time.Millisecond, true, "1:00:00"},
		{25*time.Hour + 61*time.Second, false, "25:01:01"},
	}
	for _, c := range cases {
		if got := formatTimer(c.d, c.up); got != c.want {
			t.Fatalf("formatTimer(%v, %v) = %q, want %q", c.d, c.up, got, c.want)
		}
	}
}

func TestRenderTimerStates(t *testing.T) {
	tp := timerPhase{dur: time.Minute, clr: styleAccent}
	running := renderTimer(timerView{kind: "timer", state: timerRunning, text: "0:30", frac: 0.5, phase: tp})
	paused := renderTimer(timerView{kind: "timer", state: timerPaused, text: "0:30", frac: 0.5, phase: tp})
	done := renderTimer(timerView{kind: "timer", state: timerDone, text: "0:00", frac: 1, phase: tp})
	idle := renderTimer(timerView{kind: "timer", state: timerIdle, text: "1:00", phase: tp})
	if !hasColor(running, styleAccent) || !hasColor(paused, styleWarn) || !hasColor(done, styleAlert) {
		t.Fatal("state colors")
	}
	if hasColor(idle, styleAccent) || !hasColor(idle, dimColor(styleAccent)) {
		t.Fatal("an idle timer shows a dim ring")
	}
	work := timerPhase{name: "work", dur: time.Minute, clr: styleAlert, round: 1}
	if !hasColor(renderTimer(timerView{kind: "pomodoro", state: timerRunning, text: "0:30", frac: 0.5, phase: work, of: 4}), styleAlert) {
		t.Fatal("pomodoro work is red")
	}
	if o := timerGestures(); !o.HasTap || !o.HasHold || o.HasRepeat {
		t.Fatalf("gestures %+v", o)
	}
}

func TestPlaySystemSoundRunsAfplay(t *testing.T) {
	calls := fakeExec(t, map[string]string{"/usr/bin/afplay": ""})
	playSystemSound("Glass")
	if len(*calls) != 1 || (*calls)[0] != "/usr/bin/afplay" {
		t.Fatalf("calls %v", *calls)
	}
}

// A running key changes once a second, when its digits do: every change
// is an image sent to the deck.
func TestTimerRedrawsOncePerSecond(t *testing.T) {
	for _, cfg := range []*config.Widget{
		{Type: "timer", Duration: "1m"}, {Type: "timer", Duration: "1h"},
		{Type: "stopwatch"}, {Type: "pomodoro"},
	} {
		env := useFakeTimerEnv(t)
		w, rec := newTestTimer(t, cfg)
		w.setActive(false) // the real ticker stops; ticks are driven here
		w.HandleGesture(ig.Tap)
		before := rec.count()
		for range 50 { // 10 s of ticks
			env.advance(timerTick)
			w.refresh()
		}
		if n := rec.count() - before; n != 10 {
			t.Fatalf("%s %s: %d redraws in 10 s, want 10", cfg.Type, cfg.Duration, n)
		}
	}
}
