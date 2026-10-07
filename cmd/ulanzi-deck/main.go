package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/actions"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/device"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/preflight"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/widgets"
)

const defaultConfigPath = "examples/test-config.yaml"

const usage = `ulanzi-deck — minimal headless daemon for the Ulanzi D200

Usage:
  ulanzi-deck info
  ulanzi-deck dump-events [--debug]
  ulanzi-deck set-brightness <0..100>
  ulanzi-deck run [--config <path.yaml>] [--debug]

--debug (or ULANZI_DEBUG=1) logs every HID error, retry, reconnect and
page upload, plus widget fetch failures, for diagnosing device drop-outs.

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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *debugFlag {
		debug.SetEnabled(true)
	}
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *debugFlag {
		debug.SetEnabled(true)
	}
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

	s, err := newRunSession(*cfgPath, cfg, dev)
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
func newRunSession(cfgPath string, cfg *config.Config, dev deck) (*session, error) {
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
	}
	if err := s.refreshPage(); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}
