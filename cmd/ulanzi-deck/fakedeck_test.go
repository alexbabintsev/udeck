package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
)

// sent is one transfer observed by fakeDeck.
type sent struct {
	cmd   protocol.Cmd
	slots []string // manifest keys for page transfers, sorted
	data  []byte   // payload of single-packet commands
}

// fakeDeck is an in-memory deck. Errors are consumed in order from the
// per-operation queues; nil entries mean success.
type fakeDeck struct {
	mu sync.Mutex

	sent       []sent
	sendErrs   []error // SendPacket + SendPackets share this queue
	reads      []readStep
	reconnects []reconnectStep
	reconnectN int
	tryN       int
	closed     int

	// onIdle runs when the read queue is empty (typically to cancel
	// the loop's context).
	onIdle func()
	// gate, when set, makes SendPackets wait for a value (a stalled
	// device).
	gate chan struct{}
}

type readStep struct {
	raw []byte
	err error
	// before runs right before the read is returned.
	before func()
}

type reconnectStep struct {
	reset bool
	err   error
}

func (f *fakeDeck) nextSendErr() error {
	if len(f.sendErrs) == 0 {
		return nil
	}
	err := f.sendErrs[0]
	f.sendErrs = f.sendErrs[1:]
	return err
}

func (f *fakeDeck) SendPacket(pkt [protocol.PacketSize]byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.nextSendErr(); err != nil {
		return err
	}
	cmd, n, _ := protocol.DecodeHeader(pkt[:])
	// Raw packets (probe) may be header frames of multi-packet uploads
	// or bare continuation chunks.
	n = min(n, protocol.FirstChunkSize)
	f.sent = append(f.sent, sent{cmd: cmd, data: append([]byte(nil), pkt[protocol.HeaderSize:protocol.HeaderSize+int(n)]...)})
	return nil
}

func (f *fakeDeck) SendPackets(pkts [][protocol.PacketSize]byte) error {
	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.nextSendErr(); err != nil {
		return err
	}
	cmd, total, _ := protocol.DecodeHeader(pkts[0][:])
	data := append([]byte(nil), pkts[0][protocol.HeaderSize:]...)
	for _, p := range pkts[1:] {
		data = append(data, p[:]...)
	}
	f.sent = append(f.sent, sent{cmd: cmd, slots: manifestSlots(data[:total])})
	return nil
}

func manifestSlots(data []byte) []string {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return []string{"<bad zip>"}
	}
	for _, f := range zr.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, _ := f.Open()
		raw, _ := io.ReadAll(rc)
		rc.Close()
		var m map[string]json.RawMessage
		_ = json.Unmarshal(raw, &m)
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}
	return nil
}

func (f *fakeDeck) ReadPacket(time.Duration) ([]byte, error) {
	f.mu.Lock()
	if len(f.reads) == 0 {
		idle := f.onIdle
		f.mu.Unlock()
		if idle != nil {
			idle()
		}
		return nil, nil
	}
	step := f.reads[0]
	f.reads = f.reads[1:]
	f.mu.Unlock()
	if step.before != nil {
		step.before()
	}
	return step.raw, step.err
}

func (f *fakeDeck) Reconnect() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reconnectN++
	if len(f.reconnects) == 0 {
		return false, nil
	}
	r := f.reconnects[0]
	f.reconnects = f.reconnects[1:]
	return r.reset, r.err
}

func (f *fakeDeck) TryReconnect() (bool, error) {
	f.mu.Lock()
	f.tryN++
	f.mu.Unlock()
	return f.Reconnect()
}

func (f *fakeDeck) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}

// cmds returns the command of every transfer since the last reset.
func (f *fakeDeck) cmds() []protocol.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]protocol.Cmd, len(f.sent))
	for i, s := range f.sent {
		out[i] = s.cmd
	}
	return out
}

func (f *fakeDeck) transfers(cmd protocol.Cmd) []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sent
	for _, s := range f.sent {
		if s.cmd == cmd {
			out = append(out, s)
		}
	}
	return out
}

func (f *fakeDeck) reset() {
	f.mu.Lock()
	f.sent = nil
	f.mu.Unlock()
}

func press(index int, pressed bool) []byte {
	p := byte(0)
	if pressed {
		p = 1
	}
	pkt := protocol.EncodePacket(protocol.CmdInButton, []byte{0, byte(index), 1, p})
	return pkt[:]
}

var errHID = errors.New("hid write: general error")

// writeConfig stores yaml in a temp dir and loads it.
func writeConfig(t *testing.T, yaml string) (string, *config.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return path, cfg
}

// noPause makes recovery/back-off sleeps instant for the test.
func noPause(t *testing.T) {
	t.Helper()
	old := pause
	pause = func(time.Duration) {}
	t.Cleanup(func() { pause = old })
}

// stubActions records external actions instead of running them and
// returns a snapshot accessor that is safe to call while the action
// worker is running.
func stubActions(t *testing.T) func() []config.Action {
	t.Helper()
	var mu sync.Mutex
	var got []config.Action
	old := runExternalAction
	runExternalAction = func(a *config.Action) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, *a)
		return nil
	}
	t.Cleanup(func() { runExternalAction = old })
	return func() []config.Action {
		mu.Lock()
		defer mu.Unlock()
		return append([]config.Action(nil), got...)
	}
}
