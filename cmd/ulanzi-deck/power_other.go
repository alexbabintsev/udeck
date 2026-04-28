//go:build !darwin

package main

import "context"

type powerEvent string

const (
	powerEventSleep    powerEvent = "sleep"
	powerEventWake     powerEvent = "wake"
	powerEventShutdown powerEvent = "shutdown"
)

type powerWatcher struct{}

func newPowerWatcher(context.Context) (*powerWatcher, error) { return nil, nil }
func (w *powerWatcher) Events() <-chan powerEvent            { return nil }
