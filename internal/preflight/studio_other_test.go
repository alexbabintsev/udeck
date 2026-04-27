//go:build !darwin

package preflight

import "testing"

func TestStudioRunningNoopOnNonDarwin(t *testing.T) {
	if err := StudioRunning(); err != nil {
		t.Fatalf("expected nil on non-darwin, got %v", err)
	}
}
