package main

import (
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

const (
	defaultLongPressMS   = 500
	defaultDoublePressMS = 300
	defaultRepeatEveryMS = 200
)

type gestureManager struct {
	states map[string]*gestureState
}

type gestureState struct {
	pressed     bool
	pressAt     time.Time
	longFired   bool
	nextRepeat  time.Time
	tapPending  bool
	tapDeadline time.Time
}

func newGestureManager() *gestureManager {
	return &gestureManager{states: map[string]*gestureState{}}
}

func (m *gestureManager) Reset() {
	m.states = map[string]*gestureState{}
}

func (m *gestureManager) HandleEvent(btn *config.Button, pressed bool, now time.Time) *config.Action {
	if btn == nil || btn.On == nil {
		return nil
	}
	st := m.state(btn.RuntimeID)
	tr := btn.On

	if pressed {
		if tr.DoublePress != nil && st.tapPending && now.Before(st.tapDeadline) {
			st.tapPending = false
			st.pressed = true
			st.pressAt = now
			st.longFired = false
			return tr.DoublePress
		}
		st.pressed = true
		st.pressAt = now
		st.longFired = false
		if tr.Repeat != nil {
			st.nextRepeat = now.Add(initialRepeatDelay(tr))
		}
		return nil
	}

	if !st.pressed {
		return nil
	}
	st.pressed = false
	held := now.Sub(st.pressAt)
	if tr.LongPress != nil && !st.longFired && held >= longPressDuration(tr) {
		st.longFired = true
		return tr.LongPress
	}
	if st.longFired {
		st.tapPending = false
		return nil
	}
	if tr.DoublePress != nil {
		st.tapPending = true
		st.tapDeadline = now.Add(doublePressDuration(tr))
		return nil
	}
	return tr.Tap
}

func (m *gestureManager) Tick(buttons []config.Button, now time.Time) *config.Action {
	for i := range buttons {
		btn := &buttons[i]
		if btn.On == nil {
			continue
		}
		st := m.states[btn.RuntimeID]
		if st == nil {
			continue
		}
		tr := btn.On
		if st.pressed {
			if tr.LongPress != nil && !st.longFired && now.Sub(st.pressAt) >= longPressDuration(tr) {
				st.longFired = true
				st.tapPending = false
				if tr.Repeat != nil {
					st.nextRepeat = now.Add(repeatEveryDuration(tr))
				}
				return tr.LongPress
			}
			if tr.Repeat != nil && !st.nextRepeat.IsZero() && !now.Before(st.nextRepeat) {
				st.nextRepeat = now.Add(repeatEveryDuration(tr))
				return tr.Repeat
			}
		}
		if st.tapPending && !now.Before(st.tapDeadline) {
			st.tapPending = false
			return tr.Tap
		}
	}
	return nil
}

func (m *gestureManager) state(id string) *gestureState {
	if st, ok := m.states[id]; ok {
		return st
	}
	st := &gestureState{}
	m.states[id] = st
	return st
}

func longPressDuration(tr *config.Handlers) time.Duration {
	ms := tr.LongPressMS
	if ms <= 0 {
		ms = defaultLongPressMS
	}
	return time.Duration(ms) * time.Millisecond
}

func doublePressDuration(tr *config.Handlers) time.Duration {
	ms := tr.DoublePressMS
	if ms <= 0 {
		ms = defaultDoublePressMS
	}
	return time.Duration(ms) * time.Millisecond
}

func repeatEveryDuration(tr *config.Handlers) time.Duration {
	ms := tr.RepeatEveryMS
	if ms <= 0 {
		ms = defaultRepeatEveryMS
	}
	return time.Duration(ms) * time.Millisecond
}

func initialRepeatDelay(tr *config.Handlers) time.Duration {
	if tr.LongPress != nil {
		return longPressDuration(tr)
	}
	return repeatEveryDuration(tr)
}
