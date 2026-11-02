package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
)

type infoDeck struct {
	*fakeDeck
	info string
	err  error
}

func (d infoDeck) Info(time.Duration) (string, error) { return d.info, d.err }

// stubCLI wires the CLI to dev and a context that is already cancelled,
// so long-running commands return right after setup.
func stubCLI(t *testing.T, dev deck) *int {
	t.Helper()
	noPause(t)
	oldOpen, oldStudio, oldKeys, oldPower, oldCtx := openDevice, studioRunning, requestKeyPermissions, startPowerWatcher, signalContext
	oldLogPath := defaultDebugLogPath
	logPath := filepath.Join(t.TempDir(), "logs", "debug.log")
	defaultDebugLogPath = func() string { return logPath }
	opens := 0
	openDevice = func() (deck, error) {
		opens++
		if dev == nil {
			return nil, errors.New("no device")
		}
		return dev, nil
	}
	studioRunning = func() error { return nil }
	requestKeyPermissions = func() {}
	startPowerWatcher = func(context.Context) (*powerWatcher, error) { return nil, nil }
	signalContext = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, cancel
	}
	t.Cleanup(func() {
		openDevice, studioRunning, requestKeyPermissions, startPowerWatcher, signalContext = oldOpen, oldStudio, oldKeys, oldPower, oldCtx
		defaultDebugLogPath = oldLogPath
		debug.SetEnabled(false)
	})
	return &opens
}

func cli(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := runCLI(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCLIUsageAndUnknown(t *testing.T) {
	if code, _, stderr := cli(); code != 2 || !strings.Contains(stderr, "Usage:") {
		t.Fatalf("no args: code=%d", code)
	}
	if code, stdout, _ := cli("help"); code != 0 || !strings.Contains(stdout, "Usage:") {
		t.Fatalf("help: code=%d", code)
	}
	if code, _, stderr := cli("bogus"); code != 2 || !strings.Contains(stderr, "unknown subcommand: bogus") {
		t.Fatalf("bogus: code=%d", code)
	}
}

func TestCLISetBrightness(t *testing.T) {
	dev := &fakeDeck{}
	stubCLI(t, dev)
	code, stdout, _ := cli("set-brightness", "40")
	if code != 0 || !strings.Contains(stdout, "brightness set to 40") {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	sent := dev.transfers(protocol.CmdSetBrightness)
	if len(sent) != 1 || string(sent[0].data) != "40" || dev.closed != 1 {
		t.Fatalf("sent=%+v closed=%d", sent, dev.closed)
	}

	for _, args := range [][]string{{}, {"x"}, {"101"}, {"--nope"}} {
		if code, _, _ := cli(append([]string{"set-brightness"}, args...)...); code != 1 {
			t.Fatalf("args %v: code=%d", args, code)
		}
	}
	dev.sendErrs = []error{errHID}
	if code, _, _ := cli("set-brightness", "10"); code != 1 {
		t.Fatal("expected send error")
	}
}

func TestCLIStudioRunningBlocksDeviceAccess(t *testing.T) {
	opens := stubCLI(t, &fakeDeck{})
	studioRunning = func() error { return errors.New("Ulanzi Studio is running") }
	for _, args := range [][]string{{"info"}, {"set-brightness", "5"}, {"dump-events"}} {
		code, _, stderr := cli(args...)
		if code != 1 || !strings.Contains(stderr, "Ulanzi Studio is running") {
			t.Fatalf("%v: code=%d stderr=%q", args, code, stderr)
		}
	}
	if *opens != 0 {
		t.Fatal("device must not be opened while Studio runs")
	}
}

func TestCLIOpenFailure(t *testing.T) {
	stubCLI(t, nil)
	if code, _, stderr := cli("info"); code != 1 || !strings.Contains(stderr, "no device") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestCLIInfo(t *testing.T) {
	stubCLI(t, infoDeck{fakeDeck: &fakeDeck{}, info: "D200 fw1"})
	if code, stdout, _ := cli("info"); code != 0 || !strings.Contains(stdout, "device_info: D200 fw1") {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	stubCLI(t, infoDeck{fakeDeck: &fakeDeck{}, err: errors.New("timeout")})
	if code, stdout, _ := cli("info"); code != 0 || !strings.Contains(stdout, "no IN_DEVICE_INFO frame") {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	stubCLI(t, &fakeDeck{})
	if code, _, _ := cli("info"); code != 1 {
		t.Fatal("expected error for device without Info")
	}
}

func TestCLIDumpEvents(t *testing.T) {
	dev := &fakeDeck{}
	stubCLI(t, dev)
	if code, _, stderr := cli("dump-events", "--debug"); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !debug.Enabled() {
		t.Fatal("--debug must enable diagnostics")
	}
	pages := dev.transfers(protocol.CmdSetButtons)
	if len(pages) != 1 || len(pages[0].slots) != 14 {
		t.Fatalf("expected empty host-mode page, got %+v", pages)
	}
	dev.sendErrs = []error{errHID}
	if code, _, _ := cli("dump-events"); code != 1 {
		t.Fatal("expected init failure")
	}
	if code, _, _ := cli("dump-events", "-h"); code != 0 {
		t.Fatal("-h should exit 0")
	}
}

func TestCLIRun(t *testing.T) {
	dev := &fakeDeck{}
	stubCLI(t, dev)
	path, _ := writeConfig(t, loopConfig)
	code, stdout, stderr := cli("run", "--config", path, "--debug")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "applied brightness=50") {
		t.Fatalf("stdout=%q", stdout)
	}
	if len(dev.transfers(protocol.CmdSetButtons)) != 1 || dev.closed != 1 {
		t.Fatalf("cmds=%v closed=%d", dev.cmds(), dev.closed)
	}
}

func TestCLIRunRequestsKeyPermissionsAndUsesPowerWatcher(t *testing.T) {
	stubCLI(t, &fakeDeck{})
	asked := false
	requestKeyPermissions = func() { asked = true }
	watched := false
	startPowerWatcher = func(context.Context) (*powerWatcher, error) {
		watched = true
		return &powerWatcher{events: make(chan powerEvent)}, nil
	}
	path, _ := writeConfig(t, `
buttons:
  - at: [0, 0]
    label: Copy
    on:
      tap: {type: key, key: "cmd c"}
`)
	if code, _, stderr := cli("run", "--config", path); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !asked || !watched {
		t.Fatalf("asked=%v watched=%v", asked, watched)
	}
}

func TestCLIRunErrors(t *testing.T) {
	stubCLI(t, &fakeDeck{sendErrs: []error{errHID}})
	path, _ := writeConfig(t, loopConfig)
	if code, _, _ := cli("run", "--config", path); code != 1 {
		t.Fatal("expected initial page failure")
	}
	if code, _, _ := cli("run", "--config", "/nonexistent.yaml"); code != 1 {
		t.Fatal("expected config error")
	}
	if code, _, _ := cli("run", "--bogus"); code != 1 {
		t.Fatal("expected flag error")
	}
	stubCLI(t, nil)
	if code, _, _ := cli("run", "--config", path); code != 1 {
		t.Fatal("expected open error")
	}
}

func TestCLIDebugWritesLogFile(t *testing.T) {
	stubCLI(t, &fakeDeck{})
	path, _ := writeConfig(t, loopConfig)
	if code, _, stderr := cli("run", "--config", path, "--debug"); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	data, err := os.ReadFile(defaultDebugLogPath())
	if err != nil {
		t.Fatalf("default debug log not written: %v", err)
	}
	if !strings.Contains(string(data), "ulanzi-deck started") || !strings.Contains(string(data), "running. Ctrl-C to exit.") {
		t.Fatalf("log file content: %q", data)
	}
	info, _ := os.Stat(defaultDebugLogPath())
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("log file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestCLIExplicitLogFileAndRotation(t *testing.T) {
	stubCLI(t, &fakeDeck{})
	logPath := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(logPath, make([]byte, maxLogFileSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := cli("dump-events", "--log-file", logPath); code != 0 {
		t.Fatal("dump-events failed")
	}
	if info, err := os.Stat(logPath + ".1"); err != nil || info.Size() <= maxLogFileSize {
		t.Fatal("oversized log must be rotated to .1")
	}
	if data, _ := os.ReadFile(logPath); !strings.Contains(string(data), "listening for events") {
		t.Fatalf("new log content: %q", data)
	}
	if code, _, _ := cli("dump-events", "--log-file", "/dev/null/not-a-dir/x.log"); code != 1 {
		t.Fatal("unwritable log path must fail")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := defaultConfigPath(); got != filepath.Join("/xdg", "ulanzi-deck", "config.yaml") {
		t.Fatalf("with XDG_CONFIG_HOME: %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got := defaultConfigPath(); got != filepath.Join("/home/u", ".config", "ulanzi-deck", "config.yaml") {
		t.Fatalf("default: %q", got)
	}
}
