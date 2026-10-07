package debug

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestLogfOnlyWhenEnabled(t *testing.T) {
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		SetEnabled(false)
	}()

	SetEnabled(false)
	Logf("hidden %d", 1)
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
	SetEnabled(true)
	if !Enabled() {
		t.Fatal("expected enabled")
	}
	Logf("shown %d", 2)
	if got := buf.String(); !strings.Contains(got, "[debug] shown 2") {
		t.Fatalf("got %q", got)
	}
}
