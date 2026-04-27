//go:build darwin

package preflight

import (
	"os/exec"
	"testing"
)

func TestStudioRunningReturnsConflictWhenPgrepFindsProcess(t *testing.T) {
	oldExecCommand := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "printf '123\\n'")
	}
	t.Cleanup(func() {
		execCommand = oldExecCommand
	})

	err := StudioRunning()
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
}

func TestStudioRunningIgnoresNoMatchExit(t *testing.T) {
	oldExecCommand := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "exit 1")
	}
	t.Cleanup(func() {
		execCommand = oldExecCommand
	})

	if err := StudioRunning(); err != nil {
		t.Fatalf("expected nil on no match, got %v", err)
	}
}
