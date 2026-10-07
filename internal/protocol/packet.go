package protocol

import (
	"encoding/binary"
	"errors"
)

const (
	PacketSize     = 1024
	HeaderSize     = 8
	FirstChunkSize = PacketSize - HeaderSize // 1016
)

var Header = [2]byte{0x7C, 0x7C}

// Cmd is a 16-bit command identifier transmitted big-endian on the wire.
type Cmd uint16

const (
	CmdSetButtons             Cmd = 0x0001
	CmdSetSmallWindowData     Cmd = 0x0006
	CmdSetBrightness          Cmd = 0x000A
	CmdSetLabelStyle          Cmd = 0x000B
	CmdPartiallyUpdateButtons Cmd = 0x000D
	CmdInButton               Cmd = 0x0101
	CmdInButton2              Cmd = 0x0102
	CmdInLabelStyleAck        Cmd = 0x010B
	CmdInDeviceInfo           Cmd = 0x0303
)

// EncodePacket builds a single 1024-byte HID frame for cmd with the given
// payload. The wire format is:
//
//	bytes 0..1   : header 0x7C 0x7C
//	bytes 2..3   : cmd, big-endian uint16
//	bytes 4..7   : total payload length, little-endian uint32
//	bytes 8..1023: payload, zero-padded
//
// For multi-packet transfers the caller is responsible for slicing the
// payload into a 1016-byte first chunk plus 1024-byte raw continuation
// packets — see EncodeContinuation.
//
// TODO(v2): when implementing CmdSetButtons / CmdPartiallyUpdateButtons,
// remember the device-side parser quirk: bytes at offset 1016 + n*1024 of
// the ZIP payload must not be 0x00 or 0x7C. Workaround is to pad a
// dummy.txt entry inside the ZIP with random ASCII until all critical
// offsets are clean.
func EncodePacket(cmd Cmd, payload []byte) [PacketSize]byte {
	var buf [PacketSize]byte
	buf[0] = Header[0]
	buf[1] = Header[1]
	binary.BigEndian.PutUint16(buf[2:4], uint16(cmd))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(payload)))
	chunk := payload
	if len(chunk) > FirstChunkSize {
		chunk = chunk[:FirstChunkSize]
	}
	copy(buf[HeaderSize:], chunk)
	return buf
}

// EncodeContinuation builds a raw 1024-byte continuation packet for the
// remainder of a multi-packet transfer. chunk must be at most PacketSize
// bytes; remaining bytes are zero-padded.
func EncodeContinuation(chunk []byte) [PacketSize]byte {
	var buf [PacketSize]byte
	if len(chunk) > PacketSize {
		chunk = chunk[:PacketSize]
	}
	copy(buf[:], chunk)
	return buf
}

// DecodeHeader parses the 8-byte frame header from buf. Returns an error
// if buf is too short or the magic header bytes don't match.
func DecodeHeader(buf []byte) (cmd Cmd, length uint32, err error) {
	if len(buf) < HeaderSize {
		return 0, 0, errors.New("packet too short")
	}
	if buf[0] != Header[0] || buf[1] != Header[1] {
		return 0, 0, errors.New("bad packet header")
	}
	cmd = Cmd(binary.BigEndian.Uint16(buf[2:4]))
	length = binary.LittleEndian.Uint32(buf[4:8])
	return cmd, length, nil
}
