package main

import (
	"log"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/actions"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/widgets"
)

// runExternalAction executes shell/key/http actions; tests replace it.
var runExternalAction = actions.Run

func resolveBindingEvent(evt *gestureEvent, toggles *toggleManager) (*config.GestureBinding, bool) {
	if evt == nil || evt.Button == nil || evt.Button.On == nil {
		return nil, false
	}
	if toggles != nil {
		if binding, changed := toggles.Handle(evt.ButtonID, evt.Kind); binding != nil || changed {
			return binding, changed
		}
	}
	return bindingForKind(evt.Button.On, evt.Kind), false
}

func runBinding(binding *config.GestureBinding, nav *navigator, widgetMgr *widgets.Manager) (bool, error) {
	if binding == nil {
		return false, nil
	}
	if action := binding.Action(); action != nil {
		return runAction(action, nav, widgetMgr)
	}
	pageChanged := false
	for i := range binding.Chain {
		step := binding.Chain[i]
		if step.DelayMS > 0 {
			pause(time.Duration(step.DelayMS) * time.Millisecond)
			continue
		}
		if step.Action == nil {
			continue
		}
		changed, err := runAction(step.Action, nav, widgetMgr)
		if err != nil {
			return pageChanged, err
		}
		pageChanged = pageChanged || changed
	}
	return pageChanged, nil
}

func runAction(action *config.Action, nav *navigator, widgetMgr *widgets.Manager) (bool, error) {
	if action == nil {
		return false, nil
	}
	switch action.Type {
	case "page_next":
		if nav == nil {
			return false, nil
		}
		return nav.nextPage(), nil
	case "page_prev":
		if nav == nil {
			return false, nil
		}
		return nav.prevPage(), nil
	case "close_all_projects":
		if widgetMgr == nil {
			return false, nil
		}
		return false, widgetMgr.CloseAllProjects()
	default:
		return false, runExternalAction(action)
	}
}

// bindingNeedsLoop reports whether a binding contains an action that
// mutates loop-owned state (page navigation).
func bindingNeedsLoop(binding *config.GestureBinding) bool {
	needs := func(a *config.Action) bool {
		return a != nil && (a.Type == "page_next" || a.Type == "page_prev")
	}
	if needs(binding.Action()) {
		return true
	}
	for i := range binding.Chain {
		if needs(binding.Chain[i].Action) {
			return true
		}
	}
	return false
}

// actionRunner executes actions in order on a single worker goroutine.
type actionRunner struct {
	jobs chan func() error
	done chan struct{}
}

func newActionRunner() *actionRunner {
	r := &actionRunner{jobs: make(chan func() error, 32), done: make(chan struct{})}
	go func() {
		defer close(r.done)
		for job := range r.jobs {
			if err := job(); err != nil {
				log.Printf("action error: %v", err)
			}
		}
	}()
	return r
}

func (r *actionRunner) submit(job func() error) {
	select {
	case r.jobs <- job:
	default:
		log.Println("action queue full; dropping press")
	}
}

// close lets queued actions finish and stops the worker. No submit may
// follow.
func (r *actionRunner) close() {
	close(r.jobs)
	<-r.done
}
