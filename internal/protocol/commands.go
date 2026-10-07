package protocol

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
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

// SetLabelStylePacket builds the OUT_SET_LABEL_STYLE frame from config.
// Unset fields fall back to device defaults.
func SetLabelStylePacket(s *config.LabelStyle) ([PacketSize]byte, error) {
	style := map[string]any{
		"Align":     "bottom",
		"Color":     0xFFFFFF,
		"FontName":  "Roboto",
		"ShowTitle": true,
		"Size":      10,
		"Weight":    80,
	}
	if s != nil {
		if s.Align != "" {
			style["Align"] = s.Align
		}
		if s.Color != "" {
			c, err := parseHexColor(s.Color)
			if err != nil {
				return [PacketSize]byte{}, fmt.Errorf("invalid label color: %w", err)
			}
			style["Color"] = c
		}
		if s.FontName != "" {
			style["FontName"] = s.FontName
		}
		if s.ShowTitle != nil {
			style["ShowTitle"] = *s.ShowTitle
		}
		if s.Size != nil {
			style["Size"] = *s.Size
		}
		if s.Weight != nil {
			style["Weight"] = *s.Weight
		}
	}
	payload, err := json.Marshal(style)
	if err != nil {
		return [PacketSize]byte{}, err
	}
	return EncodePacket(CmdSetLabelStyle, payload), nil
}

// SetButtonsFromConfig builds a page ZIP from config buttons and encodes
// it as OUT_SET_BUTTONS packets.
func SetButtonsFromConfig(buttons []config.Button) ([][PacketSize]byte, error) {
	zipData, err := PageZIP(buttons)
	if err != nil {
		return nil, err
	}
	return SetButtonsPackets(zipData), nil
}

func PartiallyUpdateButtonsPackets(zipData []byte) [][PacketSize]byte {
	first := EncodePacket(CmdPartiallyUpdateButtons, zipData)
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

func PartiallyUpdateButtonsFromConfig(buttons []config.Button) ([][PacketSize]byte, error) {
	zipData, err := PartialPageZIP(buttons)
	if err != nil {
		return nil, err
	}
	return PartiallyUpdateButtonsPackets(zipData), nil
}

func parseHexColor(s string) (uint64, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return 0, fmt.Errorf("must be exactly 6 hex digits")
	}
	c, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("must be valid hex RRGGBB")
	}
	return c, nil
}
