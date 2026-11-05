package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

type powerEvent string

const (
	powerEventSleep    powerEvent = "sleep"
	powerEventWake     powerEvent = "wake"
	powerEventShutdown powerEvent = "shutdown"
)

// powerWatcher delivers system sleep and wake events. After a sleep
// event the consumer calls Ack once the deck is dimmed; the system waits
// for it (up to a timeout) before going to sleep.
type powerWatcher struct {
	events <-chan powerEvent
	ack    func()
}

func (w *powerWatcher) Events() <-chan powerEvent {
	if w == nil {
		return nil
	}
	return w.events
}

// Ack lets a pending system sleep proceed.
func (w *powerWatcher) Ack() {
	if w != nil && w.ack != nil {
		w.ack()
	}
}

// signalCause records which signal cancelled the run context.
type signalCause struct{ sig os.Signal }

func (c signalCause) Error() string { return "received " + c.sig.String() }

// notifySignals returns a context cancelled by the first of sigs, with
// the signal as its cause.
func notifySignals(sigs ...os.Signal) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sigs...)
	go func() {
		select {
		case sig := <-ch:
			cancel(signalCause{sig})
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(ch)
		cancel(nil)
	}
}

// terminatedBySystem reports whether ctx was cancelled by SIGTERM or
// SIGHUP: shutdown, logout, closing the terminal or stopping the
// service. Ctrl-C (SIGINT) is not: it leaves the deck as it is.
func terminatedBySystem(ctx context.Context) bool {
	cause, ok := context.Cause(ctx).(signalCause)
	return ok && (cause.sig == syscall.SIGTERM || cause.sig == syscall.SIGHUP)
}
