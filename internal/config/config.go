package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level schema.
type Config struct {
	Device    DeviceConfig    `yaml:"device"`
	Buttons   []Button        `yaml:"buttons,omitempty"`
	SmallView *SmallViewBlock `yaml:"smallview,omitempty"` // reserved for v3
}

type DeviceConfig struct {
	Brightness *int `yaml:"brightness,omitempty"`
}

// Button describes one key slot on the deck.
type Button struct {
	// At is [col, row], zero-indexed. col ∈ [0,4], row ∈ [0,2].
	At     [2]int  `yaml:"at"`
	Label  string  `yaml:"label,omitempty"`
	Icon   string  `yaml:"icon,omitempty"` // path, ~ expanded
	Action *Action `yaml:"action,omitempty"`
}

// HIDIndex converts [col, row] to the flat button index the device uses.
func (b Button) HIDIndex() int {
	return b.At[1]*5 + b.At[0]
}

// Action is the handler fired when a button is pressed.
type Action struct {
	// Type is "shell" or "key".
	Type string `yaml:"type"`

	// Cmd is the shell command run for type=shell.
	Cmd string `yaml:"cmd,omitempty"`

	// Key is a key name for type=key, e.g. "return", "space", "f5".
	// Modifiers are space-separated prefixes: "cmd shift s".
	Key string `yaml:"key,omitempty"`
}

type SmallViewBlock struct {
	Mode      string `yaml:"mode,omitempty"`
	RefreshMS int    `yaml:"refresh_ms,omitempty"`
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
	for i, btn := range c.Buttons {
		if btn.At[0] < 0 || btn.At[0] > 4 || btn.At[1] < 0 || btn.At[1] > 2 {
			return fmt.Errorf("buttons[%d].at out of range: %v", i, btn.At)
		}
		if a := btn.Action; a != nil {
			switch a.Type {
			case "shell":
				if a.Cmd == "" {
					return fmt.Errorf("buttons[%d].action.cmd is empty", i)
				}
			case "key":
				if a.Key == "" {
					return fmt.Errorf("buttons[%d].action.key is empty", i)
				}
			default:
				return fmt.Errorf("buttons[%d].action.type %q unknown (shell|key)", i, a.Type)
			}
		}
	}
	return nil
}
