package protocol

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
)

// ButtonSlot is the per-key payload written into the ZIP manifest.
type buttonSlot struct {
	State     int           `json:"State"`
	ViewParam []viewParam   `json:"ViewParam"`
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
	manifest := map[string]buttonSlot{}
	iconData := map[string][]byte{} // icon filename → JPEG bytes

	// Build all 15 slots
	for row := 0; row < 3; row++ {
		for col := 0; col < 5; col++ {
			manifest[fmt.Sprintf("%d_%d", col, row)] = buttonSlot{
				State:     0,
				ViewParam: []viewParam{{}},
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
			iconFile := fmt.Sprintf("btn_%d_%d.jpg", col, row)
			jpeg, err := icons.Load(btn.Icon)
			if err != nil {
				return nil, fmt.Errorf("button [%d,%d] icon: %w", col, row, err)
			}
			iconData[iconFile] = jpeg
			vp.Icon = "icons/" + iconFile
		}

		manifest[key] = buttonSlot{State: 0, ViewParam: []viewParam{vp}}
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	for pad := 0; pad <= 4096; pad++ {
		data, err := buildPageZIPRaw(manifestJSON, iconData, bytes.Repeat([]byte(" "), pad))
		if err != nil {
			return nil, err
		}
		if zipChunksValid(data) {
			return data, nil
		}
	}
	return nil, fmt.Errorf("could not build valid ZIP after padding attempts")
}

// zipChunksValid returns false if any chunk-boundary byte is 0x00 or 0x7C.
func zipChunksValid(data []byte) bool {
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

	mf, err := w.Create("page/manifest.json")
	if err != nil {
		return nil, err
	}
	if _, err := mf.Write(manifest); err != nil {
		return nil, err
	}

	for name, data := range iconData {
		f, err := w.Create("page/icons/" + name)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(data); err != nil {
			return nil, err
		}
	}

	if len(dummy) > 0 {
		df, err := w.Create("page/dummy.txt")
		if err != nil {
			return nil, err
		}
		if _, err := df.Write(dummy); err != nil {
			return nil, err
		}
	}

	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
