package protocol

import (
	"fmt"
	"strconv"

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

// EmptyPageZIP builds a minimal ZIP with 15 empty button slots.
// Switches the device out of firmware UI mode so IN_BUTTON frames flow.
func EmptyPageZIP() ([]byte, error) {
	return PageZIP(nil)
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

// SetButtonsFromConfig builds a page ZIP from config buttons and encodes
// it as OUT_SET_BUTTONS packets.
func SetButtonsFromConfig(buttons []config.Button) ([][PacketSize]byte, error) {
	zipData, err := PageZIP(buttons)
	if err != nil {
		return nil, err
	}
	return SetButtonsPackets(zipData), nil
}
