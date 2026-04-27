package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/actions"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/device"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/preflight"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
)

const usage = `ulanzi-deck — minimal headless daemon for the Ulanzi D200

Usage:
  ulanzi-deck info
  ulanzi-deck dump-events
  ulanzi-deck set-brightness <0..100>
  ulanzi-deck run --config <path.yaml>

Note: Ulanzi Studio holds an exclusive HID lock. Quit it before running:
  osascript -e 'tell application "Ulanzi Studio" to quit'
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	sub := os.Args[1]
	args := os.Args[2:]

	var err error
	switch sub {
	case "info":
		err = cmdInfo()
	case "dump-events":
		err = cmdDumpEvents()
	case "set-brightness":
		err = cmdSetBrightness(args)
	case "run":
		err = cmdRun(args)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n%s", sub, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func cmdInfo() error {
	if err := preflight.StudioRunning(); err != nil {
		return err
	}
	dev, err := device.Open()
	if err != nil {
		return err
	}
	defer dev.Close()

	info, err := dev.Info(2 * time.Second)
	if err != nil {
		fmt.Printf("(no IN_DEVICE_INFO frame: %v)\n", err)
		return nil
	}
	fmt.Printf("device_info: %s\n", info)
	return nil
}

func cmdDumpEvents() error {
	if err := preflight.StudioRunning(); err != nil {
		return err
	}
	dev, err := device.Open()
	if err != nil {
		return err
	}
	defer dev.Close()

	if err := initDevice(dev, nil); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Println("listening for events. Ctrl-C to exit.")
	return readLoop(ctx, dev, nil)
}

func cmdSetBrightness(args []string) error {
	fs := flag.NewFlagSet("set-brightness", flag.ExitOnError)
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
	if err := preflight.StudioRunning(); err != nil {
		return err
	}
	dev, err := device.Open()
	if err != nil {
		return err
	}
	defer dev.Close()

	pkt, err := protocol.SetBrightnessPacket(level)
	if err != nil {
		return err
	}
	if err := dev.SendPacket(pkt); err != nil {
		return err
	}
	fmt.Printf("brightness set to %d\n", level)
	return nil
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to YAML config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cfgPath == "" {
		return errors.New("--config is required")
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if err := preflight.StudioRunning(); err != nil {
		return err
	}
	dev, err := device.Open()
	if err != nil {
		return err
	}
	defer dev.Close()

	if err := initDevice(dev, cfg.Buttons); err != nil {
		return err
	}

	if cfg.Device.Brightness != nil {
		pkt, err := protocol.SetBrightnessPacket(*cfg.Device.Brightness)
		if err != nil {
			return err
		}
		if err := dev.SendPacket(pkt); err != nil {
			return fmt.Errorf("set brightness: %w", err)
		}
		fmt.Printf("applied brightness=%d\n", *cfg.Device.Brightness)
	}

	// Build index → action map for fast lookup on button press.
	actionMap := make(map[int]*config.Action, len(cfg.Buttons))
	for _, btn := range cfg.Buttons {
		if btn.Action != nil {
			actionMap[btn.HIDIndex()] = btn.Action
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Println("running. Ctrl-C to exit.")
	return readLoop(ctx, dev, actionMap)
}

// initDevice uploads a page ZIP to switch the D200 into host mode.
// Pass nil buttons for empty slots (dump-events); pass config buttons for run.
func initDevice(dev *device.Device, buttons []config.Button) error {
	pkts, err := protocol.SetButtonsFromConfig(buttons)
	if err != nil {
		return fmt.Errorf("build page zip: %w", err)
	}
	if err := dev.SendPackets(pkts); err != nil {
		return fmt.Errorf("send page zip: %w", err)
	}
	return nil
}

func readLoop(ctx context.Context, dev *device.Device, actionMap map[int]*config.Action) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		raw, err := dev.ReadPacket(100 * time.Millisecond)
		if err != nil {
			return err
		}
		if len(raw) == 0 {
			continue
		}
		ev, err := protocol.ParseEvent(raw)
		if err != nil {
			fmt.Printf("parse error: %v (raw len=%d)\n", err, len(raw))
			continue
		}
		fmt.Println(ev.String())

		if ev.Kind == protocol.EventButton && ev.Pressed {
			if a, ok := actionMap[int(ev.Index)]; ok {
				if err := actions.Run(a); err != nil {
					fmt.Fprintf(os.Stderr, "action error: %v\n", err)
				}
			}
		}
	}
}
