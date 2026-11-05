//go:build personal

package config

import (
	"fmt"
	"strings"
)

func init() {
	widgetMinPollMS["freqtrade"] = 3000
	widgetMinPollMS["site"] = 30000
	widgetMinPollMS["project"] = 1000
}

func personalAction(typ string) bool { return typ == "close_all_projects" }

func validatePersonalWidget(w *Widget, path string) error {
	switch w.Type {
	case "freqtrade":
		if strings.TrimSpace(w.DSNs) == "" {
			return fmt.Errorf("%s.dsns is empty", path)
		}
	case "site":
		if strings.TrimSpace(w.URL) == "" {
			return fmt.Errorf("%s.url is empty", path)
		}
		if !strings.HasPrefix(w.URL, "http://") && !strings.HasPrefix(w.URL, "https://") {
			return fmt.Errorf("%s.url must start with http:// or https://", path)
		}
	case "project":
		if strings.TrimSpace(w.ProjectDir) == "" {
			return fmt.Errorf("%s.project_dir is empty", path)
		}
		if w.HoldMS != 0 && w.HoldMS < 100 {
			return fmt.Errorf("%s.hold_ms too small: %d", path, w.HoldMS)
		}
		if err := validateProjectWidgetOn(w.On, path+".on"); err != nil {
			return err
		}
	}
	return nil
}

func validateProjectWidgetOn(on *WidgetOn, path string) error {
	if on == nil {
		return nil
	}
	count := 0
	if on.Tap != "" {
		count++
		if err := validateProjectWidgetActionName(on.Tap, path+".tap"); err != nil {
			return err
		}
	}
	if on.DoubleTap != "" {
		count++
		if err := validateProjectWidgetActionName(on.DoubleTap, path+".double_tap"); err != nil {
			return err
		}
	}
	if on.Hold != "" {
		count++
		if err := validateProjectWidgetActionName(on.Hold, path+".hold"); err != nil {
			return err
		}
	}
	if on.Repeat != "" {
		count++
		if err := validateProjectWidgetActionName(on.Repeat, path+".repeat"); err != nil {
			return err
		}
	}
	if count == 0 {
		return fmt.Errorf("%s has no gestures", path)
	}
	return nil
}

func validateProjectWidgetActionName(value string, path string) error {
	switch strings.TrimSpace(value) {
	case "start", "stop", "focus":
		return nil
	default:
		return fmt.Errorf("%s %q unknown (start|stop|focus)", path, value)
	}
}
