package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alexbabintsev/udeck/internal/config"
)

func TestSetLabelStylePacketRejectsInvalidColor(t *testing.T) {
	_, err := SetLabelStylePacket(&config.LabelStyle{Color: "GGGGGG"})
	if err == nil {
		t.Fatal("expected invalid color error")
	}
	if !strings.Contains(err.Error(), "invalid label color") {
		t.Fatalf("expected invalid label color error, got %v", err)
	}
}

func TestSetLabelStylePacketParsesHashPrefixedColor(t *testing.T) {
	pkt, err := SetLabelStylePacket(&config.LabelStyle{Color: "#A1B2C3"})
	if err != nil {
		t.Fatalf("expected valid packet, got %v", err)
	}

	payloadLen := int(pkt[4]) | int(pkt[5])<<8 | int(pkt[6])<<16 | int(pkt[7])<<24
	payload := pkt[HeaderSize : HeaderSize+payloadLen]

	var style map[string]any
	if err := json.Unmarshal(payload, &style); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := style["Color"]; got != float64(0xA1B2C3) {
		t.Fatalf("expected color %#x, got %#v", 0xA1B2C3, got)
	}
}
