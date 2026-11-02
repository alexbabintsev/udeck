//go:build darwin

// Package mainloop runs the program while the macOS main thread serves
// the main run loop. Some system events, such as distributed
// notifications, are only delivered through the main run loop.
package mainloop

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>

static void keepalive_noop(CFRunLoopTimerRef timer, void *info) {}

static void run_main_loop(void) {
	// CFRunLoopRun returns at once when the loop has no sources; this
	// timer never fires in practice and keeps it blocking.
	CFRunLoopTimerRef keep = CFRunLoopTimerCreate(NULL,
		CFAbsoluteTimeGetCurrent() + 1e10, 1e10, 0, 0, keepalive_noop, NULL);
	CFRunLoopAddTimer(CFRunLoopGetCurrent(), keep, kCFRunLoopCommonModes);
	for (;;) {
		CFRunLoopRun();
	}
}
*/
import "C"

import (
	"os"
	"runtime"
)

// Init functions run on the main thread; locking here keeps main() there.
func init() { runtime.LockOSThread() }

// Run calls fn on another goroutine, serves the main run loop on the
// main thread, and exits the process with fn's result. It must be called
// from main() and never returns.
func Run(fn func() int) {
	go func() { os.Exit(fn()) }()
	C.run_main_loop()
}
