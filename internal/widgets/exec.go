package widgets

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// execCommandContext runs helper binaries; tests replace it.
var execCommandContext = exec.CommandContext

func execOut(cmd string, args []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := execCommandContext(ctx, cmd, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func expandWidgetPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
