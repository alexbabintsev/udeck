package main

import (
	"os"
	"time"
)

const configReloadPollInterval = 1 * time.Second

type configReloader struct {
	path    string
	changes chan struct{}
	stop    chan struct{}
	done    chan struct{}
	lastMod time.Time
	lastLen int64
}

func newConfigReloader(path string) *configReloader {
	r := &configReloader{
		path:    path,
		changes: make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	if info, err := os.Stat(path); err == nil {
		r.lastMod = info.ModTime()
		r.lastLen = info.Size()
	}
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

func (r *configReloader) loop() {
	defer close(r.done)
	ticker := time.NewTicker(configReloadPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			info, err := os.Stat(r.path)
			if err != nil {
				continue
			}
			mod := info.ModTime()
			size := info.Size()
			if mod.After(r.lastMod) || size != r.lastLen {
				r.lastMod = mod
				r.lastLen = size
				select {
				case r.changes <- struct{}{}:
				default:
				}
			}
		}
	}
}
