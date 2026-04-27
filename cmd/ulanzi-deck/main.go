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
	"github.com/alexbabintsev/ulanzi-deck-go/internal/widgets"
)

var backButton = config.Button{
	At:    [2]int{3, 2},
	Label: "Back",
}

const (
	deviceKeepaliveInterval = 30 * time.Second
	deviceRecoverRetries    = 2
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
	return readLoop(ctx, "", nil, dev, nil, nil, nil)
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

	nav := newNavigator(cfg.Buttons)
	gestures := newGestureManager()
	widgetMgr, err := widgets.NewManager(cfg.Buttons)
	if err != nil {
		return fmt.Errorf("widgets: %w", err)
	}
	defer widgetMgr.Close()

	if err := applyDeviceState(dev, cfg, widgetMgr.ResolveButtons(nav.currentButtons())); err != nil {
		return err
	}

	if cfg.Device.Brightness != nil {
		fmt.Printf("applied brightness=%d\n", *cfg.Device.Brightness)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Println("running. Ctrl-C to exit.")
	return readLoop(ctx, *cfgPath, cfg, dev, nav, gestures, widgetMgr)
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

func readLoop(ctx context.Context, cfgPath string, cfg *config.Config, dev *device.Device, nav *navigator, gestures *gestureManager, widgetMgr *widgets.Manager) error {
	lastKeepalive := time.Now()
	var reloader *configReloader
	if cfgPath != "" {
		reloader = newConfigReloader(cfgPath)
		defer reloader.Close()
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if reloader != nil {
			select {
			case <-reloader.Changes():
				nextCfg, err := config.Load(cfgPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "config reload failed: %v\n", err)
				} else {
					nextNav := newNavigator(nextCfg.Buttons)
					nextGestures := newGestureManager()
					nextWidgetMgr, err := widgets.NewManager(nextCfg.Buttons)
					if err != nil {
						fmt.Fprintf(os.Stderr, "widget reload failed: %v\n", err)
					} else {
						if err := applyDeviceState(dev, nextCfg, nextWidgetMgr.ResolveButtons(nextNav.currentButtons())); err != nil {
							_ = nextWidgetMgr.Close()
							fmt.Fprintf(os.Stderr, "config apply failed: %v\n", err)
						} else {
							if widgetMgr != nil {
								_ = widgetMgr.Close()
							}
							cfg = nextCfg
							nav = nextNav
							gestures = nextGestures
							widgetMgr = nextWidgetMgr
							lastKeepalive = time.Now()
							fmt.Fprintln(os.Stderr, "config reloaded")
						}
					}
				}
			default:
			}
		}
		if nav != nil && time.Since(lastKeepalive) >= deviceKeepaliveInterval {
			if err := refreshCurrentPage(cfg, dev, nav, widgetMgr); err != nil {
				if recovered, recErr := recoverDevice(cfg, &dev, nav, widgetMgr, err); recovered {
					lastKeepalive = time.Now()
					continue
				} else if recErr != nil {
					return recErr
				}
			}
			lastKeepalive = time.Now()
		}
		if widgetMgr != nil {
			select {
			case id := <-widgetMgr.Updates():
				if nav != nil && buttonVisible(nav.currentButtons(), id) {
					if err := refreshCurrentPage(cfg, dev, nav, widgetMgr); err != nil {
						if recovered, recErr := recoverDevice(cfg, &dev, nav, widgetMgr, fmt.Errorf("update widgets: %w", err)); recovered {
							lastKeepalive = time.Now()
							continue
						} else if recErr != nil {
							return recErr
						}
					}
					lastKeepalive = time.Now()
				}
			default:
			}
		}
		if gestures != nil && nav != nil {
			if action := gestures.Tick(nav.currentButtons(), time.Now()); action != nil {
				if err := actions.Run(action); err != nil {
					fmt.Fprintf(os.Stderr, "action error: %v\n", err)
				}
				lastKeepalive = time.Now()
			}
		}
		raw, err := dev.ReadPacket(100 * time.Millisecond)
		if err != nil {
			if recovered, recErr := recoverDevice(cfg, &dev, nav, widgetMgr, err); recovered {
				lastKeepalive = time.Now()
				continue
			} else if recErr != nil {
				return recErr
			}
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

		if ev.Kind != protocol.EventButton {
			continue
		}
		if nav == nil {
			continue
		}
		btn := nav.findButton(int(ev.Index))
		changed := nav.handleEvent(int(ev.Index), ev.Pressed)
		if changed {
			if gestures != nil {
				gestures.Reset()
			}
			if err := refreshCurrentPage(cfg, dev, nav, widgetMgr); err != nil {
				if recovered, recErr := recoverDevice(cfg, &dev, nav, widgetMgr, fmt.Errorf("update page: %w", err)); recovered {
					lastKeepalive = time.Now()
					continue
				} else if recErr != nil {
					return recErr
				}
			}
			lastKeepalive = time.Now()
			continue
		}
		var action *config.Action
		if btn != nil && btn.On != nil && gestures != nil {
			if action = gestures.HandleEvent(btn, ev.Pressed, time.Now()); action == nil {
				continue
			}
		}
		if ev.Pressed && widgetMgr != nil {
			if btn != nil && btn.Widget != nil {
				if widgetMgr.Press(btn.RuntimeID) {
					lastKeepalive = time.Now()
					continue
				}
			}
		}
		if action != nil {
			if err := actions.Run(action); err != nil {
				fmt.Fprintf(os.Stderr, "action error: %v\n", err)
			}
			lastKeepalive = time.Now()
		}
	}
}

func refreshCurrentPage(cfg *config.Config, dev *device.Device, nav *navigator, widgetMgr *widgets.Manager) error {
	buttons := nav.currentButtons()
	if widgetMgr != nil {
		buttons = widgetMgr.ResolveButtons(buttons)
	}
	return applyDeviceState(dev, cfg, buttons)
}

func applyDeviceState(dev *device.Device, cfg *config.Config, buttons []config.Button) error {
	if err := initDevice(dev, buttons); err != nil {
		return err
	}

	pkt, err := protocol.SetLabelStylePacket(cfg.Device.LabelStyle)
	if err != nil {
		return fmt.Errorf("set label style: %w", err)
	}
	if err := dev.SendPacket(pkt); err != nil {
		return fmt.Errorf("set label style: %w", err)
	}

	if cfg.Device.Brightness != nil {
		pkt, err := protocol.SetBrightnessPacket(*cfg.Device.Brightness)
		if err != nil {
			return err
		}
		if err := dev.SendPacket(pkt); err != nil {
			return fmt.Errorf("set brightness: %w", err)
		}
	}
	return nil
}

func recoverDevice(cfg *config.Config, dev **device.Device, nav *navigator, widgetMgr *widgets.Manager, cause error) (bool, error) {
	if nav == nil {
		return false, cause
	}
	fmt.Fprintf(os.Stderr, "recovering device after error: %v\n", cause)
	if *dev != nil {
		_ = (*dev).Close()
	}
	var lastErr error
	for attempt := 0; attempt < deviceRecoverRetries; attempt++ {
		reopened, err := device.Open()
		if err != nil {
			lastErr = err
			time.Sleep(300 * time.Millisecond)
			continue
		}
		*dev = reopened
		if err := refreshCurrentPage(cfg, reopened, nav, widgetMgr); err != nil {
			lastErr = err
			_ = reopened.Close()
			time.Sleep(300 * time.Millisecond)
			continue
		}
		fmt.Fprintln(os.Stderr, "device recovered")
		return true, nil
	}
	if lastErr != nil {
		return false, fmt.Errorf("device recovery failed after %v: %w", cause, lastErr)
	}
	return false, cause
}

type navigator struct {
	root  []config.Button
	stack []*config.Directory
}

func newNavigator(root []config.Button) *navigator {
	return &navigator{root: root}
}

func (n *navigator) currentButtons() []config.Button {
	buttons := append([]config.Button(nil), n.currentPageButtons()...)
	if len(n.stack) > 0 {
		buttons = append(buttons, backButton)
	}
	return buttons
}

func (n *navigator) handleEvent(index int, pressed bool) (pageChanged bool) {
	for _, btn := range n.currentButtons() {
		if btn.HIDIndex() != index {
			continue
		}
		if len(n.stack) > 0 && btn.At == backButton.At && btn.Label == backButton.Label && btn.Directory == nil && btn.On == nil {
			if !pressed {
				return false
			}
			n.stack = n.stack[:len(n.stack)-1]
			return true
		}
		if btn.Directory != nil {
			if !pressed {
				return false
			}
			n.stack = append(n.stack, btn.Directory)
			return true
		}
		return false
	}
	return false
}

func (n *navigator) currentPageButtons() []config.Button {
	if len(n.stack) == 0 {
		return n.root
	}
	return n.stack[len(n.stack)-1].Buttons
}

func (n *navigator) findButton(index int) *config.Button {
	for _, btn := range n.currentButtons() {
		if btn.HIDIndex() == index {
			b := btn
			return &b
		}
	}
	return nil
}

func buttonVisible(buttons []config.Button, id string) bool {
	for _, btn := range buttons {
		if btn.RuntimeID == id {
			return true
		}
	}
	return false
}
