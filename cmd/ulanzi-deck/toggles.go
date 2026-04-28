package main

import (
	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
)

type toggleManager struct {
	visuals map[string]toggleVisual
	steps   map[toggleKey][]config.ToggleStep
	index   map[toggleKey]int
}

type toggleVisual struct {
	gesture ig.Kind
}

type toggleKey struct {
	buttonID string
	gesture  ig.Kind
}

func newToggleManager(cfg *config.Config) *toggleManager {
	m := &toggleManager{
		visuals: map[string]toggleVisual{},
		steps:   map[toggleKey][]config.ToggleStep{},
		index:   map[toggleKey]int{},
	}
	m.collect(cfg.Buttons)
	return m
}

func (m *toggleManager) collect(buttons []config.Button) {
	for _, btn := range buttons {
		if btn.On != nil {
			m.addBinding(btn.RuntimeID, ig.Tap, btn.On.Tap)
			m.addBinding(btn.RuntimeID, ig.DoubleTap, btn.On.DoubleTap)
			m.addBinding(btn.RuntimeID, ig.Hold, btn.On.Hold)
			m.addBinding(btn.RuntimeID, ig.Repeat, btn.On.Repeat)
		}
		if btn.Folder != nil {
			m.collect(btn.Folder.Buttons)
		}
	}
}

func (m *toggleManager) addBinding(buttonID string, kind ig.Kind, binding *config.GestureBinding) {
	if binding == nil || len(binding.Toggle) == 0 {
		return
	}
	key := toggleKey{buttonID: buttonID, gesture: kind}
	m.steps[key] = append([]config.ToggleStep(nil), binding.Toggle...)
	if _, ok := m.visuals[buttonID]; !ok {
		m.visuals[buttonID] = toggleVisual{gesture: kind}
	}
}

func (m *toggleManager) ResolveButtons(buttons []config.Button) []config.Button {
	if m == nil {
		return buttons
	}
	out := make([]config.Button, len(buttons))
	copy(out, buttons)
	for i := range out {
		visual, ok := m.visuals[out[i].RuntimeID]
		if !ok {
			continue
		}
		step, ok := m.currentStep(out[i].RuntimeID, visual.gesture)
		if !ok {
			continue
		}
		if step.Label != "" {
			out[i].Label = step.Label
		}
		if step.Icon != "" {
			out[i].Icon = step.Icon
		}
	}
	return out
}

func (m *toggleManager) Handle(buttonID string, kind ig.Kind) (*config.GestureBinding, bool) {
	if m == nil {
		return nil, false
	}
	step, ok := m.currentStep(buttonID, kind)
	if !ok {
		return nil, false
	}
	key := toggleKey{buttonID: buttonID, gesture: kind}
	next := m.index[key] + 1
	if next >= len(m.steps[key]) {
		next = 0
	}
	m.index[key] = next
	visual, changesVisual := m.visuals[buttonID]
	return &config.GestureBinding{
		Type: step.Action.Type,
		Cmd:  step.Action.Cmd,
		Key:  step.Action.Key,
		URL:  step.Action.URL,
	}, changesVisual && visual.gesture == kind
}

func (m *toggleManager) currentStep(buttonID string, kind ig.Kind) (config.ToggleStep, bool) {
	key := toggleKey{buttonID: buttonID, gesture: kind}
	steps := m.steps[key]
	if len(steps) == 0 {
		return config.ToggleStep{}, false
	}
	idx := m.index[key]
	if idx < 0 || idx >= len(steps) {
		idx = 0
		m.index[key] = 0
	}
	return steps[idx], true
}
