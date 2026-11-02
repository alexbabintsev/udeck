//go:build darwin

package main

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <IOKit/IOMessage.h>

extern void goPowerMessage(int kind);

enum { POWER_SLEEP = 1, POWER_WAKE = 2 };

static io_connect_t power_root = MACH_PORT_NULL;

static void power_cb(void *refcon, io_service_t service, natural_t type, void *arg) {
	switch (type) {
	case kIOMessageCanSystemSleep:
		IOAllowPowerChange(power_root, (long)arg);
		break;
	case kIOMessageSystemWillSleep:
		// Returns once the deck is dimmed and closed, or after a timeout.
		goPowerMessage(POWER_SLEEP);
		IOAllowPowerChange(power_root, (long)arg);
		break;
	case kIOMessageSystemHasPoweredOn:
		goPowerMessage(POWER_WAKE);
		break;
	}
}

// power_register subscribes to system power messages on the calling
// thread's run loop.
static int power_register(void) {
	IONotificationPortRef port;
	io_object_t notifier;
	power_root = IORegisterForSystemPower(NULL, &port, power_cb, &notifier);
	if (power_root == MACH_PORT_NULL) {
		return -1;
	}
	CFRunLoopAddSource(CFRunLoopGetCurrent(),
		IONotificationPortGetRunLoopSource(port), kCFRunLoopDefaultMode);
	return 0;
}

static void power_run(void) {
	for (;;) {
		CFRunLoopRun();
	}
}
*/
import "C"

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"time"
)

// sleepAckTimeout bounds how long system sleep waits for the deck to be
// dimmed and closed.
const sleepAckTimeout = 5 * time.Second

type powerSub struct {
	events chan powerEvent
	ack    chan struct{}
}

// power fans IOKit power messages out to watchers. The IOKit
// registration and its thread live for the whole process.
var power struct {
	once sync.Once
	err  error
	mu   sync.Mutex
	subs map[*powerSub]struct{}
}

//export goPowerMessage
func goPowerMessage(kind C.int) {
	evt := powerEventWake
	if kind == C.POWER_SLEEP {
		evt = powerEventSleep
	}
	deliverPowerEvent(evt, sleepAckTimeout)
}

// deliverPowerEvent sends evt to every watcher. For sleep it then waits
// for each watcher to acknowledge, at most timeout in total.
func deliverPowerEvent(evt powerEvent, timeout time.Duration) {
	var waiting []*powerSub
	power.mu.Lock()
	for sub := range power.subs {
		select { // drop a stale ack from an earlier, timed-out sleep
		case <-sub.ack:
		default:
		}
		select {
		case sub.events <- evt:
			if evt == powerEventSleep {
				waiting = append(waiting, sub)
			}
		default:
		}
	}
	power.mu.Unlock()

	deadline := time.After(timeout)
	for _, sub := range waiting {
		select {
		case <-sub.ack:
		case <-deadline:
			return
		}
	}
}

func newPowerWatcher(ctx context.Context) (*powerWatcher, error) {
	power.once.Do(func() {
		power.subs = map[*powerSub]struct{}{}
		errc := make(chan error, 1)
		go func() {
			runtime.LockOSThread() // the run loop belongs to this thread
			if C.power_register() != 0 {
				errc <- errors.New("IORegisterForSystemPower failed")
				return
			}
			errc <- nil
			C.power_run()
		}()
		power.err = <-errc
	})
	if power.err != nil {
		return nil, power.err
	}
	return subscribePower(ctx), nil
}

// subscribePower adds a watcher that is removed when ctx is done.
func subscribePower(ctx context.Context) *powerWatcher {
	sub := &powerSub{events: make(chan powerEvent, 8), ack: make(chan struct{}, 1)}
	power.mu.Lock()
	if power.subs == nil {
		power.subs = map[*powerSub]struct{}{}
	}
	power.subs[sub] = struct{}{}
	power.mu.Unlock()
	go func() {
		<-ctx.Done()
		power.mu.Lock()
		delete(power.subs, sub)
		power.mu.Unlock()
		close(sub.events)
	}()
	return &powerWatcher{
		events: sub.events,
		ack: func() {
			select {
			case sub.ack <- struct{}{}:
			default:
			}
		},
	}
}
