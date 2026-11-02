//go:build !darwin

package macos

func PressMediaKey(MediaKey) error { return ErrUnsupported }

func AccessibilityTrusted() bool { return false }
