package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/actions"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/device"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/preflight"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/widgets"
)

const defaultConfigPath = "examples/test-config.yaml"

const usage = `ulanzi-deck — minimal headless daemon for the Ulanzi D200

Usage:
  ulanzi-deck info
  ulanzi-deck dump-events [--debug] [--log-file <path>]
  ulanzi-deck set-brightness <0..100>
  ulanzi-deck run [--config <path.yaml>] [--debug] [--log-file <path>]
  ulanzi-deck probe [--key N] [--sizes 3,8,14] [--gaps 2000,...,0] [--reps 3] [--read]
  ulanzi-deck probe --playpause N [--key N]
  ulanzi-deck probe --recovery

--debug (or ULANZI_DEBUG=1) logs every HID error, retry, reconnect and
page upload, plus widget fetch failures, for diagnosing device drop-outs.
--log-file also writes the log to a file (rotated at 10 MB). With
--debug it defaults to ~/Library/Logs/ulanzi-deck/debug.log.

Note: Ulanzi Studio holds an exclusive HID lock. Quit it before running:
  osascript -e 'tell application "Ulanzi Studio" to quit'

If --config is omitted for run, the default is:
  ` + defaultConfigPath + `
`

// External dependencies, replaced by tests.
var (
	openDevice = func() (deck, error) {
		dev, err := device.Open()
		if err != nil {
			return nil, err
		}
		return dev, nil
	}
	studioRunning         = preflight.StudioRunning
	requestKeyPermissions = actions.RequestKeyPermissions
	startPowerWatcher     = newPowerWatcher
	signalContext         = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	}
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.SetOutput(os.Stderr)
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

// runCLI dispatches a subcommand and returns the process exit code.
func runCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	sub, rest := args[0], args[1:]

	var err error
	switch sub {
	case "info":
		err = cmdInfo(stdout)
	case "dump-events":
		err = cmdDumpEvents(rest, stdout, stderr)
	case "set-brightness":
		err = cmdSetBrightness(rest, stdout, stderr)
	case "run":
		err = cmdRun(rest, stdout, stderr)
	case "probe":
		err = cmdProbe(rest, stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown subcommand: %s\n\n%s", sub, usage)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// infoReader is implemented by *device.Device.
type infoReader interface {
	Info(timeout time.Duration) (string, error)
}

func openChecked() (deck, error) {
	if err := studioRunning(); err != nil {
		return nil, err
	}
	return openDevice()
}

func cmdInfo(stdout io.Writer) error {
	dev, err := openChecked()
	if err != nil {
		return err
	}
	defer dev.Close()

	r, ok := dev.(infoReader)
	if !ok {
		return errors.New("device does not support info")
	}
	info, err := r.Info(2 * time.Second)
	if err != nil {
		fmt.Fprintf(stdout, "(no IN_DEVICE_INFO frame: %v)\n", err)
		return nil
	}
	fmt.Fprintf(stdout, "device_info: %s\n", info)
	return nil
}

func cmdDumpEvents(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("dump-events", flag.ContinueOnError)
	fs.SetOutput(stderr)
	debugFlag := fs.Bool("debug", false, "verbose diagnostics")
	logFile := fs.String("log-file", "", "also write the log to this file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	closeLog, err := setupLogging(*debugFlag, *logFile, stderr)
	if err != nil {
		return err
	}
	defer closeLog()
	dev, err := openChecked()
	if err != nil {
		return err
	}
	defer dev.Close()

	// An empty page switches the device into host mode so it reports
	// button events.
	if err := sendPage(dev, nil); err != nil {
		return err
	}

	ctx, stop := signalContext()
	defer stop()

	log.Println("listening for events. Ctrl-C to exit.")
	return (&session{dev: dev, out: stdout}).run(ctx)
}

func cmdSetBrightness(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("set-brightness", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: ulanzi-deck set-brightness <0..100>")
	}
	level, err := strconv.Atoi(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("invalid brightness: %w", err)
	}
	pkt, err := protocol.SetBrightnessPacket(level)
	if err != nil {
		return err
	}
	dev, err := openChecked()
	if err != nil {
		return err
	}
	defer dev.Close()

	if err := dev.SendPacket(pkt); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "brightness set to %d\n", level)
	return nil
}

func cmdRun(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("config", defaultConfigPath, "path to YAML config")
	debugFlag := fs.Bool("debug", false, "verbose diagnostics")
	logFile := fs.String("log-file", "", "also write the log to this file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	closeLog, err := setupLogging(*debugFlag, *logFile, stderr)
	if err != nil {
		return err
	}
	defer closeLog()
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if cfg.UsesKeyActions() {
		requestKeyPermissions()
	}
	dev, err := openChecked()
	if err != nil {
		return err
	}
	defer dev.Close()

	firmware := ""
	if r, ok := dev.(infoReader); ok {
		if info, err := r.Info(time.Second); err == nil {
			log.Printf("device: %s", compactJSON(info))
			firmware = firmwareVersion(info)
		}
	}
	s, err := newRunSession(*cfgPath, cfg, dev, firmware)
	if err != nil {
		return err
	}
	s.out = stdout
	defer s.close()

	if cfg.Device.Brightness != nil {
		fmt.Fprintf(stdout, "applied brightness=%d\n", *cfg.Device.Brightness)
	}

	ctx, stop := signalContext()
	defer stop()
	if pw, err := startPowerWatcher(ctx); err == nil && pw != nil {
		s.power = pw.Events()
	}

	log.Println("running. Ctrl-C to exit.")
	return s.run(ctx)
}

// newRunSession builds the runtime state for cfg and uploads the initial
// page. On error nothing is left running.
func newRunSession(cfgPath string, cfg *config.Config, dev deck, firmware string) (*session, error) {
	applyImageSettings(cfg, firmware)
	nav := newNavigator(cfg.PagesOrDefault())
	toggles := newToggleManager(cfg)
	widgetMgr, err := widgets.NewManager(cfg)
	if err != nil {
		return nil, fmt.Errorf("widgets: %w", err)
	}
	s := &session{
		cfgPath:  cfgPath,
		cfg:      cfg,
		dev:      dev,
		nav:      nav,
		gestures: newGestureManager(),
		toggles:  toggles,
		widgets:  widgetMgr,
		actions:  newActionRunner(),
		tx:       newOutbox(dev),
		firmware: firmware,
	}
	if err := s.refreshPage(); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

// maxLogFileSize is the size at which the log file is rotated to
// <path>.1 when the daemon starts.
const maxLogFileSize = 10 << 20

// defaultDebugLogPath is used for --debug without --log-file.
var defaultDebugLogPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Logs", "ulanzi-deck", "debug.log")
}

// setupLogging enables debug output and mirrors the standard logger into
// a file when requested. The returned func restores the logger.
func setupLogging(debugOn bool, path string, stderr io.Writer) (func(), error) {
	if debugOn {
		debug.SetEnabled(true)
		if path == "" {
			path = defaultDebugLogPath()
		}
	}
	if path == "" {
		return func() {}, nil
	}
	f, err := openLogFile(path)
	if err != nil {
		return nil, fmt.Errorf("log file: %w", err)
	}
	prev := log.Writer()
	log.SetOutput(io.MultiWriter(stderr, f))
	log.Printf("---- ulanzi-deck started (pid %d), logging to %s", os.Getpid(), path)
	return func() {
		log.SetOutput(prev)
		_ = f.Close()
	}, nil
}

// openLogFile appends to path (owner-only: logs can contain URLs),
// rotating it first when it grew past maxLogFileSize.
func openLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogFileSize {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// webpMinFirmware is the oldest firmware verified to render WebP keys
// (5.3.1 runs an older, Qt-based key app without WebP support).
const webpMinFirmware = "5.3.6"

// applyImageSettings sets the key image format and quality from the
// config and the firmware version the device reported ("" if unknown).
func applyImageSettings(cfg *config.Config, firmware string) {
	icons.SetQuality(cfg.Device.ImageQuality)
	f := imageFormatFor(cfg.Device.ImageFormat, firmware)
	icons.SetFormat(f)
	debug.Logf("key images: %s, quality %d (image_format %q, firmware %q)", f, icons.Quality(), cfg.Device.ImageFormat, firmware)
}

func imageFormatFor(setting, firmware string) icons.Format {
	switch setting {
	case "webp":
		return icons.WebP
	case "jpeg":
		return icons.JPEG
	}
	if versionAtLeast(firmware, webpMinFirmware) {
		return icons.WebP
	}
	return icons.JPEG
}

// firmwareVersion extracts "Dversion" from the device-info JSON.
func firmwareVersion(info string) string {
	var v struct {
		Dversion string
	}
	if err := json.Unmarshal([]byte(info), &v); err != nil {
		return ""
	}
	return strings.TrimSpace(v.Dversion)
}

// versionAtLeast compares dotted numeric versions; an unparsable v is
// never at least min.
func versionAtLeast(v, min string) bool {
	parse := func(s string) ([]int, bool) {
		if s == "" {
			return nil, false
		}
		var out []int
		for _, p := range strings.Split(s, ".") {
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil, false
			}
			out = append(out, n)
		}
		return out, true
	}
	a, ok1 := parse(v)
	b, ok2 := parse(min)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < max(len(a), len(b)); i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y
		}
	}
	return true
}
