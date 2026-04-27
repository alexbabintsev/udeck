package actions

import (
	"fmt"
	"os/exec"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

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
	c := exec.Command("sh", "-c", cmd)
	c.Start()
	return nil
}
