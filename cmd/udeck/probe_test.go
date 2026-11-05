//go:build personal

package main

import (
	"strings"
	"testing"

	"github.com/alexbabintsev/udeck/internal/protocol"
)

func TestProbeSweepAndPlayPause(t *testing.T) {
	dev := &fakeDeck{}
	stubCLI(t, dev)
	code, stdout, stderr := cli("probe", "--sizes", "3", "--gaps", "10,0", "--reps", "2", "--key", "13", "--read")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "packets") || strings.Count(stdout, "\n") < 3 {
		t.Fatalf("unexpected output %q", stdout)
	}
	if n := len(dev.transfers(protocol.CmdPartiallyUpdateButtons)); n != 4 {
		t.Fatalf("expected 4 probe uploads, got %d", n)
	}

	dev.reset()
	code, stdout, _ = cli("probe", "--playpause", "4", "--key", "10")
	if code != 0 || !strings.Contains(stdout, "4 play/pause uploads, 0 stalls") {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	dev.reset()
	if code, stdout, _ := cli("probe", "--recovery"); code != 0 || !strings.Contains(stdout, "I3 abort") || !strings.Contains(stdout, "J big") {
		t.Fatalf("recovery probe: code=%d out=%q", code, stdout)
	}
	if code, _, _ := cli("probe", "--sizes", "x"); code != 1 {
		t.Fatal("bad --sizes must fail")
	}
	if hidSlot(3) != [2]int{3, 0} || hidSlot(7) != [2]int{2, 1} || hidSlot(11) != [2]int{1, 2} || hidSlot(13) != [2]int{3, 2} {
		t.Fatal("hidSlot")
	}
}
