package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the v1 schema. Most fields are accepted but unused — they
// exist so users can write a forward-compatible config file today.
type Config struct {
	Device    DeviceConfig    `yaml:"device"`
	Buttons   []Button        `yaml:"buttons,omitempty"`   // not used in v1
	SmallView *SmallViewBlock `yaml:"smallview,omitempty"` // not used in v1
}

type DeviceConfig struct {
	Brightness *int `yaml:"brightness,omitempty"`
}

// Button is the v2+ shape; parsed but not acted upon in v1.
type Button struct {
	At     [2]int `yaml:"at"`
	Label  string `yaml:"label,omitempty"`
	Icon   string `yaml:"icon,omitempty"`
	Action map[string]any `yaml:"action,omitempty"`
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
	}
	return nil
}
