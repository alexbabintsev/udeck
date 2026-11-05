//go:build darwin

package main

import (
	"context"
	"testing"
	"time"
)

func TestPowerSleepWaitsForAck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := subscribePower(ctx)

	done := make(chan struct{})
	go func() {
		deliverPowerEvent(powerEventSleep, 2*time.Second)
		close(done)
	}()
	if evt := <-w.Events(); evt != powerEventSleep {
		t.Fatalf("got %v", evt)
	}
	select {
	case <-done:
		t.Fatal("sleep must wait for the ack")
	case <-time.After(50 * time.Millisecond):
	}
	w.Ack()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ack did not release sleep")
	}

	// Wake does not wait; an unacknowledged sleep times out.
	deliverPowerEvent(powerEventWake, time.Second)
	if evt := <-w.Events(); evt != powerEventWake {
		t.Fatalf("got %v", evt)
	}
	start := time.Now()
	deliverPowerEvent(powerEventSleep, 30*time.Millisecond)
	if time.Since(start) > time.Second {
		t.Fatal("sleep wait must time out")
	}
	<-w.Events()

	cancel()
	select {
	case _, ok := <-w.Events():
		if ok {
			t.Fatal("expected closed events after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("events not closed")
	}
}

func TestNewPowerWatcherRegisters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := newPowerWatcher(ctx)
	if err != nil {
		t.Fatalf("IOKit registration: %v", err)
	}
	if w.Events() == nil {
		t.Fatal("no events channel")
	}
}
