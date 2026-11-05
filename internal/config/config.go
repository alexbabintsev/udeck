package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
	"gopkg.in/yaml.v3"
)

// Config is the top-level schema.
type Config struct {
	Device    DeviceConfig    `yaml:"device"`
	Widgets   WidgetSettings  `yaml:"widgets,omitempty"`
	Pages     []Page          `yaml:"pages,omitempty"`
	Buttons   []Button        `yaml:"buttons,omitempty"`
	SmallView *SmallViewBlock `yaml:"smallview,omitempty"` // reserved for v3
}

type Page struct {
	Name    string   `yaml:"name,omitempty"`
	Buttons []Button `yaml:"buttons,omitempty"`
}

type WidgetSettings struct {
	// Font is an optional TTF/OTF path (~ expanded) for widget text; the
	// built-in Go Mono family is used when empty.
	Font    string                `yaml:"font,omitempty"`
	Project ProjectWidgetSettings `yaml:"project,omitempty"`
}

type ProjectWidgetSettings struct {
	Terminal     string `yaml:"terminal,omitempty"`
	Editor       string `yaml:"editor,omitempty"`
	EditorSubdir string `yaml:"editor_subdir,omitempty"`
}

type DeviceConfig struct {
	Brightness *int `yaml:"brightness,omitempty"`
	// ImageFormat of uploaded key images: "auto" (default; WebP when the
	// firmware is known to support it), "webp" or "jpeg".
	ImageFormat string `yaml:"image_format,omitempty"`
	// ImageQuality (1-100) of uploaded key images; 0 means the default.
	ImageQuality int         `yaml:"image_quality,omitempty"`
	LabelStyle   *LabelStyle `yaml:"label_style,omitempty"`
	IconMargins  *Insets     `yaml:"icon_margins,omitempty"`
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
	// At is [col, row] internally. YAML config is normalized from
	// user-facing [row, col] during Load().
	At            [2]int    `yaml:"at"`
	Label         string    `yaml:"label,omitempty"`
	Icon          string    `yaml:"icon,omitempty"` // path, ~ expanded
	IconColor     string    `yaml:"icon_color,omitempty"`
	IconMargins   *Insets   `yaml:"icon_margins,omitempty"`
	On            *Handlers `yaml:"on,omitempty"`
	Folder        *Folder   `yaml:"folder,omitempty"`
	Widget        *Widget   `yaml:"widget,omitempty"`
	RuntimeID     string    `yaml:"-"`
	SyntheticBack bool      `yaml:"-"`
}

type ToggleStep struct {
	Label     string `yaml:"label,omitempty"`
	Icon      string `yaml:"icon,omitempty"`
	IconColor string `yaml:"icon_color,omitempty"`
	Action    Action `yaml:"action"`
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
	Title        string        `yaml:"title,omitempty"`
	City         string        `yaml:"city,omitempty"`
	DSNs         string        `yaml:"dsns,omitempty"`
	ProjectDir   string        `yaml:"project_dir,omitempty"`
	Session      string        `yaml:"session,omitempty"`
	URL          string        `yaml:"url,omitempty"`
	Token        string        `yaml:"token,omitempty"`
	EditorSubdir string        `yaml:"editor_subdir,omitempty"`
	Scripts      WidgetScripts `yaml:"scripts,omitempty"`
	On           *WidgetOn     `yaml:"on,omitempty"`
	HoldMS       int           `yaml:"hold_ms,omitempty"`
	DayIndex     int           `yaml:"day_index,omitempty"`
	// Action is what a press does: volume (mute|up|down) and media
	// (play_pause|next|previous).
	Action string `yaml:"action,omitempty"`
	// Step is the volume change per press in percent (volume up/down).
	Step int `yaml:"step,omitempty"`
	// Devices are the output devices audio_output cycles through, by
	// (partial, case-insensitive) name; empty means all of them.
	Devices []string `yaml:"devices,omitempty"`
	// Timezone is the IANA time zone of a clock, e.g. "Europe/Kyiv";
	// empty means local time.
	Timezone string `yaml:"timezone,omitempty"`
	// Hour12 shows a clock in 12-hour format.
	Hour12 bool `yaml:"hour12,omitempty"`
	// Duration is a timer's length as a Go duration ("5m", "1h30m").
	Duration string `yaml:"duration,omitempty"`
	// Sound is the macOS system sound a timer or pomodoro plays when it
	// runs out ("Glass", "Ping", ...), or "none".
	Sound string `yaml:"sound,omitempty"`
	// Work, Break and LongBreak are the pomodoro phases (Go durations);
	// Rounds is the number of work phases before the long break.
	Work      string `yaml:"work,omitempty"`
	Break     string `yaml:"break,omitempty"`
	LongBreak string `yaml:"long_break,omitempty"`
	Rounds    int    `yaml:"rounds,omitempty"`
	// Path is the volume a disk widget shows (any path on it); default /.
	Path string `yaml:"path,omitempty"`
	// Interface lists the network interface name prefixes to count,
	// comma-separated ("en0", "en,bridge"); default en (Wi-Fi, Ethernet).
	Interface string `yaml:"interface,omitempty"`
	// Symbol is what a ticker follows, as its source spells it: AAPL,
	// ^GSPC, EURUSD=X, BTC-USD (yahoo) or BTCUSDT (binance).
	Symbol string `yaml:"symbol,omitempty"`
	// Source is where a ticker gets prices: yahoo (default) or binance.
	Source string `yaml:"source,omitempty"`
}

// TimerDuration parses a timer or pomodoro duration field; empty means
// def. Durations run from one second to a day.
func TimerDuration(s string, def time.Duration) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration (use e.g. 90s, 5m, 1h30m)", s)
	}
	if d < time.Second || d > 24*time.Hour {
		return 0, fmt.Errorf("%q must be between 1s and 24h", s)
	}
	return d, nil
}

// Timer and pomodoro defaults.
const (
	DefaultTimerDuration  = 5 * time.Minute
	DefaultPomodoroWork   = 25 * time.Minute
	DefaultPomodoroBreak  = 5 * time.Minute
	DefaultPomodoroLong   = 15 * time.Minute
	DefaultPomodoroRounds = 4
	DefaultTimerSound     = "Glass"
)

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
	To     string       `yaml:"to,omitempty"`
	Toggle []ToggleStep `yaml:"toggle,omitempty"`
	Chain  []ChainStep  `yaml:"chain,omitempty"`
}

// HIDIndex converts internal [col, row] to the flat button index the device uses.
func (b Button) HIDIndex() int {
	if b.At[1] < 2 {
		return b.At[1]*5 + b.At[0]
	}
	if b.At == [2]int{3, 2} {
		return 13
	}
	return 10 + b.At[0]
}

// Action is the handler fired when a gesture resolves.
type Action struct {
	// Type is "shell", "key", "http", "window", "page_next", "page_prev",
	// or "close_all_projects".
	Type string `yaml:"type"`

	// Cmd is the shell command run for type=shell.
	Cmd string `yaml:"cmd,omitempty"`

	// Key is a key name for type=key, e.g. "return", "space", "f5".
	// Modifiers are space-separated prefixes: "cmd shift s".
	Key string `yaml:"key,omitempty"`

	// URL is the endpoint called for type=http.
	URL string `yaml:"url,omitempty"`

	// To is where type=window puts the focused window (WindowPositions).
	To string `yaml:"to,omitempty"`
}

// WindowPositions are the values of `to` for type=window.
var WindowPositions = []string{
	"left_half", "right_half", "top_half", "bottom_half",
	"top_left", "top_right", "bottom_left", "bottom_right",
	"left_third", "center_third", "right_third", "left_two_thirds", "right_two_thirds",
	"maximize", "center", "next_display", "previous_display", "restore",
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
		To:   g.To,
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
	collectPageIconPaths(c.PagesOrDefault(), seen, &out)
	return out
}

// UsesKeyActions reports whether any button in the config tree triggers a
// `type: key` action, directly, through a toggle step, or through a chain,
// or is a media widget: all of them need Accessibility access.
func (c *Config) UsesKeyActions() bool {
	return pagesUseKeyActions(c.PagesOrDefault())
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
	normalizePageButtonCoordinates(c.PagesOrDefault())
	assignPageButtonIDs(c.PagesOrDefault())
	resolvePageButtonPaths(c.PagesOrDefault(), filepath.Dir(path))
	applyPageButtonDefaults(c.PagesOrDefault(), c.Device.IconMargins)
	if err := c.Validate(); err != nil {
		return &c, err
	}
	return &c, nil
}

// WritableByOthers reports whether the file at path can be modified by
// users other than its owner. The config runs shell commands and is
// hot-reloaded, so such a file lets them run code as this user.
func WritableByOthers(path string) (os.FileMode, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	mode := info.Mode().Perm()
	return mode, mode&0o022 != 0
}

func (c *Config) Validate() error {
	if len(c.Pages) > 0 && len(c.Buttons) > 0 {
		return fmt.Errorf("config cannot define both pages and buttons")
	}
	if q := c.Device.ImageQuality; q < 0 || q > 100 {
		return fmt.Errorf("device.image_quality out of range: %d (1-100)", q)
	}
	switch c.Device.ImageFormat {
	case "", "auto", "webp", "jpeg":
	default:
		return fmt.Errorf("device.image_format %q unknown (auto|webp|jpeg)", c.Device.ImageFormat)
	}
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
	pages := c.PagesOrDefault()
	if len(pages) == 0 {
		return fmt.Errorf("config must define at least one page")
	}
	return validatePages(pages)
}

func (c *Config) PagesOrDefault() []Page {
	if len(c.Pages) > 0 {
		return c.Pages
	}
	return []Page{{
		Name:    "Main",
		Buttons: c.Buttons,
	}}
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
	return validateInsetsForSize(in, path, 196, 196)
}

func validateInsetsForSize(in *Insets, path string, width, height int) error {
	if in == nil {
		return nil
	}
	if in.Top < 0 || in.Right < 0 || in.Bottom < 0 || in.Left < 0 {
		return fmt.Errorf("%s cannot contain negative values", path)
	}
	if in.Left+in.Right >= width {
		return fmt.Errorf("%s left+right must be less than %d", path, width)
	}
	if in.Top+in.Bottom >= height {
		return fmt.Errorf("%s top+bottom must be less than %d", path, height)
	}
	return nil
}

func validatePages(pages []Page) error {
	for i, page := range pages {
		if len(page.Buttons) == 0 {
			return fmt.Errorf("pages[%d].buttons is empty", i)
		}
		if err := validateButtons(page.Buttons, fmt.Sprintf("pages[%d].buttons", i), false); err != nil {
			return err
		}
	}
	return nil
}

func validateButtons(buttons []Button, path string, reserveBack bool) error {
	seen := make(map[[2]int]int, len(buttons))
	for i, btn := range buttons {
		if !validButtonSlot(btn.At) {
			return fmt.Errorf("%s[%d].at out of range: %v", path, i, btn.At)
		}
		if reserveBack && btn.At == [2]int{3, 2} {
			return fmt.Errorf("%s[%d].at conflicts with reserved Back button slot: %v", path, i, btn.At)
		}
		if prev, ok := seen[btn.At]; ok {
			return fmt.Errorf("%s[%d].at duplicates %s[%d].at: %v", path, i, path, prev, btn.At)
		}
		seen[btn.At] = i
		width, height := buttonDimensions(btn.At)
		if err := validateInsetsForSize(btn.IconMargins, fmt.Sprintf("%s[%d].icon_margins", path, i), width, height); err != nil {
			return err
		}
		if btn.IconColor != "" {
			if err := validateHexColor(btn.IconColor); err != nil {
				return fmt.Errorf("%s[%d].icon_color: %w", path, i, err)
			}
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
		if err := validateHandlers(btn.On, fmt.Sprintf("%s[%d].on", path, i)); err != nil {
			return err
		}
		if err := validateWidget(btn.Widget, btn.At, fmt.Sprintf("%s[%d].widget", path, i)); err != nil {
			return err
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

func validateHandlers(tr *Handlers, path string) error {
	if tr == nil {
		return nil
	}
	gestures := []struct {
		name    string
		binding *GestureBinding
	}{
		{"tap", tr.Tap}, {"double_tap", tr.DoubleTap}, {"hold", tr.Hold}, {"repeat", tr.Repeat},
	}
	actionCount, toggleCount := 0, 0
	for _, g := range gestures {
		if g.binding == nil {
			continue
		}
		actionCount++
		isToggle, err := validateGestureBinding(g.binding, path+"."+g.name)
		if err != nil {
			return err
		}
		if isToggle {
			toggleCount++
		}
	}
	switch {
	case actionCount == 0:
		return fmt.Errorf("%s has no actions", path)
	case toggleCount > 1:
		return fmt.Errorf("%s can define toggle for only one gesture", path)
	case tr.HoldMS != 0 && tr.HoldMS < 100:
		return fmt.Errorf("%s.hold_ms too small: %d", path, tr.HoldMS)
	case tr.DoubleTapMS != 0 && tr.DoubleTapMS < 100:
		return fmt.Errorf("%s.double_tap_ms too small: %d", path, tr.DoubleTapMS)
	case tr.RepeatEveryMS != 0 && tr.RepeatEveryMS < 50:
		return fmt.Errorf("%s.repeat_every_ms too small: %d", path, tr.RepeatEveryMS)
	}
	return nil
}

// widgetMinPollMS is the smallest poll_ms each widget type accepts.
var widgetMinPollMS = map[string]int{
	"memory":             1000,
	"spotify_play_pause": 1000,
	"spotify_next":       1000,
	"spotify_artwork":    1000,
	"wide_dashboard":     1000,
	"freqtrade":          3000,
	"site":               30000,
	"weather_day":        1000,
	"project":            1000,
	"volume":             200,
	"microphone":         200,
	"audio_output":       500,
	"media":              1000,
	"clock":              1000,
	"timer":              1000,
	"stopwatch":          1000,
	"pomodoro":           1000,
	"cpu":                500,
	"network":            500,
	"disk":               5000,
	"battery":            5000,
	"ticker":             15000,
}

func validateWidget(w *Widget, at [2]int, path string) error {
	if w == nil {
		return nil
	}
	minPoll, known := widgetMinPollMS[w.Type]
	if !known {
		return fmt.Errorf("%s.type %q unknown (clock|timer|stopwatch|pomodoro|cpu|disk|network|battery|ticker|volume|microphone|audio_output|media|memory|spotify_play_pause|spotify_next|spotify_artwork|wide_dashboard|weather_day|freqtrade|site|project)", path, w.Type)
	}
	switch w.Type {
	case "wide_dashboard":
		if at != [2]int{3, 2} {
			return fmt.Errorf("%s.type %q requires wide slot at [2, 3] in YAML", path, w.Type)
		}
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
	case "weather_day":
		if strings.TrimSpace(w.City) == "" {
			return fmt.Errorf("%s.city is empty", path)
		}
		if w.DayIndex < 0 || w.DayIndex > 13 {
			return fmt.Errorf("%s.day_index must be 0-13, got %d", path, w.DayIndex)
		}
	case "volume":
		switch w.Action {
		case "", "mute", "up", "down":
		default:
			return fmt.Errorf("%s.action %q unknown (mute|up|down)", path, w.Action)
		}
		if w.Step < 0 || w.Step > 50 {
			return fmt.Errorf("%s.step must be 1-50, got %d", path, w.Step)
		}
	case "clock":
		if tz := strings.TrimSpace(w.Timezone); tz != "" {
			if _, err := time.LoadLocation(tz); err != nil {
				return fmt.Errorf("%s.timezone %q unknown (use an IANA name such as Europe/Kyiv)", path, tz)
			}
		}
	case "timer", "pomodoro":
		fields := map[string]string{"duration": w.Duration}
		if w.Type == "pomodoro" {
			fields = map[string]string{"work": w.Work, "break": w.Break, "long_break": w.LongBreak}
			if w.Rounds < 0 || w.Rounds > 12 {
				return fmt.Errorf("%s.rounds must be 1-12, got %d", path, w.Rounds)
			}
		}
		for name, v := range fields {
			if _, err := TimerDuration(v, time.Second); err != nil {
				return fmt.Errorf("%s.%s: %w", path, name, err)
			}
		}
		if !validSoundName(w.Sound) {
			return fmt.Errorf("%s.sound %q must be a system sound name such as Glass, or none", path, w.Sound)
		}
	case "ticker":
		if strings.TrimSpace(w.Symbol) == "" {
			return fmt.Errorf("%s.symbol is empty", path)
		}
		switch strings.TrimSpace(w.Source) {
		case "", "yahoo", "binance":
		default:
			return fmt.Errorf("%s.source %q unknown (yahoo|binance)", path, w.Source)
		}
	case "media":
		switch w.Action {
		case "play_pause", "next", "previous":
		case "":
			return fmt.Errorf("%s.action is empty (play_pause|next|previous)", path)
		default:
			return fmt.Errorf("%s.action %q unknown (play_pause|next|previous)", path, w.Action)
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
	if w.PollMS != 0 && w.PollMS < minPoll {
		return fmt.Errorf("%s.poll_ms too small: %d", path, w.PollMS)
	}
	return nil
}

// validSoundName accepts "", "none" and plain names of the sounds in
// /System/Library/Sounds: letters only, so the name cannot form a path.
func validSoundName(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return len(s) <= 32
}

func buttonDimensions(at [2]int) (width, height int) {
	if at == [2]int{3, 2} {
		return 458, 196
	}
	return 196, 196
}

func validButtonSlot(at [2]int) bool {
	if at[0] < 0 || at[0] > 4 || at[1] < 0 || at[1] > 2 {
		return false
	}
	return at != [2]int{4, 2}
}

func pagesUseKeyActions(pages []Page) bool {
	for i := range pages {
		if buttonsUseKeyActions(pages[i].Buttons) {
			return true
		}
	}
	return false
}

func buttonsUseKeyActions(buttons []Button) bool {
	for i := range buttons {
		btn := buttons[i]
		if handlersUseKeyActions(btn.On) {
			return true
		}
		// Media keys are posted like key presses and need the same
		// Accessibility permission.
		if btn.Widget != nil && btn.Widget.Type == "media" {
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

// gestureUsesKeyAction reports actions that need Accessibility access:
// key presses and window moves.
func gestureUsesKeyAction(g *GestureBinding) bool {
	if g == nil {
		return false
	}
	needsAX := func(typ string) bool {
		typ = strings.ToLower(strings.TrimSpace(typ))
		return typ == "key" || typ == "window"
	}
	if needsAX(g.Type) {
		return true
	}
	for i := range g.Toggle {
		if needsAX(g.Toggle[i].Action.Type) {
			return true
		}
	}
	for i := range g.Chain {
		if g.Chain[i].Action != nil && needsAX(g.Chain[i].Action.Type) {
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
	case "window":
		if !slices.Contains(WindowPositions, a.To) {
			return fmt.Errorf("%s.to %q unknown (%s)", path, a.To, strings.Join(WindowPositions, "|"))
		}
	case "page_next", "page_prev", "close_all_projects":
	default:
		return fmt.Errorf("%s.type %q unknown (shell|key|http|window|page_next|page_prev|close_all_projects)", path, a.Type)
	}
	return nil
}

func validateGestureBinding(g *GestureBinding, path string) (bool, error) {
	if g == nil {
		return false, nil
	}
	hasDirect := strings.TrimSpace(g.Type) != "" || strings.TrimSpace(g.Cmd) != "" || strings.TrimSpace(g.Key) != "" || strings.TrimSpace(g.URL) != "" || strings.TrimSpace(g.To) != ""
	if len(g.Toggle) > 0 {
		if hasDirect || len(g.Chain) > 0 {
			return false, fmt.Errorf("%s cannot combine direct action fields, toggle, and chain", path)
		}
		if len(g.Toggle) < 2 {
			return false, fmt.Errorf("%s.toggle must contain at least 2 steps", path)
		}
		for i := range g.Toggle {
			stepPath := fmt.Sprintf("%s.toggle[%d]", path, i)
			if g.Toggle[i].IconColor != "" {
				if err := validateHexColor(g.Toggle[i].IconColor); err != nil {
					return false, fmt.Errorf("%s.icon_color: %w", stepPath, err)
				}
			}
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

func resolvePageButtonPaths(pages []Page, baseDir string) {
	for i := range pages {
		resolveButtonPaths(pages[i].Buttons, baseDir)
	}
}

func normalizePageButtonCoordinates(pages []Page) {
	for i := range pages {
		normalizeButtonCoordinates(pages[i].Buttons)
	}
}

func normalizeButtonCoordinates(buttons []Button) {
	for i := range buttons {
		buttons[i].At = [2]int{buttons[i].At[1], buttons[i].At[0]}
		if buttons[i].Folder != nil {
			normalizeButtonCoordinates(buttons[i].Folder.Buttons)
		}
	}
}

func resolveButtonPaths(buttons []Button, baseDir string) {
	for i := range buttons {
		if buttons[i].Icon != "" && !icons.IsPackIcon(buttons[i].Icon) && !filepath.IsAbs(buttons[i].Icon) && !strings.HasPrefix(buttons[i].Icon, "~/") {
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

func applyPageButtonDefaults(pages []Page, iconMargins *Insets) {
	for i := range pages {
		applyButtonDefaults(pages[i].Buttons, iconMargins)
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

func assignPageButtonIDs(pages []Page) {
	for i := range pages {
		assignButtonIDs(pages[i].Buttons, fmt.Sprintf("pages[%d].buttons", i))
	}
}

func assignButtonIDs(buttons []Button, path string) {
	for i := range buttons {
		buttons[i].RuntimeID = fmt.Sprintf("%s[%d]", path, i)
		if buttons[i].Folder != nil {
			assignButtonIDs(buttons[i].Folder.Buttons, buttons[i].RuntimeID+".folder.buttons")
		}
	}
}

func collectPageIconPaths(pages []Page, seen map[string]struct{}, out *[]string) {
	for i := range pages {
		collectIconPaths(pages[i].Buttons, seen, out)
	}
}

func collectIconPaths(buttons []Button, seen map[string]struct{}, out *[]string) {
	for _, btn := range buttons {
		if btn.Icon != "" && !icons.IsPackIcon(btn.Icon) {
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
		if icon == "" || icons.IsPackIcon(icon) || filepath.IsAbs(icon) || strings.HasPrefix(icon, "~/") {
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
		if step.Icon == "" || icons.IsPackIcon(step.Icon) {
			continue
		}
		if _, ok := seen[step.Icon]; ok {
			continue
		}
		seen[step.Icon] = struct{}{}
		*out = append(*out, step.Icon)
	}
}
