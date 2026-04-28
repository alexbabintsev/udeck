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
	Brightness  *int        `yaml:"brightness,omitempty"`
	LabelStyle  *LabelStyle `yaml:"label_style,omitempty"`
	IconMargins *Insets     `yaml:"icon_margins,omitempty"`
}

type Insets struct {
	Top    int `yaml:"top,omitempty"`
	Right  int `yaml:"right,omitempty"`
	Bottom int `yaml:"bottom,omitempty"`
	Left   int `yaml:"left,omitempty"`
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
	At          [2]int    `yaml:"at"`
	Label       string    `yaml:"label,omitempty"`
	Icon        string    `yaml:"icon,omitempty"` // path, ~ expanded
	IconMargins *Insets   `yaml:"icon_margins,omitempty"`
	On          *Handlers `yaml:"on,omitempty"`
	Folder      *Folder   `yaml:"folder,omitempty"`
	Widget      *Widget   `yaml:"widget,omitempty"`
	RuntimeID   string    `yaml:"-"`
}

type ToggleStep struct {
	Label  string `yaml:"label,omitempty"`
	Icon   string `yaml:"icon,omitempty"`
	Action Action `yaml:"action"`
}

type ChainStep struct {
	DelayMS int     `yaml:"delay_ms,omitempty"`
	Action  *Action `yaml:"action,omitempty"`
}

// Folder defines a nested page of buttons. When opened at runtime, a
// synthetic "Back" button is injected automatically.
type Folder struct {
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
	On           *WidgetOn     `yaml:"on,omitempty"`
	HoldMS       int           `yaml:"hold_ms,omitempty"`
}

type WidgetScripts struct {
	Start string `yaml:"start,omitempty"`
	Stop  string `yaml:"stop,omitempty"`
	Focus string `yaml:"focus,omitempty"`
}

type WidgetOn struct {
	Tap       string `yaml:"tap,omitempty"`
	DoubleTap string `yaml:"double_tap,omitempty"`
	Hold      string `yaml:"hold,omitempty"`
	Repeat    string `yaml:"repeat,omitempty"`
}

// Handlers defines interaction handlers for a button. This is only valid
// for action buttons and is mutually exclusive with Folder/Widget.
type Handlers struct {
	Tap           *GestureBinding `yaml:"tap,omitempty"`
	DoubleTap     *GestureBinding `yaml:"double_tap,omitempty"`
	Hold          *GestureBinding `yaml:"hold,omitempty"`
	Repeat        *GestureBinding `yaml:"repeat,omitempty"`
	HoldMS        int             `yaml:"hold_ms,omitempty"`
	DoubleTapMS   int             `yaml:"double_tap_ms,omitempty"`
	RepeatEveryMS int             `yaml:"repeat_every_ms,omitempty"`
}

type GestureBinding struct {
	Type   string       `yaml:"type,omitempty"`
	Cmd    string       `yaml:"cmd,omitempty"`
	Key    string       `yaml:"key,omitempty"`
	URL    string       `yaml:"url,omitempty"`
	Toggle []ToggleStep `yaml:"toggle,omitempty"`
	Chain  []ChainStep  `yaml:"chain,omitempty"`
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

func (g *GestureBinding) Action() *Action {
	if g == nil || len(g.Toggle) > 0 || len(g.Chain) > 0 || strings.TrimSpace(g.Type) == "" {
		return nil
	}
	return &Action{
		Type: g.Type,
		Cmd:  g.Cmd,
		Key:  g.Key,
		URL:  g.URL,
	}
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

// UsesKeyActions reports whether any button in the config tree triggers a
// `type: key` action, directly, through a toggle step, or through a chain.
func (c *Config) UsesKeyActions() bool {
	return buttonsUseKeyActions(c.Buttons)
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
	applyButtonDefaults(c.Buttons, c.Device.IconMargins)
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
	if err := validateInsets(c.Device.IconMargins, "device.icon_margins"); err != nil {
		return err
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

func validateInsets(in *Insets, path string) error {
	if in == nil {
		return nil
	}
	if in.Top < 0 || in.Right < 0 || in.Bottom < 0 || in.Left < 0 {
		return fmt.Errorf("%s cannot contain negative values", path)
	}
	if in.Left+in.Right >= 196 {
		return fmt.Errorf("%s left+right must be less than 196", path)
	}
	if in.Top+in.Bottom >= 196 {
		return fmt.Errorf("%s top+bottom must be less than 196", path)
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
		if err := validateInsets(btn.IconMargins, fmt.Sprintf("%s[%d].icon_margins", path, i)); err != nil {
			return err
		}
		modeCount := 0
		if btn.On != nil {
			modeCount++
		}
		if btn.Folder != nil {
			modeCount++
		}
		if btn.Widget != nil {
			modeCount++
		}
		if modeCount > 1 {
			return fmt.Errorf("%s[%d] cannot combine on, folder, and widget", path, i)
		}
		if tr := btn.On; tr != nil {
			actionCount := 0
			toggleCount := 0
			if tr.Tap != nil {
				actionCount++
				if tg, err := validateGestureBinding(tr.Tap, fmt.Sprintf("%s[%d].on.tap", path, i)); err != nil {
					return err
				} else if tg {
					toggleCount++
				}
			}
			if tr.DoubleTap != nil {
				actionCount++
				if tg, err := validateGestureBinding(tr.DoubleTap, fmt.Sprintf("%s[%d].on.double_tap", path, i)); err != nil {
					return err
				} else if tg {
					toggleCount++
				}
			}
			if tr.Hold != nil {
				actionCount++
				if tg, err := validateGestureBinding(tr.Hold, fmt.Sprintf("%s[%d].on.hold", path, i)); err != nil {
					return err
				} else if tg {
					toggleCount++
				}
			}
			if tr.Repeat != nil {
				actionCount++
				if tg, err := validateGestureBinding(tr.Repeat, fmt.Sprintf("%s[%d].on.repeat", path, i)); err != nil {
					return err
				} else if tg {
					toggleCount++
				}
			}
			if actionCount == 0 {
				return fmt.Errorf("%s[%d].on has no actions", path, i)
			}
			if toggleCount > 1 {
				return fmt.Errorf("%s[%d].on can define toggle for only one gesture", path, i)
			}
			if tr.HoldMS != 0 && tr.HoldMS < 100 {
				return fmt.Errorf("%s[%d].on.hold_ms too small: %d", path, i, tr.HoldMS)
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
				if w.HoldMS != 0 && w.HoldMS < 100 {
					return fmt.Errorf("%s[%d].widget.hold_ms too small: %d", path, i, w.HoldMS)
				}
				if err := validateProjectWidgetOn(w.On, fmt.Sprintf("%s[%d].widget.on", path, i)); err != nil {
					return err
				}
			default:
				return fmt.Errorf("%s[%d].widget.type %q unknown (memory|spotify_play_pause|spotify_next|spotify_artwork|project)", path, i, w.Type)
			}
		}
		if d := btn.Folder; d != nil {
			if len(d.Buttons) == 0 {
				return fmt.Errorf("%s[%d].folder.buttons is empty", path, i)
			}
			childPath := fmt.Sprintf("%s[%d].folder.buttons", path, i)
			if err := validateButtons(d.Buttons, childPath, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func buttonsUseKeyActions(buttons []Button) bool {
	for i := range buttons {
		btn := buttons[i]
		if handlersUseKeyActions(btn.On) {
			return true
		}
		if btn.Folder != nil && buttonsUseKeyActions(btn.Folder.Buttons) {
			return true
		}
	}
	return false
}

func handlersUseKeyActions(h *Handlers) bool {
	if h == nil {
		return false
	}
	return gestureUsesKeyAction(h.Tap) ||
		gestureUsesKeyAction(h.DoubleTap) ||
		gestureUsesKeyAction(h.Hold) ||
		gestureUsesKeyAction(h.Repeat)
}

func gestureUsesKeyAction(g *GestureBinding) bool {
	if g == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(g.Type), "key") {
		return true
	}
	for i := range g.Toggle {
		if strings.EqualFold(strings.TrimSpace(g.Toggle[i].Action.Type), "key") {
			return true
		}
	}
	for i := range g.Chain {
		if g.Chain[i].Action != nil && strings.EqualFold(strings.TrimSpace(g.Chain[i].Action.Type), "key") {
			return true
		}
	}
	return false
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

func validateGestureBinding(g *GestureBinding, path string) (bool, error) {
	if g == nil {
		return false, nil
	}
	hasDirect := strings.TrimSpace(g.Type) != "" || strings.TrimSpace(g.Cmd) != "" || strings.TrimSpace(g.Key) != "" || strings.TrimSpace(g.URL) != ""
	if len(g.Toggle) > 0 {
		if hasDirect || len(g.Chain) > 0 {
			return false, fmt.Errorf("%s cannot combine direct action fields, toggle, and chain", path)
		}
		if len(g.Toggle) < 2 {
			return false, fmt.Errorf("%s.toggle must contain at least 2 steps", path)
		}
		for i := range g.Toggle {
			stepPath := fmt.Sprintf("%s.toggle[%d]", path, i)
			if err := validateAction(&g.Toggle[i].Action, stepPath+".action"); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	if len(g.Chain) > 0 {
		if hasDirect {
			return false, fmt.Errorf("%s cannot combine direct action fields, toggle, and chain", path)
		}
		for i := range g.Chain {
			step := g.Chain[i]
			stepPath := fmt.Sprintf("%s.chain[%d]", path, i)
			hasDelay := step.DelayMS > 0
			hasAction := step.Action != nil
			if !hasDelay && !hasAction {
				return false, fmt.Errorf("%s must define either delay_ms or action", stepPath)
			}
			if hasDelay && hasAction {
				return false, fmt.Errorf("%s cannot define both delay_ms and action", stepPath)
			}
			if step.DelayMS < 0 {
				return false, fmt.Errorf("%s.delay_ms cannot be negative", stepPath)
			}
			if hasDelay && step.DelayMS < 10 {
				return false, fmt.Errorf("%s.delay_ms too small: %d", stepPath, step.DelayMS)
			}
			if hasAction {
				if err := validateAction(step.Action, stepPath+".action"); err != nil {
					return false, err
				}
			}
		}
		return false, nil
	}
	action := g.Action()
	if action == nil {
		return false, fmt.Errorf("%s has no action", path)
	}
	if err := validateAction(action, path); err != nil {
		return false, err
	}
	return false, nil
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

func resolveButtonPaths(buttons []Button, baseDir string) {
	for i := range buttons {
		if buttons[i].Icon != "" && !filepath.IsAbs(buttons[i].Icon) && !strings.HasPrefix(buttons[i].Icon, "~/") {
			if _, err := os.Stat(buttons[i].Icon); err != nil {
				buttons[i].Icon = filepath.Join(baseDir, buttons[i].Icon)
			}
		}
		resolveTogglePaths(buttons[i].On, baseDir)
		if buttons[i].Widget != nil && buttons[i].Widget.ProjectDir != "" &&
			!filepath.IsAbs(buttons[i].Widget.ProjectDir) && !strings.HasPrefix(buttons[i].Widget.ProjectDir, "~/") {
			buttons[i].Widget.ProjectDir = filepath.Join(baseDir, buttons[i].Widget.ProjectDir)
		}
		if buttons[i].Folder != nil {
			resolveButtonPaths(buttons[i].Folder.Buttons, baseDir)
		}
	}
}

func applyButtonDefaults(buttons []Button, iconMargins *Insets) {
	for i := range buttons {
		if buttons[i].IconMargins == nil && iconMargins != nil {
			buttons[i].IconMargins = cloneInsets(iconMargins)
		}
		if buttons[i].Folder != nil {
			applyButtonDefaults(buttons[i].Folder.Buttons, iconMargins)
		}
	}
}

func cloneInsets(in *Insets) *Insets {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func assignButtonIDs(buttons []Button, path string) {
	for i := range buttons {
		buttons[i].RuntimeID = fmt.Sprintf("%s[%d]", path, i)
		if buttons[i].Folder != nil {
			assignButtonIDs(buttons[i].Folder.Buttons, buttons[i].RuntimeID+".folder.buttons")
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
		collectToggleIconPaths(btn.On, seen, out)
		if btn.Folder != nil {
			collectIconPaths(btn.Folder.Buttons, seen, out)
		}
	}
}

func resolveTogglePaths(on *Handlers, baseDir string) {
	if on == nil {
		return
	}
	resolveGestureBindingPaths(on.Tap, baseDir)
	resolveGestureBindingPaths(on.DoubleTap, baseDir)
	resolveGestureBindingPaths(on.Hold, baseDir)
	resolveGestureBindingPaths(on.Repeat, baseDir)
}

func resolveGestureBindingPaths(g *GestureBinding, baseDir string) {
	if g == nil {
		return
	}
	for i := range g.Toggle {
		icon := g.Toggle[i].Icon
		if icon == "" || filepath.IsAbs(icon) || strings.HasPrefix(icon, "~/") {
			continue
		}
		if _, err := os.Stat(icon); err != nil {
			g.Toggle[i].Icon = filepath.Join(baseDir, icon)
		}
	}
}

func collectToggleIconPaths(on *Handlers, seen map[string]struct{}, out *[]string) {
	if on == nil {
		return
	}
	collectGestureBindingIconPaths(on.Tap, seen, out)
	collectGestureBindingIconPaths(on.DoubleTap, seen, out)
	collectGestureBindingIconPaths(on.Hold, seen, out)
	collectGestureBindingIconPaths(on.Repeat, seen, out)
}

func collectGestureBindingIconPaths(g *GestureBinding, seen map[string]struct{}, out *[]string) {
	if g == nil {
		return
	}
	for _, step := range g.Toggle {
		if step.Icon == "" {
			continue
		}
		if _, ok := seen[step.Icon]; ok {
			continue
		}
		seen[step.Icon] = struct{}{}
		*out = append(*out, step.Icon)
	}
}
