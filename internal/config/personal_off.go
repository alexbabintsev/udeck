//go:build !personal

package config

func personalAction(string) bool { return false }

func validatePersonalWidget(*Widget, string) error { return nil }
