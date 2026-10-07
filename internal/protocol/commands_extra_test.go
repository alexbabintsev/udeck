package protocol

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"testing"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

func TestSetBrightnessPacket(t *testing.T) {
	pkt, err := SetBrightnessPacket(55)
	if err != nil {
		t.Fatal(err)
	}
	cmd, n, _ := DecodeHeader(pkt[:])
	if cmd != CmdSetBrightness || n != 2 || string(pkt[8:10]) != "55" {
		t.Fatalf("unexpected packet cmd=%v n=%d", cmd, n)
	}
	for _, bad := range []int{-1, 101} {
		if _, err := SetBrightnessPacket(bad); err == nil {
			t.Fatalf("expected error for %d", bad)
		}
	}
}

func TestSetLabelStylePacketDefaultsAndOverrides(t *testing.T) {
	pkt, err := SetLabelStylePacket(nil)
	if err != nil {
		t.Fatal(err)
	}
	style := decodeStyle(t, pkt)
	if style["Align"] != "bottom" || style["FontName"] != "Roboto" || style["Color"] != float64(0xFFFFFF) {
		t.Fatalf("unexpected defaults %v", style)
	}

	show := false
	size, weight := 8, 50
	pkt, err = SetLabelStylePacket(&config.LabelStyle{
		Align: "top", Color: "00FF00", FontName: "Inter", ShowTitle: &show, Size: &size, Weight: &weight,
	})
	if err != nil {
		t.Fatal(err)
	}
	style = decodeStyle(t, pkt)
	if style["Align"] != "top" || style["Color"] != float64(0x00FF00) || style["FontName"] != "Inter" ||
		style["ShowTitle"] != false || style["Size"] != float64(8) || style["Weight"] != float64(50) {
		t.Fatalf("overrides not applied: %v", style)
	}
}

func decodeStyle(t *testing.T, pkt [PacketSize]byte) map[string]any {
	t.Helper()
	cmd, n, _ := DecodeHeader(pkt[:])
	if cmd != CmdSetLabelStyle {
		t.Fatalf("cmd = %v", cmd)
	}
	var out map[string]any
	if err := json.Unmarshal(pkt[HeaderSize:HeaderSize+int(n)], &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// reassemble rebuilds the ZIP payload from a multi-packet transfer.
func reassemble(t *testing.T, pkts [][PacketSize]byte, want Cmd) []byte {
	t.Helper()
	cmd, total, err := DecodeHeader(pkts[0][:])
	if err != nil || cmd != want {
		t.Fatalf("header cmd=%v err=%v", cmd, err)
	}
	data := append([]byte(nil), pkts[0][HeaderSize:]...)
	for _, p := range pkts[1:] {
		data = append(data, p[:]...)
	}
	return data[:total]
}

func readManifest(t *testing.T, data []byte) map[string]buttonSlot {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, _ := f.Open()
		raw, _ := io.ReadAll(rc)
		rc.Close()
		var m map[string]buttonSlot
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	t.Fatal("manifest.json missing")
	return nil
}

func TestEmptyPageHasAllPhysicalSlots(t *testing.T) {
	data, err := PageZIP(nil)
	if err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, data)
	if len(m) != 14 {
		t.Fatalf("expected 14 slots, got %d", len(m))
	}
	if _, ok := m["4_2"]; ok {
		t.Fatal("slot 4_2 must not exist")
	}
}

func TestSetButtonsFromConfigRoundTrip(t *testing.T) {
	pkts, err := SetButtonsFromConfig([]config.Button{{At: [2]int{1, 0}, Label: "Hi"}})
	if err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, reassemble(t, pkts, CmdSetButtons))
	if m["1_0"].ViewParam[0].Text != "Hi" {
		t.Fatalf("label missing: %+v", m["1_0"])
	}
	if len(m) != 14 {
		t.Fatalf("full page should list all slots, got %d", len(m))
	}
}

func TestPartiallyUpdateButtonsFromConfigOnlyListsGivenButtons(t *testing.T) {
	pkts, err := PartiallyUpdateButtonsFromConfig([]config.Button{{At: [2]int{2, 1}, Label: "X", Icon: "lucide/play"}})
	if err != nil {
		t.Fatal(err)
	}
	data := reassemble(t, pkts, CmdPartiallyUpdateButtons)
	m := readManifest(t, data)
	if len(m) != 1 || m["2_1"].ViewParam[0].Icon == "" {
		t.Fatalf("unexpected partial manifest %+v", m)
	}
	if !zipChunksValid(data) {
		t.Fatal("chunk boundary bytes must be valid")
	}
}

func TestMultiPacketChunking(t *testing.T) {
	data := bytes.Repeat([]byte{0x41}, FirstChunkSize+PacketSize+5)
	for _, pkts := range [][][PacketSize]byte{SetButtonsPackets(data), PartiallyUpdateButtonsPackets(data)} {
		if len(pkts) != 3 {
			t.Fatalf("expected 3 packets, got %d", len(pkts))
		}
		if got := binary.LittleEndian.Uint32(pkts[0][4:8]); int(got) != len(data) {
			t.Fatalf("total length = %d", got)
		}
		if pkts[2][4] != 0x41 || pkts[2][5] != 0 {
			t.Fatal("last chunk should be zero padded after 5 bytes")
		}
	}
}

func TestZipChunksValid(t *testing.T) {
	data := bytes.Repeat([]byte{0x41}, FirstChunkSize+PacketSize+1)
	if !zipChunksValid(data) {
		t.Fatal("expected valid")
	}
	data[FirstChunkSize] = 0x7C
	if zipChunksValid(data) {
		t.Fatal("0x7C at chunk boundary must be invalid")
	}
	data[FirstChunkSize] = 0x41
	data[FirstChunkSize+PacketSize] = 0x00
	if zipChunksValid(data) {
		t.Fatal("0x00 at chunk boundary must be invalid")
	}
}

func TestRandomASCII(t *testing.T) {
	b := randomASCII(64)
	if len(b) != 64 {
		t.Fatal("wrong length")
	}
	for _, c := range b {
		if !bytes.ContainsRune([]byte(asciiChars), rune(c)) {
			t.Fatalf("non-ascii char %q", c)
		}
	}
}

func TestParseHexColor(t *testing.T) {
	if c, err := parseHexColor(" #FF8000 "); err != nil || c != 0xFF8000 {
		t.Fatalf("got %x %v", c, err)
	}
	for _, bad := range []string{"FFF", "GGGGGG"} {
		if _, err := parseHexColor(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestPageZIPReportsIconErrors(t *testing.T) {
	if _, err := PageZIP([]config.Button{{At: [2]int{0, 0}, Icon: "/nonexistent/icon.png"}}); err == nil {
		t.Fatal("expected icon error")
	}
}
