package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/alexbabintsev/udeck/internal/icons"

	"github.com/alexbabintsev/udeck/internal/config"
	"github.com/alexbabintsev/udeck/internal/device"
	"github.com/alexbabintsev/udeck/internal/protocol"
)

func TestBindingNeedsLoopOnlyForPageActions(t *testing.T) {
	if bindingNeedsLoop(&config.GestureBinding{Type: "shell", Cmd: "true"}) {
		t.Fatal("shell binding should run off the loop")
	}
	if !bindingNeedsLoop(&config.GestureBinding{Type: "page_next"}) {
		t.Fatal("page_next must run on the loop")
	}
	chain := &config.GestureBinding{Chain: []config.ChainStep{
		{Action: &config.Action{Type: "key", Key: "a"}},
		{Action: &config.Action{Type: "page_prev"}},
	}}
	if !bindingNeedsLoop(chain) {
		t.Fatal("chain containing page_prev must run on the loop")
	}
	delayed := &config.GestureBinding{Chain: []config.ChainStep{
		{DelayMS: 100},
		{Action: &config.Action{Type: "page_prev"}},
	}}
	if bindingNeedsLoop(delayed) {
		t.Fatal("chain with a delay must run on the worker, not stall the loop")
	}
}

func TestDeferredPagerPostsMoves(t *testing.T) {
	ch := make(chan int, 1)
	p := deferredPager(ch)
	if p.nextPage() {
		t.Fatal("deferred move must not report an immediate change")
	}
	if got := <-ch; got != 1 {
		t.Fatalf("next = %d", got)
	}
	p.prevPage()
	if got := <-ch; got != -1 {
		t.Fatalf("prev = %d", got)
	}
	p.nextPage()
	p.nextPage() // queue full: dropped, must not block
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

func TestKeepalivePacketIsDeviceInfoRequest(t *testing.T) {
	pkt := keepalivePacket()
	cmd, n, err := protocol.DecodeHeader(pkt[:])
	if err != nil || cmd != protocol.CmdGetDeviceInfo || n != 0 {
		t.Fatalf("cmd=%v n=%d err=%v", cmd, n, err)
	}
	if compactJSON("{\n  \"a\": 1\n}") != `{"a":1}` || compactJSON("not json") != "not json" {
		t.Fatal("compactJSON")
	}
}

func TestImageFormatSelection(t *testing.T) {
	cases := []struct {
		setting, firmware string
		want              icons.Format
	}{
		{"", "5.3.6", icons.WebP},
		{"auto", "5.4.0", icons.WebP},
		{"", "5.3.1", icons.JPEG},
		{"", "", icons.JPEG},
		{"", "garbage", icons.JPEG},
		{"webp", "5.3.1", icons.WebP},
		{"jpeg", "5.3.6", icons.JPEG},
	}
	for _, c := range cases {
		if got := imageFormatFor(c.setting, c.firmware); got != c.want {
			t.Fatalf("imageFormatFor(%q, %q) = %s, want %s", c.setting, c.firmware, got, c.want)
		}
	}
	if !versionAtLeast("5.3.6", "5.3.6") || !versionAtLeast("6", "5.3.6") || versionAtLeast("5.3", "5.3.6") || !versionAtLeast("5.10", "5.3.6") {
		t.Fatal("versionAtLeast")
	}
	if firmwareVersion(`{"Dversion": " 5.3.6 "}`) != "5.3.6" || firmwareVersion("nope") != "" {
		t.Fatal("firmwareVersion")
	}
	defer icons.SetFormat(icons.JPEG)
	applyImageSettings(&config.Config{Device: config.DeviceConfig{ImageFormat: "webp", ImageQuality: 60}}, "")
	if icons.CurrentFormat() != icons.WebP {
		t.Fatal("applyImageSettings must set the format")
	}
}
