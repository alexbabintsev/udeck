//go:build !darwin

package actions

import "fmt"

func sendKey(key string) error {
	return fmt.Errorf("key actions not supported on this platform")
}
