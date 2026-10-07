package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
)

// probeStallThreshold marks a transfer the device was too busy to take
// promptly; a healthy upload of a few packets takes milliseconds.
const probeStallThreshold = 100 * time.Millisecond

// cmdProbe measures how quickly the device accepts consecutive partial
// uploads of different sizes, to choose pacing between transfers. It
// sends uploads of ~N packets to one key separated by a given gap and
// reports how many transfers stalled.
func cmdProbe(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reps := fs.Int("reps", 3, "transfers per size/gap combination")
	sizes := fs.String("sizes", "3,8,14", "target upload sizes in packets")
	gaps := fs.String("gaps", "2000,1500,1000,750,500,250,0", "gaps between uploads in ms, largest first")
	concurrentRead := fs.Bool("read", false, "poll for input reports concurrently, like the daemon")
	keyIndex := fs.Int("key", 0, "HID index of the key to upload to (13 = wide key)")
	playPause := fs.Int("playpause", 0, "instead of the timing sweep, alternate N Spotify play/pause icons (300ms apart)")
	recovery := fs.Bool("recovery", false, "instead of the timing sweep, test how the device recovers from interrupted transfers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sizeList, err := parseInts(*sizes)
	if err != nil {
		return fmt.Errorf("--sizes: %w", err)
	}
	gapList, err := parseInts(*gaps)
	if err != nil {
		return fmt.Errorf("--gaps: %w", err)
	}

	dev, err := openChecked()
	if err != nil {
		return err
	}
	defer dev.Close()
	if err := sendPage(dev, nil); err != nil {
		return err
	}
	pause(3 * time.Second)

	if *concurrentRead {
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				if raw, err := dev.ReadPacket(readPollTimeout); err == nil && len(raw) > 0 {
					if ev, err := protocol.ParseEvent(raw); err == nil {
						fmt.Fprintf(stdout, "  event %s\n", ev)
					}
				}
			}
		}()
	}

	dir, err := os.MkdirTemp("", "ulanzi-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	if *recovery {
		return probeRecovery(dev, dir, stdout)
	}
	if *playPause > 0 {
		return probePlayPause(dev, dir, *playPause, hidSlot(*keyIndex), stdout)
	}

	fmt.Fprintf(stdout, "%-8s %-8s %-8s %s\n", "packets", "gap_ms", "stalled", "durations")
	for _, target := range sizeList {
		variants, packets, err := probeVariants(dir, target)
		if err != nil {
			return err
		}
		for _, gapMS := range gapList {
			gap := time.Duration(gapMS) * time.Millisecond
			stalled := 0
			var durs []string
			for i := 0; i < *reps; i++ {
				btn := config.Button{At: hidSlot(*keyIndex), Icon: variants[i%len(variants)]}
				pkts, err := protocol.PartiallyUpdateButtonsFromConfig([]config.Button{btn})
				if err != nil {
					return err
				}
				start := time.Now()
				err = dev.SendPackets(pkts)
				took := time.Since(start)
				durs = append(durs, took.Round(time.Millisecond).String())
				if err != nil || took > probeStallThreshold {
					stalled++
				}
				pause(gap)
			}
			fmt.Fprintf(stdout, "%-8d %-8d %d/%-6d %s\n", packets, gapMS, stalled, *reps, strings.Join(durs, " "))
			if stalled > 0 {
				// Let the device drain before the next measurement.
				pause(8 * time.Second)
			}
			if stalled*2 > *reps {
				break // smaller gaps will only be worse
			}
		}
	}
	return nil
}

// probeVariants writes icons whose upload is about target packets and
// returns the actual packet count. Variants differ so the device has to
// process every upload.
func probeVariants(dir string, target int) ([]string, int, error) {
	for side := 8; side <= 196; side += 4 {
		var paths []string
		packets := 0
		for v := 0; v < 3; v++ {
			path := filepath.Join(dir, fmt.Sprintf("p%d_%d_%d.png", target, side, v))
			if err := writeNoiseIcon(path, side, int64(v)); err != nil {
				return nil, 0, err
			}
			pkts, err := protocol.PartiallyUpdateButtonsFromConfig([]config.Button{{At: [2]int{0, 0}, Icon: path}})
			if err != nil {
				return nil, 0, err
			}
			packets = len(pkts)
			paths = append(paths, path)
		}
		if packets >= target {
			return paths, packets, nil
		}
	}
	return nil, 0, errors.New("cannot build an upload that large")
}

// writeNoiseIcon draws random pixels in a side×side square on black;
// noise defeats JPEG compression, so the side controls the upload size.
func writeNoiseIcon(path string, side int, seed int64) error {
	rnd := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, 196, 196))
	for y := 0; y < 196; y++ {
		for x := 0; x < 196; x++ {
			c := color.RGBA{A: 255}
			if x < side && y < side {
				c = color.RGBA{uint8(rnd.Intn(256)), uint8(rnd.Intn(256)), uint8(rnd.Intn(256)), 255}
			}
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func parseInts(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		var v int
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &v); err != nil || v < 0 {
			return nil, fmt.Errorf("invalid number %q", part)
		}
		out = append(out, v)
	}
	return out, nil
}

// hidSlot maps a HID key index to [col, row].
func hidSlot(index int) [2]int {
	switch {
	case index < 10:
		return [2]int{index % 5, index / 5}
	case index == 13:
		return [2]int{3, 2}
	default:
		return [2]int{index - 10, 2}
	}
}

// probePlayPause alternates the exact images the spotify_play_pause
// widget renders. It reproduced the packet-boundary firmware bug (every
// upload after the 3064-byte pause ZIP stalled) and now checks the fix
// on real hardware.
func probePlayPause(dev deck, dir string, n int, at [2]int, stdout io.Writer) error {
	var paths []string
	for _, id := range []string{"lucide/play", "lucide/pause"} {
		img := image.NewRGBA(image.Rect(0, 0, 196, 196))
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xff
		}
		if err := icons.DrawPackIcon(img, id, image.Rect(34, 34, 162, 162), color.RGBA{0x1d, 0xb9, 0x54, 0xff}); err != nil {
			return err
		}
		path := filepath.Join(dir, strings.ReplaceAll(id, "/", "_")+".png")
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			return err
		}
		f.Close()
		paths = append(paths, path)
	}
	stalls := 0
	for i := 0; i < n; i++ {
		which := i % 2
		pkts, err := protocol.PartiallyUpdateButtonsFromConfig([]config.Button{{At: at, Icon: paths[which]}})
		if err != nil {
			return err
		}
		start := time.Now()
		err = dev.SendPackets(pkts)
		took := time.Since(start)
		if err != nil || took > probeStallThreshold {
			stalls++
			fmt.Fprintf(stdout, "%s %s (%d packets): %s err=%v  <-- STALL\n", start.Format("15:04:05.000"), []string{"play", "pause"}[which], len(pkts), took.Round(time.Millisecond), err)
		}
		pause(300 * time.Millisecond)
	}
	fmt.Fprintf(stdout, "%d play/pause uploads, %d stalls\n", n, stalls)
	return nil
}

// probeRecovery interrupts transfers in different ways and checks
// whether the next normal upload is accepted promptly. Restarting from
// the header (at once, after 1s or 3s) stalled every time; completing
// the interrupted upload with filler packets first never did - which is
// what device.SendPackets does.
func probeRecovery(dev deck, dir string, stdout io.Writer) error {
	small, _, err := probeVariants(dir, 5)
	if err != nil {
		return err
	}
	medium, _, err := probeVariants(dir, 9)
	if err != nil {
		return err
	}
	build := func(path string, at [2]int) [][protocol.PacketSize]byte {
		pkts, err := protocol.PartiallyUpdateButtonsFromConfig([]config.Button{{At: at, Icon: path}})
		if err != nil {
			panic(err)
		}
		return pkts
	}
	sendRaw := func(pkts [][protocol.PacketSize]byte) {
		for _, p := range pkts {
			if err := dev.SendPacket(p); err != nil {
				fmt.Fprintf(stdout, "    raw send error: %v\n", err)
				return
			}
		}
	}
	var zero [protocol.PacketSize]byte
	zeros := func(n int) [][protocol.PacketSize]byte {
		out := make([][protocol.PacketSize]byte, n)
		for i := range out {
			out[i] = zero
		}
		return out
	}
	checkN := 0
	check := func(label string) {
		checkN++
		pkts := build(small[checkN%len(small)], [2]int{1, 0})
		start := time.Now()
		err := dev.SendPackets(pkts)
		took := time.Since(start)
		mark := "ok"
		if err != nil || took > probeStallThreshold {
			mark = "STALL"
		}
		fmt.Fprintf(stdout, "%-48s next upload: %-6s %s err=%v\n", label, mark, took.Round(time.Millisecond), err)
		if mark == "STALL" {
			pause(8 * time.Second)
		} else {
			pause(500 * time.Millisecond)
		}
	}
	m := build(medium[0], [2]int{0, 0})
	n := len(m)

	for rep := 1; rep <= 3; rep++ {
		sendRaw(m[:5])
		sendRaw(m)
		check(fmt.Sprintf("H%d restart from header immediately", rep))
		sendRaw(m[:5])
		pause(time.Second)
		sendRaw(m)
		check(fmt.Sprintf("K%d abort 5/%d, wait 1s, resend", rep, n))
		sendRaw(m[:5])
		pause(3 * time.Second)
		sendRaw(m)
		check(fmt.Sprintf("L%d abort 5/%d, wait 3s, resend", rep, n))
		sendRaw(m[:5])
		sendRaw(zeros(n - 5))
		sendRaw(m)
		check(fmt.Sprintf("I%d abort 5/%d, zero-fill, resend", rep, n))
		sendRaw(m[:5])
		pause(3 * time.Second)
		check(fmt.Sprintf("M%d abort 5/%d, wait 3s, other upload", rep, n))
	}
	// A big multi-key upload (like 13 weather widgets) followed quickly
	// by a full page.
	var keys []config.Button
	for i := 0; i < 13; i++ {
		keys = append(keys, config.Button{At: hidSlot(i), Icon: medium[i%len(medium)]})
	}
	big, err := protocol.PartiallyUpdateButtonsFromConfig(keys)
	if err != nil {
		return err
	}
	page, err := protocol.SetButtonsFromConfig(keys[:11])
	if err != nil {
		return err
	}
	for _, gap := range []time.Duration{2 * time.Second, time.Second, 500 * time.Millisecond, 0} {
		start := time.Now()
		err := dev.SendPackets(big)
		tookBig := time.Since(start)
		pause(gap)
		start = time.Now()
		err2 := dev.SendPackets(page)
		tookPage := time.Since(start)
		mark := "ok"
		if err2 != nil || tookPage > probeStallThreshold {
			mark = "STALL"
		}
		fmt.Fprintf(stdout, "J big %d-packet upload (%s, err=%v), gap %s, full page %d packets: %-6s %s err=%v\n",
			len(big), tookBig.Round(time.Millisecond), err, gap, len(page), mark, tookPage.Round(time.Millisecond), err2)
		pause(8 * time.Second)
	}
	return nil
}
