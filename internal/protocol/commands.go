package protocol

import (
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
