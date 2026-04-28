//go:build !darwin

package actions

// RequestKeyPermissions is a no-op on non-macOS platforms.
func RequestKeyPermissions() {}
