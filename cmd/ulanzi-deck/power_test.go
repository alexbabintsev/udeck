package main

import (
	"strings"
	"testing"
)

func TestScanPowerEvents(t *testing.T) {
	done := false
	ch := scanPowerEvents(strings.NewReader("sleep\n  wake \nnoise\nshutdown\n"), func() { done = true })
	var got []powerEvent
	for evt := range ch {
		got = append(got, evt)
	}
	if len(got) != 3 || got[0] != powerEventSleep || got[1] != powerEventWake || got[2] != powerEventShutdown {
		t.Fatalf("got %v", got)
	}
	if !done {
		t.Fatal("done callback must run when the stream ends")
	}
	var nilWatcher *powerWatcher
	if nilWatcher.Events() != nil {
		t.Fatal("nil watcher must have no events")
	}
}
