// Package debug provides opt-in verbose diagnostics. It is enabled by the
// --debug flag or the UDECK_DEBUG=1 environment variable and is silent
// otherwise, so call sites can log freely on hot paths.
package debug

import (
	"log"
	"os"
	"sync/atomic"
)

var enabled atomic.Bool

func init() {
	if v := os.Getenv("UDECK_DEBUG"); v != "" && v != "0" {
		enabled.Store(true)
	}
}

// SetEnabled turns verbose diagnostics on or off.
func SetEnabled(on bool) { enabled.Store(on) }

// Enabled reports whether verbose diagnostics are on.
func Enabled() bool { return enabled.Load() }

// Logf writes a "[debug]"-prefixed line to the standard logger when
// diagnostics are enabled.
func Logf(format string, args ...any) {
	if !enabled.Load() {
		return
	}
	log.Printf("[debug] "+format, args...)
}
