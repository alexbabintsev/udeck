//go:build !darwin

package main

import "context"

// newPowerWatcher is unavailable outside macOS.
func newPowerWatcher(context.Context) (*powerWatcher, error) { return nil, nil }
