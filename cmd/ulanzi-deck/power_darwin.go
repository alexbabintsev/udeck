//go:build darwin

package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strings"
)

const powerWatcherSwift = `import AppKit
import Foundation

final class Observer: NSObject {
    @objc func onSleep(_ note: Notification) {
        print("sleep")
        fflush(stdout)
    }

    @objc func onWake(_ note: Notification) {
        print("wake")
        fflush(stdout)
    }

    @objc func onShutdown(_ note: Notification) {
        print("shutdown")
        fflush(stdout)
    }
}

let observer = Observer()
let center = NSWorkspace.shared.notificationCenter
center.addObserver(observer, selector: #selector(Observer.onSleep(_:)), name: NSWorkspace.willSleepNotification, object: nil)
center.addObserver(observer, selector: #selector(Observer.onWake(_:)), name: NSWorkspace.didWakeNotification, object: nil)
center.addObserver(observer, selector: #selector(Observer.onShutdown(_:)), name: NSWorkspace.willPowerOffNotification, object: nil)
RunLoop.main.run()
`

type powerEvent string

const (
	powerEventSleep    powerEvent = "sleep"
	powerEventWake     powerEvent = "wake"
	powerEventShutdown powerEvent = "shutdown"
)

type powerWatcher struct {
	events <-chan powerEvent
}

func newPowerWatcher(ctx context.Context) (*powerWatcher, error) {
	if _, err := os.Stat("/usr/bin/swift"); err != nil {
		return nil, err
	}
	script, err := os.CreateTemp("", "ulanzi-power-watcher-*.swift")
	if err != nil {
		return nil, err
	}
	if _, err := script.WriteString(powerWatcherSwift); err != nil {
		script.Close()
		_ = os.Remove(script.Name())
		return nil, err
	}
	if err := script.Close(); err != nil {
		_ = os.Remove(script.Name())
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "/usr/bin/swift", script.Name())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.Remove(script.Name())
		return nil, err
	}
	cmd.Stderr = ioDiscard{}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(script.Name())
		return nil, err
	}

	ch := make(chan powerEvent, 8)
	go func() {
		defer close(ch)
		defer cmd.Wait()
		defer os.Remove(script.Name())

		scanner := bufio.NewScanner(stdout)
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

	return &powerWatcher{events: ch}, nil
}

func (w *powerWatcher) Events() <-chan powerEvent {
	if w == nil {
		return nil
	}
	return w.events
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
