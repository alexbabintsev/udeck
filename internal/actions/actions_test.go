package actions

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRunShellReturnsStartError(t *testing.T) {
	oldExecCommand := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/definitely-missing-binary-for-test")
	}
	t.Cleanup(func() {
		execCommand = oldExecCommand
	})

	err := runShell("echo hello")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "start shell action") {
		t.Fatalf("expected wrapped start error, got %v", err)
	}
}
