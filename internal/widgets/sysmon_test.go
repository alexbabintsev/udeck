package widgets

import (
	"errors"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/macos"
)

// fakeSystem serves scripted readings, one per call.
type fakeSystem struct {
	cpu     []macos.CPUTicks
	net     [][]macos.NetCounter
	disk    macos.DiskSpace
	diskAt  string
	battery macos.BatteryState
	fail    bool
	now     time.Time
}

func (f *fakeSystem) source() systemSource {
	pop := func(n int) int { return min(n, 1) }
	return systemSource{
		cpu: func() (macos.CPUTicks, error) {
			if f.fail {
				return macos.CPUTicks{}, errors.New("x")
			}
			t := f.cpu[0]
			f.cpu = f.cpu[pop(len(f.cpu)-1):]
			return t, nil
		},
		net: func() ([]macos.NetCounter, error) {
			if f.fail {
				return nil, errors.New("x")
			}
			n := f.net[0]
			f.net = f.net[pop(len(f.net)-1):]
			return n, nil
		},
		disk: func(p string) (macos.DiskSpace, error) {
			f.diskAt = p
			if f.fail {
				return macos.DiskSpace{}, errors.New("x")
			}
			return f.disk, nil
		},
		battery: func() (macos.BatteryState, error) { return f.battery, nil },
		now:     func() time.Time { return f.now },
	}
}

func TestCPUUsage(t *testing.T) {
	prev := macos.CPUTicks{Busy: 100, Total: 1000}
	if u := (macos.CPUTicks{Busy: 150, Total: 1100}).Usage(prev); u != 0.5 {
		t.Fatalf("usage %v", u)
	}
	if u := (macos.CPUTicks{Busy: 10, Total: 20}).Usage(prev); u != 0 {
		t.Fatal("counters going back (after a reset) read as idle")
	}
	if u := prev.Usage(prev); u != 0 {
		t.Fatal("no time passed")
	}
}

func TestCPUFrame(t *testing.T) {
	f := &fakeSystem{cpu: []macos.CPUTicks{{Busy: 100, Total: 1000}, {Busy: 190, Total: 1100}, {Busy: 200, Total: 1200}}}
	frame, err := sysmonFrame(&config.Widget{Type: "cpu"}, f.source())
	if err != nil {
		t.Fatal(err)
	}
	frame() // since boot: 10%
	if img := frame(); !hasColor(img, styleAlert) {
		t.Fatal("90% busy must be red")
	}
	if img := frame(); !hasColor(img, styleLive) || hasColor(img, styleAlert) {
		t.Fatal("10% busy must be green")
	}
	f.fail = true
	if img := frame(); hasColor(img, styleLive) || !hasColor(img, styleDim) {
		t.Fatal("a failed read renders dim")
	}
}

func TestNetworkFrameRatesAndPrefixes(t *testing.T) {
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	f := &fakeSystem{now: at, net: [][]macos.NetCounter{
		{{Name: "en0", RxByte: 1000, TxByte: 100}, {Name: "utun1", RxByte: 5e9}, {Name: "lo0", RxByte: 7e9}},
		{{Name: "en0", RxByte: 2_001_000, TxByte: 200}, {Name: "utun1", RxByte: 9e9}, {Name: "lo0", RxByte: 9e9}},
	}}
	// Only en* counts: the tunnel and loopback traffic is ignored.
	rx1, tx1 := sumNet(f.net[0], netPrefixes(""))
	rx2, tx2 := sumNet(f.net[1], netPrefixes(""))
	if rx2-rx1 != 2_000_000 || tx2-tx1 != 100 {
		t.Fatalf("deltas %d/%d", rx2-rx1, tx2-tx1)
	}
	frame, _ := sysmonFrame(&config.Widget{Type: "network"}, f.source())
	// Through the frame: the first reading has no rate yet.
	first := frame()
	f.now = at.Add(2 * time.Second)
	second := frame()
	if string(first.Pix) == string(second.Pix) {
		t.Fatal("the second reading must show rates")
	}
	if p := netPrefixes(" en0, bridge ,"); len(p) != 2 || p[0] != "en0" || p[1] != "bridge" {
		t.Fatalf("prefixes %v", p)
	}
	f.fail = true
	if img := frame(); !hasColor(img, styleDim) {
		t.Fatal("a failed read renders dim")
	}
}

func TestNetworkCounterResetShowsNoRate(t *testing.T) {
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	f := &fakeSystem{now: at, net: [][]macos.NetCounter{
		{{Name: "en0", RxByte: 5000, TxByte: 5000}},
		{{Name: "en0", RxByte: 10, TxByte: 10}}, // interface re-created
	}}
	frame, _ := sysmonFrame(&config.Widget{Type: "network"}, f.source())
	idle := frame()
	f.now = at.Add(time.Second)
	if string(frame().Pix) != string(idle.Pix) {
		t.Fatal("a counter reset must show no rate, not a huge one")
	}
}

func TestDiskFrame(t *testing.T) {
	f := &fakeSystem{disk: macos.DiskSpace{Total: 1000e9, Free: 50e9}}
	frame, _ := sysmonFrame(&config.Widget{Type: "disk", Path: "~/Backups"}, f.source())
	if img := frame(); !hasColor(img, styleAlert) {
		t.Fatal("95% full must be red")
	}
	if f.diskAt == "~/Backups" || f.diskAt == "" {
		t.Fatalf("path must be expanded, got %q", f.diskAt)
	}
	frame, _ = sysmonFrame(&config.Widget{Type: "disk"}, f.source())
	frame()
	if f.diskAt != "/" {
		t.Fatalf("default path %q", f.diskAt)
	}
	if (macos.DiskSpace{Total: 100, Free: 25}).Used() != 0.75 || (macos.DiskSpace{}).Used() != 0 {
		t.Fatal("DiskSpace.Used")
	}
	f.fail = true
	if img := frame(); !hasColor(img, styleDim) || hasColor(img, styleAlert) {
		t.Fatal("a failed read renders dim")
	}
}

func TestBatteryFrame(t *testing.T) {
	f := &fakeSystem{}
	frame, _ := sysmonFrame(&config.Widget{Type: "battery"}, f.source())
	if img := frame(); hasColor(img, styleLive) {
		t.Fatal("no battery renders dim")
	}
	cases := []struct {
		st   macos.BatteryState
		want [3]bool // live, warn, alert
	}{
		{macos.BatteryState{Present: true, Percent: 80}, [3]bool{true, false, false}},
		{macos.BatteryState{Present: true, Percent: 15}, [3]bool{false, true, false}},
		{macos.BatteryState{Present: true, Percent: 5}, [3]bool{false, false, true}},
		{macos.BatteryState{Present: true, Percent: 5, Charging: true, OnAC: true}, [3]bool{true, false, false}},
	}
	for _, c := range cases {
		f.battery = c.st
		img := frame()
		got := [3]bool{hasColor(img, styleLive), hasColor(img, styleWarn), hasColor(img, styleAlert)}
		if got != c.want {
			t.Fatalf("%+v: live/warn/alert %v, want %v", c.st, got, c.want)
		}
	}
}

func TestSystemWidgetLifecycle(t *testing.T) {
	// 50% since boot, then 50% between the polls.
	f := &fakeSystem{cpu: []macos.CPUTicks{{Busy: 1, Total: 2}, {Busy: 2, Total: 4}}}
	old := systemStats
	systemStats = f.source()
	t.Cleanup(func() { systemStats = old })
	rec := &recorder{}
	w, err := NewSystemWidget("c", &config.Widget{Type: "cpu", PollMS: 60000}, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	readIcon(t, w.IconPath())
	w.refresh() // same reading, same picture: nothing sent
	if rec.count() != 0 {
		t.Fatalf("notifications %d", rec.count())
	}
	w.HandleGesture(ig.Tap)
	_ = w.Close()
	if _, err := NewSystemWidget("x", &config.Widget{Type: "gpu"}, nil); err == nil {
		t.Fatal("expected an unknown monitor error")
	}
}

func TestFormatSizeAndRate(t *testing.T) {
	sizes := map[uint64]string{0: "0", 999: "999", 1500: "1.5K", 151e9: "151G", 2.4e12: "2.4T", 820e6: "820M"}
	for in, want := range sizes {
		if got := formatSize(in); got != want {
			t.Fatalf("formatSize(%d) = %q, want %q", in, got, want)
		}
	}
	rates := map[float64]string{0: "0 B/s", 340: "340 B/s", 85e3: "85 KB/s", 1.24e6: "1.2 MB/s", 112e6: "112 MB/s", 1.5e9: "1.5 GB/s"}
	for in, want := range rates {
		if got := formatRate(in); got != want {
			t.Fatalf("formatRate(%v) = %q, want %q", in, got, want)
		}
	}
}
