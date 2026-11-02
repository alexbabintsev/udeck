//go:build !darwin

// Package mainloop runs the program while the macOS main thread serves
// the main run loop; elsewhere it just runs the program.
package mainloop

import "os"

// Run exits the process with fn's result.
func Run(fn func() int) { os.Exit(fn()) }
