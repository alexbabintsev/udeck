package main

import (
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
)

const (
	defaultHoldMS        = 500
	defaultDoubleTapMS   = 300
	defaultRepeatEveryMS = 200
)

type gestureManager struct {
	engine *ig.Engine
}

type gestureEvent struct {
	ButtonID string
	Kind     ig.Kind
	Button   *config.Button
}

func newGestureManager() *gestureManager {
	return &gestureManager{engine: ig.New()}
}

func (m *gestureManager) Reset() {
	m.engine.Reset()
}

func (m *gestureManager) HandleEvent(btn *config.Button, pressed bool, now time.Time) *gestureEvent {
	if btn == nil || btn.On == nil {
		return nil
	}
	kind := m.engine.HandleEvent(btn.RuntimeID, handlerOptions(btn.On), pressed, now)
	if kind == ig.None {
		return nil
	}
	return &gestureEvent{ButtonID: btn.RuntimeID, Kind: kind, Button: btn}
}

func (m *gestureManager) Tick(buttons []config.Button, now time.Time) *gestureEvent {
	var bindings []ig.Binding
	for i := range buttons {
		btn := &buttons[i]
		if btn.On == nil {
			continue
		}
		bindings = append(bindings, ig.Binding{
			ID:      btn.RuntimeID,
			Options: handlerOptions(btn.On),
		})
	}
	id, kind := m.engine.Tick(bindings, now)
	if kind == ig.None {
		return nil
	}
	for i := range buttons {
		btn := &buttons[i]
		if btn.RuntimeID == id && btn.On != nil {
			return &gestureEvent{ButtonID: id, Kind: kind, Button: btn}
		}
	}
	return nil
}

func handlerOptions(tr *config.Handlers) ig.Options {
	return ig.Options{
		HasTap:         tr.Tap != nil,
		HasDoubleTap:   tr.DoubleTap != nil,
		HasHold:        tr.Hold != nil,
		HasRepeat:      tr.Repeat != nil,
		HoldDelay:      holdDuration(tr),
		DoubleTapDelay: doubleTapDuration(tr),
		RepeatEvery:    repeatEveryDuration(tr),
	}
}

func actionForKind(tr *config.Handlers, kind ig.Kind) *config.Action {
	switch kind {
	case ig.Tap:
		return actionForBinding(tr.Tap)
	case ig.DoubleTap:
		return actionForBinding(tr.DoubleTap)
	case ig.Hold:
		return actionForBinding(tr.Hold)
	case ig.Repeat:
		return actionForBinding(tr.Repeat)
	default:
		return nil
	}
}

func actionForBinding(binding *config.GestureBinding) *config.Action {
	if binding == nil {
		return nil
	}
	return binding.Action()
}

func holdDuration(tr *config.Handlers) time.Duration {
	ms := tr.HoldMS
	if ms <= 0 {
		ms = defaultHoldMS
	}
	return time.Duration(ms) * time.Millisecond
}

func doubleTapDuration(tr *config.Handlers) time.Duration {
	ms := tr.DoubleTapMS
	if ms <= 0 {
		ms = defaultDoubleTapMS
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
