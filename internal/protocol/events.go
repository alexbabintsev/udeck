package protocol

import "fmt"

// EventKind tags the variant of an inbound event.
type EventKind int

const (
	EventUnknown EventKind = iota
	EventButton
	EventDeviceInfo
)

// Event is the parsed form of an inbound HID frame.
type Event struct {
	Kind EventKind
	Cmd  Cmd

	// Button events
	State   uint8
	Index   uint8 // 0..13; index 13 is the wide [3,2] button
	Pressed bool

	// DeviceInfo events
	Info string

	// Raw payload (offset 8 of the frame), trimmed to declared length when
	// possible. Useful for debugging unknown commands.
	Payload []byte
}

func (e Event) String() string {
	switch e.Kind {
	case EventButton:
		var label string
		switch e.Cmd {
		case CmdInButton:
			label = "BUTTON"
		case CmdInButton2:
			label = "BUTTON_2"
		default:
			label = fmt.Sprintf("BUTTON_CMD_%04X", uint16(e.Cmd))
		}
		action := "release"
		if e.Pressed {
			action = "press"
		}
		return fmt.Sprintf("%s index=%d state=%d %s", label, e.Index, e.State, action)
	case EventDeviceInfo:
		return fmt.Sprintf("DEVICE_INFO %q", e.Info)
	default:
		return fmt.Sprintf("UNKNOWN cmd=%04X len=%d", uint16(e.Cmd), len(e.Payload))
	}
}

// ParseEvent decodes a raw HID frame into an Event. Returns an error
// only when the header is malformed; unknown command codes are reported
// as EventUnknown.
func ParseEvent(raw []byte) (Event, error) {
	cmd, length, err := DecodeHeader(raw)
	if err != nil {
		return Event{}, err
	}
	end := HeaderSize + int(length)
	if end > len(raw) || end < HeaderSize {
		end = len(raw)
	}
	payload := raw[HeaderSize:end]

	ev := Event{Cmd: cmd, Payload: payload}
	switch cmd {
	case CmdInButton, CmdInButton2:
		// Layout (verified empirically against strmdck):
		//   payload[0] = state, payload[1] = index, payload[2] = 0x01,
		//   payload[3] = pressed (0x00 release / 0x01 press).
		if len(payload) < 4 {
			return ev, fmt.Errorf("button payload too short: %d", len(payload))
		}
		ev.Kind = EventButton
		ev.State = payload[0]
		ev.Index = payload[1]
		ev.Pressed = payload[3] != 0
	case CmdInDeviceInfo:
		ev.Kind = EventDeviceInfo
		// trim trailing zeros — the device pads the ASCII string.
		s := payload
		for i := len(s) - 1; i >= 0; i-- {
			if s[i] != 0 {
				s = s[:i+1]
				break
			}
		}
		ev.Info = string(s)
	default:
		ev.Kind = EventUnknown
	}
	return ev, nil
}
