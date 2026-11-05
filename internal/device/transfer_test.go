package device

import (
	"errors"
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/protocol"
)

type readResult struct {
	data []byte
	err  error
}

// fakeHandle records successfully written frames, fails selected write
// calls (1-based) with the given error and replays queued reads.
type fakeHandle struct {
	calls     int
	failOn    map[int]error
	shortOn   map[int]bool
	frames    []byte // first payload byte of every accepted frame
	reads     []readResult
	closed    bool
	closeErr  error
	lastWrite []byte
}

func (f *fakeHandle) Write(b []byte) (int, error) {
	f.calls++
	if err := f.failOn[f.calls]; err != nil {
		return -1, err
	}
	if f.shortOn[f.calls] {
		return len(b) - 1, nil
	}
	f.lastWrite = append([]byte(nil), b...)
	f.frames = append(f.frames, b[1])
	return len(b), nil
}

func (f *fakeHandle) ReadWithTimeout(b []byte, _ time.Duration) (int, error) {
	if len(f.reads) == 0 {
		return 0, nil
	}
	r := f.reads[0]
	f.reads = f.reads[1:]
	if r.err != nil {
		return -1, r.err
	}
	return copy(b, r.data), nil
}

func (f *fakeHandle) Close() error {
	f.closed = true
	return f.closeErr
}

// fakeEnv replaces the hidapi seams with a scripted opener and a fake
// clock that advances on every sleep.
type fakeEnv struct {
	opens   []func() (hidHandle, string, error)
	opened  int
	clock   time.Time
	slept   time.Duration
	inited  int
	initErr error
}

func installFakeEnv(t *testing.T, env *fakeEnv) {
	t.Helper()
	oldInit, oldOpen, oldSleep, oldNow := hidInit, openFirst, sleep, now
	env.clock = time.Unix(1_700_000_000, 0)
	hidInit = func() error { env.inited++; return env.initErr }
	openFirst = func() (hidHandle, string, error) {
		env.opened++
		if env.opened > len(env.opens) {
			return nil, "", errors.New(errNotFound)
		}
		return env.opens[env.opened-1]()
	}
	sleep = func(d time.Duration) { env.slept += d; env.clock = env.clock.Add(d) }
	now = func() time.Time { return env.clock }
	t.Cleanup(func() { hidInit, openFirst, sleep, now = oldInit, oldOpen, oldSleep, oldNow })
}

func ok(h hidHandle) func() (hidHandle, string, error) { return okAt(h, "") }
func okAt(h hidHandle, path string) func() (hidHandle, string, error) {
	return func() (hidHandle, string, error) { return h, path, nil }
}
func fail(msg string) func() (hidHandle, string, error) {
	return func() (hidHandle, string, error) { return nil, "", errors.New(msg) }
}

var (
	errGeneral      = errors.New("IOHIDDeviceSetReport failed: (0xE00002BC) (iokit/common) general error")
	errDisconnected = errors.New("Device is disconnected")
	errNotFound     = "No HID devices with requested VID/PID found in the system."
)

func testPackets(n int) [][protocol.PacketSize]byte {
	pkts := make([][protocol.PacketSize]byte, n)
	for i := range pkts {
		pkts[i][0] = byte(i + 1)
	}
	return pkts
}

func TestSendPacketsRestartsFromHeaderOnMidTransferError(t *testing.T) {
	installFakeEnv(t, &fakeEnv{})
	fake := &fakeHandle{failOn: map[int]error{3: errGeneral}}
	d := &Device{dev: fake}
	if err := d.SendPackets(testPackets(4)); err != nil {
		t.Fatalf("SendPackets: %v", err)
	}
	// Frames 1,2 went out and frame 3 failed. The device still expects
	// two packets, so two filler packets complete the broken upload
	// before the whole transfer is resent from its header. The failed
	// frame is never retried in place.
	want := []byte{1, 2, 0, 0, 1, 2, 3, 4}
	if string(fake.frames) != string(want) {
		t.Fatalf("frames = %v, want %v", fake.frames, want)
	}
}

func TestSendPacketsGivesUpOnNonRetryableError(t *testing.T) {
	installFakeEnv(t, &fakeEnv{})
	fake := &fakeHandle{failOn: map[int]error{2: errors.New("permission denied")}}
	d := &Device{dev: fake}
	if err := d.SendPackets(testPackets(3)); err == nil {
		t.Fatal("expected error")
	}
	if fake.calls != 2 {
		t.Fatalf("expected no retry after fatal error, got %d writes", fake.calls)
	}
}

func TestSendPacketsExhaustsRetries(t *testing.T) {
	env := &fakeEnv{}
	installFakeEnv(t, env)
	failAll := map[int]error{}
	for i := 1; i <= 100; i++ {
		failAll[i] = errGeneral
	}
	d := &Device{dev: &fakeHandle{failOn: failAll}}
	if err := d.SendPackets(testPackets(2)); err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if env.slept == 0 {
		t.Fatal("expected backoff between transfer attempts")
	}
}

func TestSendPacketsSinglePacketUsesSendPacket(t *testing.T) {
	installFakeEnv(t, &fakeEnv{})
	fake := &fakeHandle{failOn: map[int]error{1: errGeneral}}
	d := &Device{dev: fake}
	if err := d.SendPackets(testPackets(1)); err != nil {
		t.Fatal(err)
	}
	if string(fake.frames) != string([]byte{1}) {
		t.Fatalf("frames = %v", fake.frames)
	}
}

func TestSendPacketsReopensStaleHandleAndRestarts(t *testing.T) {
	fresh := &fakeHandle{}
	installFakeEnv(t, &fakeEnv{opens: []func() (hidHandle, string, error){ok(fresh)}})
	stale := &fakeHandle{failOn: map[int]error{2: errDisconnected}}
	d := &Device{dev: stale}
	if err := d.SendPackets(testPackets(3)); err != nil {
		t.Fatal(err)
	}
	if !stale.closed {
		t.Fatal("stale handle must be closed before reopening")
	}
	// The device (same path, so not rebooted) still expects the rest of
	// the broken upload: filler first, then the full resend.
	if string(fresh.frames) != string([]byte{0, 0, 1, 2, 3}) {
		t.Fatalf("fresh handle frames = %v", fresh.frames)
	}
}

func TestSendPacketsReportsDeviceReset(t *testing.T) {
	env := &fakeEnv{}
	opens := []func() (hidHandle, string, error){}
	for i := 0; i < 10; i++ {
		opens = append(opens, fail(errNotFound))
	}
	opens = append(opens, ok(&fakeHandle{}))
	env.opens = opens
	installFakeEnv(t, env)
	d := &Device{dev: &fakeHandle{failOn: map[int]error{1: errDisconnected}}}
	err := d.SendPackets(testPackets(2))
	if !errors.Is(err, ErrDeviceReset) {
		t.Fatalf("expected ErrDeviceReset, got %v", err)
	}
}

func TestSendPacketRetriesTransientError(t *testing.T) {
	env := &fakeEnv{}
	installFakeEnv(t, env)
	fake := &fakeHandle{failOn: map[int]error{1: errGeneral, 2: errGeneral}}
	d := &Device{dev: fake}
	var pkt [protocol.PacketSize]byte
	pkt[0] = 7
	if err := d.SendPacket(pkt); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 3 || env.slept != 2*writeRetryDelay {
		t.Fatalf("calls=%d slept=%v", fake.calls, env.slept)
	}
	if len(fake.lastWrite) != protocol.PacketSize+1 || fake.lastWrite[0] != 0 || fake.lastWrite[1] != 7 {
		t.Fatal("write must be prefixed with a zero report ID")
	}
}

func TestSendPacketFailsOnShortWrite(t *testing.T) {
	installFakeEnv(t, &fakeEnv{})
	d := &Device{dev: &fakeHandle{shortOn: map[int]bool{1: true}}}
	var pkt [protocol.PacketSize]byte
	if err := d.SendPacket(pkt); err == nil {
		t.Fatal("expected short write error")
	}
}

func TestSendPacketReopenFailure(t *testing.T) {
	installFakeEnv(t, &fakeEnv{opens: []func() (hidHandle, string, error){fail("permission denied")}})
	d := &Device{dev: &fakeHandle{failOn: map[int]error{1: errDisconnected}}}
	var pkt [protocol.PacketSize]byte
	if err := d.SendPacket(pkt); err == nil {
		t.Fatal("expected reopen failure")
	}
}

func TestSendPacketStaleHandleReopensOnce(t *testing.T) {
	fresh := &fakeHandle{}
	installFakeEnv(t, &fakeEnv{opens: []func() (hidHandle, string, error){ok(fresh)}})
	d := &Device{dev: &fakeHandle{failOn: map[int]error{1: errDisconnected}}}
	var pkt [protocol.PacketSize]byte
	if err := d.SendPacket(pkt); err != nil {
		t.Fatal(err)
	}
	if fresh.calls != 1 {
		t.Fatalf("expected the frame on the fresh handle, got %d writes", fresh.calls)
	}
}
