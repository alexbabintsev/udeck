package main

import (
	"os"
	"sync"
	"time"
)

// configReloadPollInterval is a var so tests can poll faster.
var configReloadPollInterval = 1 * time.Second

type configReloader struct {
	mu      sync.RWMutex
	paths   []string
	state   map[string]fileState
	changes chan struct{}
	stop    chan struct{}
	done    chan struct{}
}

type fileState struct {
	exists bool
	mod    time.Time
	size   int64
}

func newConfigReloader(path string, iconPaths []string) *configReloader {
	r := &configReloader{
		state:   map[string]fileState{},
		changes: make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	r.setPaths(path, iconPaths)
	go r.loop()
	return r
}

func (r *configReloader) Changes() <-chan struct{} {
	return r.changes
}

func (r *configReloader) Close() {
	close(r.stop)
	<-r.done
}

func (r *configReloader) UpdatePaths(path string, iconPaths []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setPathsLocked(path, iconPaths)
}

func (r *configReloader) loop() {
	defer close(r.done)
	ticker := time.NewTicker(configReloadPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			if r.pollChanged() {
				select {
				case r.changes <- struct{}{}:
				default:
				}
			}
		}
	}
}

func (r *configReloader) pollChanged() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, path := range r.paths {
		next := statFile(path)
		prev := r.state[path]
		if next != prev {
			r.state[path] = next
			return true
		}
	}
	return false
}

func (r *configReloader) setPaths(path string, iconPaths []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setPathsLocked(path, iconPaths)
}

func (r *configReloader) setPathsLocked(path string, iconPaths []string) {
	paths := make([]string, 0, 1+len(iconPaths))
	seen := map[string]struct{}{}
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	add(path)
	for _, p := range iconPaths {
		add(p)
	}
	nextState := make(map[string]fileState, len(paths))
	for _, p := range paths {
		nextState[p] = statFile(p)
	}
	r.paths = paths
	r.state = nextState
}

func statFile(path string) fileState {
	info, err := os.Stat(path)
	if err != nil {
		return fileState{}
	}
	return fileState{
		exists: true,
		mod:    info.ModTime(),
		size:   info.Size(),
	}
}
