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
	wakeRecoverRetries      = 10
	defaultConfigPath       = "examples/test-config.yaml"
)

const usage = `ulanzi-deck — minimal headless daemon for the Ulanzi D200

Usage:
  ulanzi-deck info
  ulanzi-deck dump-events
  ulanzi-deck set-brightness <0..100>
  ulanzi-deck run [--config <path.yaml>]

Note: Ulanzi Studio holds an exclusive HID lock. Quit it before running:
  osascript -e 'tell application "Ulanzi Studio" to quit'

If --config is omitted for run, the default is:
  ` + defaultConfigPath + `
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
	return readLoop(ctx, "", nil, dev, nil, nil, nil, nil)
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
		*cfgPath = defaultConfigPath
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if cfg.UsesKeyActions() {
		actions.RequestKeyPermissions()
	}
	if err := preflight.StudioRunning(); err != nil {
		return err
	}
	dev, err := device.Open()
	if err != nil {
		return err
	}
	defer dev.Close()

	nav := newNavigator(cfg.PagesOrDefault())
	gestures := newGestureManager()
	toggles := newToggleManager(cfg)
	widgetMgr, err := widgets.NewManager(cfg)
	if err != nil {
		return fmt.Errorf("widgets: %w", err)
	}
	defer widgetMgr.Close()
	widgetMgr.SetContext(nav.Context())

	initialButtons := toggles.ResolveButtons(nav.currentButtons())
	initialButtons, err = widgetMgr.ResolveButtons(initialButtons)
	if err != nil {
		return fmt.Errorf("widgets: %w", err)
	}
	if err := applyDeviceState(dev, cfg, initialButtons); err != nil {
		return err
	}

	if cfg.Device.Brightness != nil {
		fmt.Printf("applied brightness=%d\n", *cfg.Device.Brightness)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Println("running. Ctrl-C to exit.")
	return readLoop(ctx, *cfgPath, cfg, dev, nav, gestures, toggles, widgetMgr)
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

func readLoop(ctx context.Context, cfgPath string, cfg *config.Config, dev *device.Device, nav *navigator, gestures *gestureManager, toggles *toggleManager, widgetMgr *widgets.Manager) error {
	lastKeepalive := time.Now()
	sleeping := false
	var reloader *configReloader
	if cfgPath != "" {
		reloader = newConfigReloader(cfgPath, cfg.IconPaths())
		defer reloader.Close()
	}
	power, _ := newPowerWatcher(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if power != nil {
			select {
			case evt, ok := <-power.Events():
				if ok {
					switch evt {
					case powerEventSleep:
						fmt.Fprintln(os.Stderr, "system sleep detected; dimming and closing device")
						sleeping = true
						prepareDeviceForSleep(cfg, dev)
					case powerEventWake:
						fmt.Fprintln(os.Stderr, "system wake detected; restoring device")
						sleeping = false
						if recovered, recErr := recoverDeviceWithRetries(cfg, &dev, nav, toggles, widgetMgr, errors.New("wake restore"), wakeRecoverRetries); recovered {
							lastKeepalive = time.Now()
						} else if recErr != nil {
							fmt.Fprintf(os.Stderr, "wake recovery failed: %v\n", recErr)
						}
					case powerEventShutdown:
						fmt.Fprintln(os.Stderr, "system shutdown detected; dimming and closing device")
						prepareDeviceForSleep(cfg, dev)
						return nil
					}
				}
			default:
			}
		}
		if sleeping {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if reloader != nil {
			select {
			case <-reloader.Changes():
				nextCfg, err := config.Load(cfgPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "config reload failed: %v\n", err)
				} else {
					nextNav := newNavigator(nextCfg.PagesOrDefault())
					nextGestures := newGestureManager()
					nextToggles := newToggleManager(nextCfg)
					nextWidgetMgr, err := widgets.NewManager(nextCfg)
					if err != nil {
						fmt.Fprintf(os.Stderr, "widget reload failed: %v\n", err)
					} else {
						nextWidgetMgr.SetContext(nextNav.Context())
						initialButtons := nextToggles.ResolveButtons(nextNav.currentButtons())
						initialButtons, err := nextWidgetMgr.ResolveButtons(initialButtons)
						if err != nil {
							_ = nextWidgetMgr.Close()
							fmt.Fprintf(os.Stderr, "widget resolve failed: %v\n", err)
						} else if err := applyDeviceState(dev, nextCfg, initialButtons); err != nil {
							_ = nextWidgetMgr.Close()
							fmt.Fprintf(os.Stderr, "config apply failed: %v\n", err)
						} else {
							if widgetMgr != nil {
								_ = widgetMgr.Close()
							}
							cfg = nextCfg
							nav = nextNav
							gestures = nextGestures
							toggles = nextToggles
							widgetMgr = nextWidgetMgr
							reloader.UpdatePaths(cfgPath, nextCfg.IconPaths())
							lastKeepalive = time.Now()
							fmt.Fprintln(os.Stderr, "config reloaded")
						}
					}
				}
			default:
			}
		}
		if nav != nil && time.Since(lastKeepalive) >= deviceKeepaliveInterval {
			if err := refreshCurrentPage(cfg, dev, nav, toggles, widgetMgr); err != nil {
				if recovered, recErr := recoverDevice(cfg, &dev, nav, toggles, widgetMgr, err); recovered {
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
					if err := refreshCurrentPage(cfg, dev, nav, toggles, widgetMgr); err != nil {
						if recovered, recErr := recoverDevice(cfg, &dev, nav, toggles, widgetMgr, fmt.Errorf("update widgets: %w", err)); recovered {
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
			if evt := gestures.Tick(nav.currentButtons(), time.Now()); evt != nil {
				binding, changed := resolveBindingEvent(evt, toggles)
				if changed {
					if err := refreshCurrentPage(cfg, dev, nav, toggles, widgetMgr); err != nil {
						if recovered, recErr := recoverDevice(cfg, &dev, nav, toggles, widgetMgr, fmt.Errorf("update toggle page: %w", err)); recovered {
							lastKeepalive = time.Now()
							continue
						} else if recErr != nil {
							return recErr
						}
					}
				}
				if binding == nil {
					lastKeepalive = time.Now()
					continue
				}
				if changed, err := runBinding(binding, nav, widgetMgr); err != nil {
					fmt.Fprintf(os.Stderr, "action error: %v\n", err)
				} else if changed {
					if err := refreshCurrentPage(cfg, dev, nav, toggles, widgetMgr); err != nil {
						if recovered, recErr := recoverDevice(cfg, &dev, nav, toggles, widgetMgr, fmt.Errorf("switch page: %w", err)); recovered {
							lastKeepalive = time.Now()
							continue
						} else if recErr != nil {
							return recErr
						}
					}
				}
				lastKeepalive = time.Now()
			}
		}
		if widgetMgr != nil && nav != nil {
			if widgetMgr.Tick(nav.currentButtons(), time.Now()) {
				lastKeepalive = time.Now()
			}
		}
		raw, err := dev.ReadPacket(100 * time.Millisecond)
		if err != nil {
			if recovered, recErr := recoverDevice(cfg, &dev, nav, toggles, widgetMgr, err); recovered {
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
		if btn != nil && btn.SyntheticBack {
			changed := nav.handleEvent(int(ev.Index), ev.Pressed)
			if changed {
				if gestures != nil {
					gestures.Reset()
				}
				if err := refreshCurrentPage(cfg, dev, nav, toggles, widgetMgr); err != nil {
					if recovered, recErr := recoverDevice(cfg, &dev, nav, toggles, widgetMgr, fmt.Errorf("update page: %w", err)); recovered {
						lastKeepalive = time.Now()
						continue
					} else if recErr != nil {
						return recErr
					}
				}
			}
			lastKeepalive = time.Now()
			continue
		}
		changed := nav.handleEvent(int(ev.Index), ev.Pressed)
		if changed {
			if gestures != nil {
				gestures.Reset()
			}
			if err := refreshCurrentPage(cfg, dev, nav, toggles, widgetMgr); err != nil {
				if recovered, recErr := recoverDevice(cfg, &dev, nav, toggles, widgetMgr, fmt.Errorf("update page: %w", err)); recovered {
					lastKeepalive = time.Now()
					continue
				} else if recErr != nil {
					return recErr
				}
			}
			lastKeepalive = time.Now()
			continue
		}
		var binding *config.GestureBinding
		if btn != nil && btn.On != nil && gestures != nil {
			if evt := gestures.HandleEvent(btn, ev.Pressed, time.Now()); evt != nil {
				var changed bool
				binding, changed = resolveBindingEvent(evt, toggles)
				if changed {
					if err := refreshCurrentPage(cfg, dev, nav, toggles, widgetMgr); err != nil {
						if recovered, recErr := recoverDevice(cfg, &dev, nav, toggles, widgetMgr, fmt.Errorf("update toggle page: %w", err)); recovered {
							lastKeepalive = time.Now()
							continue
						} else if recErr != nil {
							return recErr
						}
					}
				}
			}
			if binding == nil {
				continue
			}
		}
		if widgetMgr != nil {
			if btn != nil && btn.Widget != nil {
				if widgetMgr.HandleEvent(btn.RuntimeID, ev.Pressed, time.Now()) {
					lastKeepalive = time.Now()
					continue
				}
			}
		}
		if binding != nil {
			if changed, err := runBinding(binding, nav, widgetMgr); err != nil {
				fmt.Fprintf(os.Stderr, "action error: %v\n", err)
			} else if changed {
				if err := refreshCurrentPage(cfg, dev, nav, toggles, widgetMgr); err != nil {
					if recovered, recErr := recoverDevice(cfg, &dev, nav, toggles, widgetMgr, fmt.Errorf("switch page: %w", err)); recovered {
						lastKeepalive = time.Now()
						continue
					} else if recErr != nil {
						return recErr
					}
				}
			}
			lastKeepalive = time.Now()
		}
	}
}

func refreshCurrentPage(cfg *config.Config, dev *device.Device, nav *navigator, toggles *toggleManager, widgetMgr *widgets.Manager) error {
	buttons := nav.currentButtons()
	if toggles != nil {
		buttons = toggles.ResolveButtons(buttons)
	}
	if widgetMgr != nil {
		widgetMgr.SetContext(nav.Context())
		var err error
		buttons, err = widgetMgr.ResolveButtons(buttons)
		if err != nil {
			return fmt.Errorf("resolve widgets: %w", err)
		}
	}
	return applyDeviceState(dev, cfg, buttons)
}

func resolveBindingEvent(evt *gestureEvent, toggles *toggleManager) (*config.GestureBinding, bool) {
	if evt == nil || evt.Button == nil || evt.Button.On == nil {
		return nil, false
	}
	if toggles != nil {
		if binding, changed := toggles.Handle(evt.ButtonID, evt.Kind); binding != nil || changed {
			return binding, changed
		}
	}
	return bindingForKind(evt.Button.On, evt.Kind), false
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

func runBinding(binding *config.GestureBinding, nav *navigator, widgetMgr *widgets.Manager) (bool, error) {
	if binding == nil {
		return false, nil
	}
	if action := binding.Action(); action != nil {
		return runAction(action, nav, widgetMgr)
	}
	pageChanged := false
	for i := range binding.Chain {
		step := binding.Chain[i]
		if step.DelayMS > 0 {
			time.Sleep(time.Duration(step.DelayMS) * time.Millisecond)
			continue
		}
		if step.Action == nil {
			continue
		}
		changed, err := runAction(step.Action, nav, widgetMgr)
		if err != nil {
			return pageChanged, err
		}
		pageChanged = pageChanged || changed
	}
	return pageChanged, nil
}

func runAction(action *config.Action, nav *navigator, widgetMgr *widgets.Manager) (bool, error) {
	if action == nil {
		return false, nil
	}
	switch action.Type {
	case "page_next":
		if nav == nil {
			return false, nil
		}
		return nav.nextPage(), nil
	case "page_prev":
		if nav == nil {
			return false, nil
		}
		return nav.prevPage(), nil
	case "close_all_projects":
		if widgetMgr == nil {
			return false, nil
		}
		return false, widgetMgr.CloseAllProjects()
	default:
		return false, actions.Run(action)
	}
}

func recoverDevice(cfg *config.Config, dev **device.Device, nav *navigator, toggles *toggleManager, widgetMgr *widgets.Manager, cause error) (bool, error) {
	return recoverDeviceWithRetries(cfg, dev, nav, toggles, widgetMgr, cause, deviceRecoverRetries)
}

func recoverDeviceWithRetries(cfg *config.Config, dev **device.Device, nav *navigator, toggles *toggleManager, widgetMgr *widgets.Manager, cause error, retries int) (bool, error) {
	if nav == nil {
		return false, cause
	}
	fmt.Fprintf(os.Stderr, "recovering device after error: %v\n", cause)
	if *dev != nil {
		(*dev).Detach()
	}
	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		reopened, err := device.Open()
		if err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		*dev = reopened
		if err := refreshCurrentPage(cfg, reopened, nav, toggles, widgetMgr); err != nil {
			lastErr = err
			reopened.Detach()
			time.Sleep(500 * time.Millisecond)
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

func prepareDeviceForSleep(cfg *config.Config, dev *device.Device) {
	if dev == nil {
		return
	}
	pkt, err := protocol.SetBrightnessPacket(0)
	if err == nil {
		_ = dev.SendPacket(pkt)
	}
	_ = dev.Close()
}

type navigator struct {
	pages []config.Page
	page  int
	stack []*config.Folder
}

func newNavigator(pages []config.Page) *navigator {
	return &navigator{pages: append([]config.Page(nil), pages...)}
}

func (n *navigator) currentButtons() []config.Button {
	buttons := append([]config.Button(nil), n.currentPageButtons()...)
	if len(n.stack) > 0 {
		buttons = append(buttons, n.contextualBackButton())
	}
	return buttons
}

func (n *navigator) contextualBackButton() config.Button {
	for _, btn := range n.currentRootButtons() {
		if btn.At == backButton.At && btn.Widget != nil && btn.Widget.Type == "wide_dashboard" {
			btn.SyntheticBack = true
			return btn
		}
	}
	btn := backButton
	btn.SyntheticBack = true
	return btn
}

func (n *navigator) handleEvent(index int, pressed bool) (pageChanged bool) {
	for _, btn := range n.currentButtons() {
		if btn.HIDIndex() != index {
			continue
		}
		if len(n.stack) > 0 && btn.SyntheticBack {
			if !pressed {
				return false
			}
			n.stack = n.stack[:len(n.stack)-1]
			return true
		}
		if btn.Folder != nil {
			if !pressed {
				return false
			}
			n.stack = append(n.stack, btn.Folder)
			return true
		}
		return false
	}
	return false
}

func (n *navigator) currentPageButtons() []config.Button {
	if len(n.stack) == 0 {
		if len(n.pages) == 0 {
			return nil
		}
		return n.pages[n.page].Buttons
	}
	return n.stack[len(n.stack)-1].Buttons
}

func (n *navigator) currentRootButtons() []config.Button {
	if len(n.pages) == 0 {
		return nil
	}
	return n.pages[n.page].Buttons
}

func (n *navigator) nextPage() bool {
	if len(n.pages) < 2 {
		return false
	}
	n.page++
	if n.page >= len(n.pages) {
		n.page = 0
	}
	n.stack = nil
	return true
}

func (n *navigator) prevPage() bool {
	if len(n.pages) < 2 {
		return false
	}
	n.page--
	if n.page < 0 {
		n.page = len(n.pages) - 1
	}
	n.stack = nil
	return true
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

func (n *navigator) Context() widgets.Context {
	return widgets.Context{
		PageIndex:   n.page,
		TotalPages:  len(n.pages),
		FolderDepth: len(n.stack),
	}
}

func buttonVisible(buttons []config.Button, id string) bool {
	for _, btn := range buttons {
		if btn.RuntimeID == id {
			return true
		}
	}
	return false
}
