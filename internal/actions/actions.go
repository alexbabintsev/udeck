package actions

import (
	"fmt"
	"os/exec"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

var execCommand = exec.Command

// Run fires the action associated with a button press.
func Run(a *config.Action) error {
	if a == nil {
		return nil
	}
	switch a.Type {
	case "shell":
		return runShell(a.Cmd)
	case "key":
		return sendKey(a.Key)
	default:
		return fmt.Errorf("unknown action type %q", a.Type)
	}
}

func runShell(cmd string) error {
	c := execCommand("sh", "-c", cmd)
	if err := c.Start(); err != nil {
		return fmt.Errorf("start shell action: %w", err)
	}
	return nil
}
