//go:build !darwin

package preflight

// StudioRunning is a no-op on non-macOS platforms.
func StudioRunning() error {
	return nil
}
