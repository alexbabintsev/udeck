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
var sleepFunc = time.Sleep

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

// RunBinding executes either a direct action or a chain in order.
func RunBinding(binding *config.GestureBinding) error {
	if binding == nil {
		return nil
	}
	if action := binding.Action(); action != nil {
		return Run(action)
	}
	for i := range binding.Chain {
		step := binding.Chain[i]
		if step.DelayMS > 0 {
			sleepFunc(time.Duration(step.DelayMS) * time.Millisecond)
			continue
		}
		if step.Action != nil {
			if err := Run(step.Action); err != nil {
				return err
			}
		}
	}
	return nil
}

// RunAll fires every action in order and returns the first error, if any.
func RunAll(actions []config.Action) error {
	for i := range actions {
		if err := Run(&actions[i]); err != nil {
			return err
		}
	}
	return nil
}

func runShell(cmd string) error {
	c := execCommand("sh", "-c", cmd)
	if err := c.Start(); err != nil {
		return fmt.Errorf("start shell action: %w", err)
	}
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
