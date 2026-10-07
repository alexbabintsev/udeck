package protocol

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
)

// ButtonSlot is the per-key payload written into the ZIP manifest.
type buttonSlot struct {
	State     int         `json:"State"`
	ViewParam []viewParam `json:"ViewParam"`
}

type viewParam struct {
	Text string `json:"Text,omitempty"`
	Icon string `json:"Icon,omitempty"`
}

// PageZIP builds a ZIP payload for the given button definitions.
// Buttons not in the list default to empty slots. Icon files are loaded,
// resized to 196×196, and embedded as JPEG. Returns an error if any icon
// fails to load.
func PageZIP(buttons []config.Button) ([]byte, error) {
	return pageZIP(buttons, true)
}

func PartialPageZIP(buttons []config.Button) ([]byte, error) {
	return pageZIP(buttons, false)
}

func pageZIP(buttons []config.Button, full bool) ([]byte, error) {
	manifest := map[string]buttonSlot{}
	iconData := map[string][]byte{} // icon filename → JPEG bytes

	if full {
		// Build all physical slots. [4,2] does not exist because [3,2] is
		// the device's double-wide button.
		for row := 0; row < 3; row++ {
			for col := 0; col < 5; col++ {
				if col == 4 && row == 2 {
					continue
				}
				manifest[fmt.Sprintf("%d_%d", col, row)] = buttonSlot{
					State:     0,
					ViewParam: []viewParam{{}},
				}
			}
		}
	}

	for _, btn := range buttons {
		col, row := btn.At[0], btn.At[1]
		key := fmt.Sprintf("%d_%d", col, row)
		vp := viewParam{}

		if btn.Label != "" {
			vp.Text = btn.Label
		}
		if btn.Icon != "" {
			jpeg, err := icons.LoadForButton(btn.Icon, iconInsets(btn.IconMargins), btn.At, btn.IconColor)
			if err != nil {
				return nil, fmt.Errorf("button [%d,%d] icon: %w", col, row, err)
			}
			iconFile := fmt.Sprintf("btn_%d_%d_%08x.jpg", col, row, buttonIconID(btn, jpeg))
			iconData[iconFile] = jpeg
			vp.Icon = "icons/" + iconFile
		}

		manifest[key] = buttonSlot{State: 0, ViewParam: []viewParam{vp}}
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	// Each attempt adds one more random byte to the dummy, shifting all
	// icon DEFLATE streams by one position relative to chunk boundaries.
	// Worst case: lcm(1024, iconData) attempts — in practice < 1024.
	for size := 0; size <= 1024; size++ {
		var dummy []byte
		if size > 0 {
			dummy = randomASCII(size)
		}
		data, err := buildPageZIPRaw(manifestJSON, iconData, dummy)
		if err != nil {
			return nil, err
		}
		if zipChunksValid(data) {
			return data, nil
		}
	}
	return nil, fmt.Errorf("could not build valid ZIP after 1024 padding attempts")
}

const asciiChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func randomASCII(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = asciiChars[rand.Intn(len(asciiChars))]
	}
	return b
}

// zipChunksValid reports whether the device parses data correctly when
// it is split into packets:
//
//   - a continuation packet must not start with 0x00 or 0x7C;
//   - the ZIP must not end exactly on a packet boundary. The D200 then
//     keeps waiting for more data and swallows the next transfer as the
//     tail of this one: that upload is accepted at ~1 packet/s and its
//     image is lost or late (measured with `ulanzi-deck probe
//     --playpause`: every upload following a 3064-byte ZIP stalled for
//     0.7-3.4s, none followed any other size).
func zipChunksValid(data []byte) bool {
	if len(data) >= FirstChunkSize && (len(data)-FirstChunkSize)%PacketSize == 0 {
		return false
	}
	for i := FirstChunkSize; i < len(data); i += PacketSize {
		if data[i] == 0x00 || data[i] == 0x7C {
			return false
		}
	}
	return true
}

func buildPageZIPRaw(manifest []byte, iconData map[string][]byte, dummy []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	// dummy must be written first so changing its size actually shifts the
	// following file payloads relative to packet boundaries.
	df, err := w.Create("dummy.txt")
	if err != nil {
		return nil, err
	}
	if _, err := df.Write(dummy); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(iconData))
	for name := range iconData {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		f, err := w.Create("icons/" + name)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(iconData[name]); err != nil {
			return nil, err
		}
	}

	mf, err := w.Create("manifest.json")
	if err != nil {
		return nil, err
	}
	if _, err := mf.Write(manifest); err != nil {
		return nil, err
	}

	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buttonIconID(btn config.Button, jpeg []byte) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(btn.Icon))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(btn.IconColor))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(btn.Label))
	_, _ = h.Write([]byte{0})
	if btn.IconMargins != nil {
		_, _ = h.Write([]byte{
			byte(btn.IconMargins.Top),
			byte(btn.IconMargins.Right),
			byte(btn.IconMargins.Bottom),
			byte(btn.IconMargins.Left),
		})
		_, _ = h.Write([]byte{0})
	}
	_, _ = h.Write(jpeg)
	return h.Sum32()
}

func iconInsets(in *config.Insets) icons.Insets {
	if in == nil {
		return icons.Insets{}
	}
	return icons.Insets{
		Top:    in.Top,
		Right:  in.Right,
		Bottom: in.Bottom,
		Left:   in.Left,
	}
}
