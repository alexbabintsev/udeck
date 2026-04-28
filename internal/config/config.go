package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level schema.
type Config struct {
	Device    DeviceConfig    `yaml:"device"`
	Widgets   WidgetSettings  `yaml:"widgets,omitempty"`
	Buttons   []Button        `yaml:"buttons,omitempty"`
	SmallView *SmallViewBlock `yaml:"smallview,omitempty"` // reserved for v3
}

type WidgetSettings struct {
	Project ProjectWidgetSettings `yaml:"project,omitempty"`
}

type ProjectWidgetSettings struct {
	Terminal     string `yaml:"terminal,omitempty"`
	Editor       string `yaml:"editor,omitempty"`
	EditorSubdir string `yaml:"editor_subdir,omitempty"`
}

type DeviceConfig struct {
	Brightness *int        `yaml:"brightness,omitempty"`
	LabelStyle *LabelStyle `yaml:"label_style,omitempty"`
}

// LabelStyle controls button label rendering. All fields optional;
// unset fields use device defaults.
type LabelStyle struct {
	Align     string `yaml:"align,omitempty"`      // "top" | "bottom" | "center"
	Color     string `yaml:"color,omitempty"`      // hex RRGGBB, e.g. "FFFFFF"
	FontName  string `yaml:"font_name,omitempty"`  // "Roboto"
	ShowTitle *bool  `yaml:"show_title,omitempty"` // default true
	Size      *int   `yaml:"size,omitempty"`       // font size, default 10
	Weight    *int   `yaml:"weight,omitempty"`     // font weight, default 80
}

// Button describes one key slot on the deck.
type Button struct {
	// At is [col, row], zero-indexed. col ∈ [0,4], row ∈ [0,2].
	At        [2]int     `yaml:"at"`
	Label     string     `yaml:"label,omitempty"`
	Icon      string     `yaml:"icon,omitempty"` // path, ~ expanded
	On        *Handlers  `yaml:"on,omitempty"`
	Directory *Directory `yaml:"directory,omitempty"`
	Widget    *Widget    `yaml:"widget,omitempty"`
	RuntimeID string     `yaml:"-"`
}

// Directory defines a nested page of buttons. When opened at runtime, a
// synthetic "Back" button is injected automatically.
type Directory struct {
	Buttons []Button `yaml:"buttons,omitempty"`
}

// Widget defines a dynamic button that manages its own icon and press logic.
type Widget struct {
	Type         string        `yaml:"type"`
	Apps         []string      `yaml:"apps,omitempty"`
	PollMS       int           `yaml:"poll_ms,omitempty"`
	ProjectDir   string        `yaml:"project_dir,omitempty"`
	Session      string        `yaml:"session,omitempty"`
	EditorSubdir string        `yaml:"editor_subdir,omitempty"`
	Scripts      WidgetScripts `yaml:"scripts,omitempty"`
	LongPressMS  int           `yaml:"long_press_ms,omitempty"`
}

type WidgetScripts struct {
	Start string `yaml:"start,omitempty"`
	Stop  string `yaml:"stop,omitempty"`
	Focus string `yaml:"focus,omitempty"`
}

// Handlers defines interaction handlers for a button. This is only valid
// for action buttons and is mutually exclusive with Directory/Widget.
type Handlers struct {
	Tap           *Action `yaml:"tap,omitempty"`
	DoubleTap     *Action `yaml:"double_tap,omitempty"`
	LongPress     *Action `yaml:"long_press,omitempty"`
	Repeat        *Action `yaml:"repeat,omitempty"`
	LongPressMS   int     `yaml:"long_press_ms,omitempty"`
	DoubleTapMS   int     `yaml:"double_tap_ms,omitempty"`
	RepeatEveryMS int     `yaml:"repeat_every_ms,omitempty"`
}

// HIDIndex converts [col, row] to the flat button index the device uses.
func (b Button) HIDIndex() int {
	return b.At[1]*5 + b.At[0]
}

// Action is the handler fired when a button is pressed.
type Action struct {
	// Type is "shell", "key", or "http".
	Type string `yaml:"type"`

	// Cmd is the shell command run for type=shell.
	Cmd string `yaml:"cmd,omitempty"`

	// Key is a key name for type=key, e.g. "return", "space", "f5".
	// Modifiers are space-separated prefixes: "cmd shift s".
	Key string `yaml:"key,omitempty"`

	// URL is the endpoint called for type=http.
	URL string `yaml:"url,omitempty"`
}

type SmallViewBlock struct {
	Mode      string `yaml:"mode,omitempty"`
	RefreshMS int    `yaml:"refresh_ms,omitempty"`
}

// IconPaths returns all configured static icon paths, including icons in
// nested directories. Paths are returned in first-seen order.
func (c *Config) IconPaths() []string {
	var out []string
	seen := map[string]struct{}{}
	collectIconPaths(c.Buttons, seen, &out)
	return out
}

// Load reads + parses a YAML config file. Returns a non-nil *Config and
// any validation error.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	assignButtonIDs(c.Buttons, "buttons")
	resolveButtonPaths(c.Buttons, filepath.Dir(path))
	if err := c.Validate(); err != nil {
		return &c, err
	}
	return &c, nil
}

func (c *Config) Validate() error {
	if c.Device.Brightness != nil {
		b := *c.Device.Brightness
		if b < 0 || b > 100 {
			return fmt.Errorf("device.brightness out of range: %d", b)
		}
	}
	if c.Device.LabelStyle != nil {
		s := c.Device.LabelStyle
		if s.Align != "" && s.Align != "top" && s.Align != "bottom" && s.Align != "center" {
			return fmt.Errorf("device.label_style.align %q unknown (top|bottom|center)", s.Align)
		}
		if s.Color != "" {
			if err := validateHexColor(s.Color); err != nil {
				return fmt.Errorf("device.label_style.color: %w", err)
			}
		}
	}
	return validateButtons(c.Buttons, "buttons", false)
}

func validateHexColor(s string) error {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return fmt.Errorf("must be exactly 6 hex digits")
	}
	if _, err := hex.DecodeString(s); err != nil {
		return fmt.Errorf("must be valid hex RRGGBB")
	}
	return nil
}

func validateButtons(buttons []Button, path string, reserveBack bool) error {
	seen := make(map[[2]int]int, len(buttons))
	for i, btn := range buttons {
		if btn.At[0] < 0 || btn.At[0] > 4 || btn.At[1] < 0 || btn.At[1] > 2 {
			return fmt.Errorf("%s[%d].at out of range: %v", path, i, btn.At)
		}
		if reserveBack && btn.At == [2]int{3, 2} {
			return fmt.Errorf("%s[%d].at conflicts with reserved Back button slot: %v", path, i, btn.At)
		}
		if prev, ok := seen[btn.At]; ok {
			return fmt.Errorf("%s[%d].at duplicates %s[%d].at: %v", path, i, path, prev, btn.At)
		}
		seen[btn.At] = i
		modeCount := 0
		if btn.On != nil {
			modeCount++
		}
		if btn.Directory != nil {
			modeCount++
		}
		if btn.Widget != nil {
			modeCount++
		}
		if modeCount > 1 {
			return fmt.Errorf("%s[%d] cannot combine on, directory, and widget", path, i)
		}
		if tr := btn.On; tr != nil {
			actionCount := 0
			if tr.Tap != nil {
				actionCount++
				if err := validateAction(tr.Tap, fmt.Sprintf("%s[%d].on.tap", path, i)); err != nil {
					return err
				}
			}
			if tr.DoubleTap != nil {
				actionCount++
				if err := validateAction(tr.DoubleTap, fmt.Sprintf("%s[%d].on.double_tap", path, i)); err != nil {
					return err
				}
			}
			if tr.LongPress != nil {
				actionCount++
				if err := validateAction(tr.LongPress, fmt.Sprintf("%s[%d].on.long_press", path, i)); err != nil {
					return err
				}
			}
			if tr.Repeat != nil {
				actionCount++
				if err := validateAction(tr.Repeat, fmt.Sprintf("%s[%d].on.repeat", path, i)); err != nil {
					return err
				}
			}
			if actionCount == 0 {
				return fmt.Errorf("%s[%d].on has no actions", path, i)
			}
			if tr.LongPressMS != 0 && tr.LongPressMS < 100 {
				return fmt.Errorf("%s[%d].on.long_press_ms too small: %d", path, i, tr.LongPressMS)
			}
			if tr.DoubleTapMS != 0 && tr.DoubleTapMS < 100 {
				return fmt.Errorf("%s[%d].on.double_tap_ms too small: %d", path, i, tr.DoubleTapMS)
			}
			if tr.RepeatEveryMS != 0 && tr.RepeatEveryMS < 50 {
				return fmt.Errorf("%s[%d].on.repeat_every_ms too small: %d", path, i, tr.RepeatEveryMS)
			}
		}
		if w := btn.Widget; w != nil {
			switch w.Type {
			case "memory", "spotify_play_pause", "spotify_next", "spotify_artwork":
				if w.PollMS != 0 && w.PollMS < 1000 {
					return fmt.Errorf("%s[%d].widget.poll_ms too small: %d", path, i, w.PollMS)
				}
			case "project":
				if strings.TrimSpace(w.ProjectDir) == "" {
					return fmt.Errorf("%s[%d].widget.project_dir is empty", path, i)
				}
				if w.PollMS != 0 && w.PollMS < 1000 {
					return fmt.Errorf("%s[%d].widget.poll_ms too small: %d", path, i, w.PollMS)
				}
				if w.LongPressMS != 0 && w.LongPressMS < 100 {
					return fmt.Errorf("%s[%d].widget.long_press_ms too small: %d", path, i, w.LongPressMS)
				}
			default:
				return fmt.Errorf("%s[%d].widget.type %q unknown (memory|spotify_play_pause|spotify_next|spotify_artwork|project)", path, i, w.Type)
			}
		}
		if d := btn.Directory; d != nil {
			if len(d.Buttons) == 0 {
				return fmt.Errorf("%s[%d].directory.buttons is empty", path, i)
			}
			childPath := fmt.Sprintf("%s[%d].directory.buttons", path, i)
			if err := validateButtons(d.Buttons, childPath, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateAction(a *Action, path string) error {
	switch a.Type {
	case "shell":
		if a.Cmd == "" {
			return fmt.Errorf("%s.cmd is empty", path)
		}
	case "key":
		if a.Key == "" {
			return fmt.Errorf("%s.key is empty", path)
		}
	case "http":
		if a.URL == "" {
			return fmt.Errorf("%s.url is empty", path)
		}
	default:
		return fmt.Errorf("%s.type %q unknown (shell|key|http)", path, a.Type)
	}
	return nil
}

func resolveButtonPaths(buttons []Button, baseDir string) {
	for i := range buttons {
		if buttons[i].Icon != "" && !filepath.IsAbs(buttons[i].Icon) && !strings.HasPrefix(buttons[i].Icon, "~/") {
			if _, err := os.Stat(buttons[i].Icon); err != nil {
				buttons[i].Icon = filepath.Join(baseDir, buttons[i].Icon)
			}
		}
		if buttons[i].Widget != nil && buttons[i].Widget.ProjectDir != "" &&
			!filepath.IsAbs(buttons[i].Widget.ProjectDir) && !strings.HasPrefix(buttons[i].Widget.ProjectDir, "~/") {
			buttons[i].Widget.ProjectDir = filepath.Join(baseDir, buttons[i].Widget.ProjectDir)
		}
		if buttons[i].Directory != nil {
			resolveButtonPaths(buttons[i].Directory.Buttons, baseDir)
		}
	}
}

func assignButtonIDs(buttons []Button, path string) {
	for i := range buttons {
		buttons[i].RuntimeID = fmt.Sprintf("%s[%d]", path, i)
		if buttons[i].Directory != nil {
			assignButtonIDs(buttons[i].Directory.Buttons, buttons[i].RuntimeID+".directory.buttons")
		}
	}
}

func collectIconPaths(buttons []Button, seen map[string]struct{}, out *[]string) {
	for _, btn := range buttons {
		if btn.Icon != "" {
			if _, ok := seen[btn.Icon]; !ok {
				seen[btn.Icon] = struct{}{}
				*out = append(*out, btn.Icon)
			}
		}
		if btn.Directory != nil {
			collectIconPaths(btn.Directory.Buttons, seen, out)
		}
	}
}
