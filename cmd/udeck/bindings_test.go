package main

import (
	"errors"
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
)

func TestRunBindingChainDelaysStopsOnErrorAndTracksPageChange(t *testing.T) {
	var paused time.Duration
	old := pause
	pause = func(d time.Duration) { paused += d }
	defer func() { pause = old }()
	stubActions(t)

	nav := newNavigator([]config.Page{{}, {}})
	changed, err := runBinding(&config.GestureBinding{Chain: []config.ChainStep{
		{DelayMS: 25},
		{Action: &config.Action{Type: "shell", Cmd: "x"}},
		{},
		{Action: &config.Action{Type: "page_prev"}},
	}}, nav, nil)
	if err != nil || !changed || paused != 25*time.Millisecond || nav.page != 1 {
		t.Fatalf("changed=%v err=%v paused=%v page=%d", changed, err, paused, nav.page)
	}

	runExternalAction = func(*config.Action) error { return errors.New("fail") }
	if _, err := runBinding(&config.GestureBinding{Chain: []config.ChainStep{
		{Action: &config.Action{Type: "shell", Cmd: "x"}},
		{Action: &config.Action{Type: "page_next"}},
	}}, nav, nil); err == nil || nav.page != 1 {
		t.Fatalf("chain must stop at the failing step: err=%v page=%d", err, nav.page)
	}
}

func TestRunActionWithoutNavigatorOrManager(t *testing.T) {
	for _, typ := range []string{"page_next", "page_prev", "close_all_projects"} {
		if changed, err := runAction(&config.Action{Type: typ}, nil, nil); changed || err != nil {
			t.Fatalf("%s: changed=%v err=%v", typ, changed, err)
		}
	}
	if changed, err := runAction(nil, nil, nil); changed || err != nil {
		t.Fatal("nil action must be a no-op")
	}
	if changed, err := runBinding(nil, nil, nil); changed || err != nil {
		t.Fatal("nil binding must be a no-op")
	}
}

func TestActionRunnerRunsInOrderAndDropsWhenFull(t *testing.T) {
	r := &actionRunner{jobs: make(chan func() error, 1)}
	// No worker yet: the second submit overflows the buffer.
	ran := make(chan int, 3)
	r.submit(func() error { ran <- 1; return nil })
	r.submit(func() error { ran <- 2; return nil })
	if len(r.jobs) != 1 {
		t.Fatalf("expected the overflow job to be dropped, queue=%d", len(r.jobs))
	}

	w := newActionRunner()
	w.submit(func() error { ran <- 10; return nil })
	w.submit(func() error { ran <- 11; return errors.New("logged") })
	if a, b := <-ran, <-ran; a != 10 || b != 11 {
		t.Fatalf("order = %d,%d", a, b)
	}
}
