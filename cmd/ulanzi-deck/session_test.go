package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/device"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
)

func TestBindingNeedsLoopOnlyForPageActions(t *testing.T) {
	if bindingNeedsLoop(&config.GestureBinding{Type: "shell", Cmd: "true"}) {
		t.Fatal("shell binding should run off the loop")
	}
	if !bindingNeedsLoop(&config.GestureBinding{Type: "page_next"}) {
		t.Fatal("page_next must run on the loop")
	}
	chain := &config.GestureBinding{Chain: []config.ChainStep{
		{DelayMS: 100},
		{Action: &config.Action{Type: "page_prev"}},
	}}
	if !bindingNeedsLoop(chain) {
		t.Fatal("chain containing page_prev must run on the loop")
	}
}

func TestDeviceErrorClassification(t *testing.T) {
	if isDeviceError(errors.New("decode icon: bad png")) {
		t.Fatal("build errors must not trigger device recovery")
	}
	wrapped := fmt.Errorf("switch page: %w", &deviceError{fmt.Errorf("x: %w", device.ErrDeviceReset)})
	if !isDeviceError(wrapped) {
		t.Fatal("expected wrapped device error to be detected")
	}
	if !errors.Is(wrapped, device.ErrDeviceReset) {
		t.Fatal("expected ErrDeviceReset to survive wrapping")
	}
}

func TestKeepalivePacketNeverUploadsPage(t *testing.T) {
	level := 40
	pkt, err := keepalivePacket(&config.Config{Device: config.DeviceConfig{Brightness: &level}})
	if err != nil {
		t.Fatal(err)
	}
	cmd, _, err := protocol.DecodeHeader(pkt[:])
	if err != nil || cmd != protocol.CmdSetBrightness {
		t.Fatalf("expected brightness keepalive, got cmd=%v err=%v", cmd, err)
	}
	pkt, err = keepalivePacket(&config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if cmd, _, _ := protocol.DecodeHeader(pkt[:]); cmd != protocol.CmdSetLabelStyle {
		t.Fatalf("expected label style keepalive without brightness, got %v", cmd)
	}
}
