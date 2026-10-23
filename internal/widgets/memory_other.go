//go:build !darwin

package widgets

import "errors"

func readVMStats() (vmStats, error) {
	return vmStats{}, errors.New("memory statistics are only implemented on macOS")
}
