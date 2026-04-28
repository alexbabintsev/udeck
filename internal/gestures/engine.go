package gestures

import "time"

type Kind string

const (
	None      Kind = ""
	Tap       Kind = "tap"
	DoubleTap Kind = "double_tap"
	LongPress Kind = "long_press"
	Repeat    Kind = "repeat"
)

type Options struct {
	HasTap       bool
	HasDoubleTap bool
	HasLongPress bool
	HasRepeat    bool

	LongPressDelay time.Duration
	DoubleTapDelay time.Duration
	RepeatEvery    time.Duration
}

type Engine struct {
	states map[string]*state
}

type state struct {
	pressed     bool
	secondTap   bool
	pressAt     time.Time
	longFired   bool
	nextRepeat  time.Time
	tapPending  bool
	tapDeadline time.Time
}

type Binding struct {
	ID      string
	Options Options
}

func New() *Engine {
	return &Engine{states: map[string]*state{}}
}

func (e *Engine) Reset() {
	e.states = map[string]*state{}
}

func (e *Engine) HandleEvent(id string, opts Options, pressed bool, now time.Time) Kind {
	st := e.state(id)

	if pressed {
		if opts.HasDoubleTap && st.tapPending && now.Before(st.tapDeadline) {
			st.tapPending = false
			st.secondTap = true
		} else {
			st.secondTap = false
		}
		st.pressed = true
		st.pressAt = now
		st.longFired = false
		if opts.HasRepeat {
			st.nextRepeat = now.Add(initialRepeatDelay(opts))
		} else {
			st.nextRepeat = time.Time{}
		}
		return None
	}

	if !st.pressed {
		return None
	}
	st.pressed = false
	held := now.Sub(st.pressAt)
	if opts.HasLongPress && !st.longFired && held >= opts.LongPressDelay {
		st.longFired = true
		st.secondTap = false
		st.tapPending = false
		return LongPress
	}
	if st.longFired {
		st.secondTap = false
		st.tapPending = false
		return None
	}
	if st.secondTap {
		st.secondTap = false
		return DoubleTap
	}
	if opts.HasDoubleTap {
		st.tapPending = true
		st.tapDeadline = now.Add(opts.DoubleTapDelay)
		return None
	}
	if opts.HasTap {
		return Tap
	}
	return None
}

func (e *Engine) Tick(bindings []Binding, now time.Time) (string, Kind) {
	for _, binding := range bindings {
		st := e.states[binding.ID]
		if st == nil {
			continue
		}
		opts := binding.Options
		if st.pressed {
			if opts.HasLongPress && !st.longFired && now.Sub(st.pressAt) >= opts.LongPressDelay {
				st.longFired = true
				st.secondTap = false
				st.tapPending = false
				if opts.HasRepeat {
					st.nextRepeat = now.Add(opts.RepeatEvery)
				}
				return binding.ID, LongPress
			}
			if opts.HasRepeat && !st.nextRepeat.IsZero() && !now.Before(st.nextRepeat) {
				st.nextRepeat = now.Add(opts.RepeatEvery)
				return binding.ID, Repeat
			}
		}
		if st.tapPending && !now.Before(st.tapDeadline) {
			st.tapPending = false
			return binding.ID, Tap
		}
	}
	return "", None
}

func (e *Engine) state(id string) *state {
	if st, ok := e.states[id]; ok {
		return st
	}
	st := &state{}
	e.states[id] = st
	return st
}

func initialRepeatDelay(opts Options) time.Duration {
	if opts.HasLongPress {
		return opts.LongPressDelay
	}
	return opts.RepeatEvery
}
