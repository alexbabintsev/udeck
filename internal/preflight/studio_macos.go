package preflight

import (
	"errors"
	"os/exec"
)

// StudioRunning returns nil if Ulanzi Studio is NOT detected, or an
// error describing the conflict. macOS-only; on other platforms it
// returns nil and lets the HID open call surface the real error.
func StudioRunning() error {
	cmd := exec.Command("pgrep", "-f", "Ulanzi Studio")
	out, err := cmd.Output()
	if err != nil {
		// pgrep exits 1 when no match — treat as "not running".
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil
		}
		// pgrep missing or other failure — don't block startup.
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return errors.New("Ulanzi Studio is running and holds an exclusive HID lock on the D200. " +
		"Quit it first: `osascript -e 'tell application \"Ulanzi Studio\" to quit'`")
}
