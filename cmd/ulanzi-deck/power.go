package main

import (
	"bufio"
	"io"
	"strings"
)

type powerEvent string

const (
	powerEventSleep    powerEvent = "sleep"
	powerEventWake     powerEvent = "wake"
	powerEventShutdown powerEvent = "shutdown"
)

type powerWatcher struct {
	events <-chan powerEvent
}

func (w *powerWatcher) Events() <-chan powerEvent {
	if w == nil {
		return nil
	}
	return w.events
}

// scanPowerEvents turns one event name per line into powerEvents. The
// channel is closed when r is exhausted; unknown lines are ignored and
// events are dropped rather than blocking when the consumer lags.
func scanPowerEvents(r io.Reader, done func()) <-chan powerEvent {
	ch := make(chan powerEvent, 8)
	go func() {
		defer close(ch)
		if done != nil {
			defer done()
		}
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			switch evt := powerEvent(strings.TrimSpace(scanner.Text())); evt {
			case powerEventSleep, powerEventWake, powerEventShutdown:
				select {
				case ch <- evt:
				default:
				}
			}
		}
	}()
	return ch
}
