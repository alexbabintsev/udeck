package protocol

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strconv"
)

// SetBrightnessPacket builds the OUT_SET_BRIGHTNESS frame. Level is
// clamped to [0, 100]. Wire payload is the level rendered as an ASCII
// decimal string.
func SetBrightnessPacket(level int) ([PacketSize]byte, error) {
	if level < 0 || level > 100 {
		return [PacketSize]byte{}, fmt.Errorf("brightness out of range: %d", level)
	}
	payload := []byte(strconv.Itoa(level))
	return EncodePacket(CmdSetBrightness, payload), nil
}

// EmptyPageZIP builds a minimal ZIP payload that registers 15 empty
// button slots (5 cols × 3 rows) with the D200. Sending this via
// SetButtonsPackets switches the device out of firmware UI mode so that
// it begins emitting IN_BUTTON HID frames.
//
// The ZIP must contain page/manifest.json. A page/dummy.txt is padded
// with spaces until no chunk-boundary byte (offsets 1016, 2040, …) is
// 0x00 or 0x7C — a device-side parser quirk documented in packet.go.
func EmptyPageZIP() ([]byte, error) {
	manifest := buildManifestJSON(5, 3)
	for pad := 0; pad <= 4096; pad++ {
		data, err := buildZIP(manifest, bytes.Repeat([]byte(" "), pad))
		if err != nil {
			return nil, err
		}
		if zipChunksValid(data) {
			return data, nil
		}
	}
	return nil, fmt.Errorf("could not build valid ZIP after padding attempts")
}

func buildManifestJSON(cols, rows int) []byte {
	var b bytes.Buffer
	b.WriteString("{")
	first := true
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			if !first {
				b.WriteString(",")
			}
			first = false
			fmt.Fprintf(&b, `"%d_%d":{"State":0,"ViewParam":[{}]}`, col, row)
		}
	}
	b.WriteString("}")
	return b.Bytes()
}

func buildZIP(manifest, dummy []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	mf, err := w.Create("page/manifest.json")
	if err != nil {
		return nil, err
	}
	if _, err := mf.Write(manifest); err != nil {
		return nil, err
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

// zipChunksValid returns false if any chunk-boundary byte is 0x00 or 0x7C.
func zipChunksValid(data []byte) bool {
	for i := FirstChunkSize; i < len(data); i += PacketSize {
		if data[i] == 0x00 || data[i] == 0x7C {
			return false
		}
	}
	return true
}

// SetButtonsPackets encodes a ZIP payload as the multi-packet
// OUT_SET_BUTTONS sequence. The first packet carries the 8-byte header
// and up to 1016 bytes of payload; subsequent packets are raw 1024-byte
// continuation chunks.
func SetButtonsPackets(zipData []byte) [][PacketSize]byte {
	first := EncodePacket(CmdSetButtons, zipData)
	pkts := [][PacketSize]byte{first}
	for i := FirstChunkSize; i < len(zipData); i += PacketSize {
		end := i + PacketSize
		if end > len(zipData) {
			end = len(zipData)
		}
		pkts = append(pkts, EncodeContinuation(zipData[i:end]))
	}
	return pkts
}
