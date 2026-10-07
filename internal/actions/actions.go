package actions

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

var execCommand = exec.Command
var httpClient = &http.Client{Timeout: 3 * time.Second}

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
	case "http":
		return runHTTP(a.URL)
	default:
		return fmt.Errorf("unknown action type %q", a.Type)
	}
}

func runShell(cmd string) error {
	c := execCommand("sh", "-c", cmd)
	if err := c.Start(); err != nil {
		return fmt.Errorf("start shell action: %w", err)
	}
	// Reap the child; without Wait every press leaves a zombie process
	// for the lifetime of the daemon.
	go func() { _ = c.Wait() }()
	return nil
}

func runHTTP(url string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return explainHTTPError(url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http action status: %s", resp.Status)
	}
	return nil
}

func explainHTTPError(rawURL string, err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		msg := strings.ToLower(urlErr.Err.Error())
		if strings.Contains(msg, "connection refused") || strings.Contains(msg, "timeout") {
			return fmt.Errorf("http action request to %s failed: %v. Is the target local service running?", rawURL, urlErr.Err)
		}
	}
	return fmt.Errorf("http action request to %s failed: %w", rawURL, err)
}
