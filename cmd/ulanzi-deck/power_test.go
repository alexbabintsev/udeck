package main

import (
	"context"
	"syscall"
	"testing"
	"time"
)

func TestNilPowerWatcher(t *testing.T) {
	var w *powerWatcher
	if w.Events() != nil {
		t.Fatal("nil watcher must have no events")
	}
	w.Ack() // must not panic
	(&powerWatcher{}).Ack()
}

func TestNotifySignalsRecordsCause(t *testing.T) {
	ctx, stop := notifySignals(syscall.SIGHUP)
	defer stop()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context not cancelled by signal")
	}
	if !terminatedBySystem(ctx) {
		t.Fatalf("SIGHUP must count as system termination, cause=%v", context.Cause(ctx))
	}

	ctx2, stop2 := notifySignals(syscall.SIGINT)
	stop2()
	if terminatedBySystem(ctx2) {
		t.Fatal("a plain cancel is not a system termination")
	}
	ctx3, cancel3 := context.WithCancelCause(context.Background())
	cancel3(signalCause{syscall.SIGINT})
	if terminatedBySystem(ctx3) {
		t.Fatal("Ctrl-C must not count as system termination")
	}
	if signalCause(signalCause{syscall.SIGTERM}).Error() == "" {
		t.Fatal("empty cause message")
	}
}
