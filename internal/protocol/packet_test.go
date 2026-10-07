package protocol

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestEncodePacketLayout(t *testing.T) {
	pkt := EncodePacket(CmdSetBrightness, []byte("70"))
	if pkt[0] != 0x7C || pkt[1] != 0x7C {
		t.Fatalf("bad magic: % x", pkt[:2])
	}
	if got := binary.BigEndian.Uint16(pkt[2:4]); got != uint16(CmdSetBrightness) {
		t.Fatalf("cmd = %04x", got)
	}
	if got := binary.LittleEndian.Uint32(pkt[4:8]); got != 2 {
		t.Fatalf("length = %d", got)
	}
	if string(pkt[8:10]) != "70" || pkt[10] != 0 {
		t.Fatalf("payload not copied/padded: % x", pkt[8:12])
	}
}

func TestEncodePacketTruncatesToFirstChunkButKeepsTotalLength(t *testing.T) {
	payload := bytes.Repeat([]byte{0xAB}, FirstChunkSize+50)
	pkt := EncodePacket(CmdSetButtons, payload)
	if got := binary.LittleEndian.Uint32(pkt[4:8]); int(got) != len(payload) {
		t.Fatalf("length = %d, want %d", got, len(payload))
	}
	if pkt[PacketSize-1] != 0xAB {
		t.Fatal("first chunk should fill the packet")
	}
}

func TestEncodeContinuationPadsAndTruncates(t *testing.T) {
	short := EncodeContinuation([]byte{1, 2, 3})
	if short[0] != 1 || short[2] != 3 || short[3] != 0 {
		t.Fatalf("unexpected short continuation: % x", short[:4])
	}
	long := EncodeContinuation(bytes.Repeat([]byte{9}, PacketSize+10))
	if long[PacketSize-1] != 9 {
		t.Fatal("expected full continuation")
	}
}

func TestDecodeHeader(t *testing.T) {
	pkt := EncodePacket(CmdInButton, []byte{1, 2, 3, 4})
	cmd, n, err := DecodeHeader(pkt[:])
	if err != nil || cmd != CmdInButton || n != 4 {
		t.Fatalf("DecodeHeader = %v %d %v", cmd, n, err)
	}
	if _, _, err := DecodeHeader([]byte{0x7C}); err == nil {
		t.Fatal("expected short packet error")
	}
	if _, _, err := DecodeHeader([]byte{0, 0, 0, 0, 0, 0, 0, 0}); err == nil {
		t.Fatal("expected bad header error")
	}
}

func TestParseEventButton(t *testing.T) {
	pkt := EncodePacket(CmdInButton, []byte{200, 13, 1, 1})
	ev, err := ParseEvent(pkt[:])
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != EventButton || ev.Index != 13 || ev.State != 200 || !ev.Pressed {
		t.Fatalf("unexpected event %+v", ev)
	}
	if !strings.Contains(ev.String(), "BUTTON index=13 state=200 press") {
		t.Fatalf("String() = %q", ev.String())
	}

	pkt = EncodePacket(CmdInButton2, []byte{0, 4, 1, 0})
	ev, _ = ParseEvent(pkt[:])
	if ev.Pressed || !strings.Contains(ev.String(), "BUTTON_2 index=4 state=0 release") {
		t.Fatalf("String() = %q", ev.String())
	}
}

func TestParseEventButtonTooShort(t *testing.T) {
	raw := EncodePacket(CmdInButton, []byte{1, 2})
	if _, err := ParseEvent(raw[:HeaderSize+2]); err == nil {
		t.Fatal("expected short payload error")
	}
}

func TestParseEventDeviceInfoTrimsPadding(t *testing.T) {
	pkt := EncodePacket(CmdInDeviceInfo, []byte("D200 v1.2\x00\x00"))
	ev, err := ParseEvent(pkt[:])
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != EventDeviceInfo || ev.Info != "D200 v1.2" {
		t.Fatalf("unexpected info %+v", ev)
	}
	if ev.String() != `DEVICE_INFO "D200 v1.2"` {
		t.Fatalf("String() = %q", ev.String())
	}
}

func TestParseEventLabelAckAndUnknown(t *testing.T) {
	pkt := EncodePacket(CmdInLabelStyleAck, nil)
	ev, _ := ParseEvent(pkt[:])
	if ev.Kind != EventLabelStyleAck || ev.String() != "LABEL_STYLE_ACK" {
		t.Fatalf("unexpected %+v", ev)
	}
	pkt = EncodePacket(Cmd(0x0999), []byte{1, 2, 3})
	ev, _ = ParseEvent(pkt[:])
	if ev.Kind != EventUnknown || ev.String() != "UNKNOWN cmd=0999 len=3" {
		t.Fatalf("String() = %q", ev.String())
	}
}

func TestParseEventClampsBogusLength(t *testing.T) {
	raw := []byte{0x7C, 0x7C, 0x09, 0x99, 0xFF, 0xFF, 0xFF, 0x7F, 1, 2}
	ev, err := ParseEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Payload) != 2 {
		t.Fatalf("payload len = %d", len(ev.Payload))
	}
	if _, err := ParseEvent([]byte{1, 2}); err == nil {
		t.Fatal("expected header error")
	}
}

func TestEventStringUnknownButtonCmd(t *testing.T) {
	ev := Event{Kind: EventButton, Cmd: Cmd(0x0103), Index: 1}
	if !strings.HasPrefix(ev.String(), "BUTTON_CMD_0103") {
		t.Fatalf("String() = %q", ev.String())
	}
}
