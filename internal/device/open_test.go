package device

import (
	"errors"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/protocol"
)

func TestOpenRetriesTransientErrors(t *testing.T) {
	h := &fakeHandle{}
	env := &fakeEnv{opens: []func() (hidHandle, string, error){
		fail("privilege violation"), fail("resource busy"), ok(h),
	}}
	installFakeEnv(t, env)
	d, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if env.inited != 1 || env.opened != 3 || d.dev != h {
		t.Fatalf("inited=%d opened=%d", env.inited, env.opened)
	}
	if env.slept != openRetryBackoff(0)+openRetryBackoff(1) {
		t.Fatalf("unexpected backoff %v", env.slept)
	}
}

func TestOpenStopsOnFatalError(t *testing.T) {
	env := &fakeEnv{opens: []func() (hidHandle, string, error){fail("permission denied"), ok(&fakeHandle{})}}
	installFakeEnv(t, env)
	if _, err := Open(); err == nil {
		t.Fatal("expected error")
	}
	if env.opened != 1 {
		t.Fatalf("fatal error must not be retried, opened=%d", env.opened)
	}
}

func TestOpenGivesUpAfterRetries(t *testing.T) {
	env := &fakeEnv{}
	installFakeEnv(t, env)
	if _, err := Open(); err == nil {
		t.Fatal("expected error")
	}
	if env.opened != openRetries {
		t.Fatalf("opened=%d want %d", env.opened, openRetries)
	}
}

func TestOpenReportsInitError(t *testing.T) {
	installFakeEnv(t, &fakeEnv{initErr: errors.New("boom")})
	if _, err := Open(); err == nil {
		t.Fatal("expected init error")
	}
}

func TestReconnectShortAbsenceIsNotReset(t *testing.T) {
	old := &fakeHandle{}
	fresh := &fakeHandle{}
	installFakeEnv(t, &fakeEnv{opens: []func() (hidHandle, string, error){fail(errNotFound), ok(fresh)}})
	d := &Device{dev: old}
	reset, err := d.Reconnect()
	if err != nil || reset {
		t.Fatalf("reset=%v err=%v", reset, err)
	}
	if !old.closed || d.dev != fresh {
		t.Fatal("expected old handle closed and fresh handle installed")
	}
}

func TestReconnectLongAbsenceIsReset(t *testing.T) {
	opens := []func() (hidHandle, string, error){}
	for i := 0; i < 8; i++ {
		opens = append(opens, fail(errNotFound))
	}
	opens = append(opens, ok(&fakeHandle{}))
	installFakeEnv(t, &fakeEnv{opens: opens})
	d := &Device{dev: &fakeHandle{closeErr: errors.New("close failed")}}
	reset, err := d.Reconnect()
	if err != nil || !reset {
		t.Fatalf("reset=%v err=%v", reset, err)
	}
}

func TestTryReconnectMakesSingleAttempt(t *testing.T) {
	env := &fakeEnv{}
	installFakeEnv(t, env)
	d := &Device{}
	reset, err := d.TryReconnect()
	if err == nil || !reset || env.opened != 1 {
		t.Fatalf("reset=%v err=%v opened=%d", reset, err, env.opened)
	}
}

func TestCloseClosesHandleOnce(t *testing.T) {
	h := &fakeHandle{}
	d := &Device{dev: h}
	if err := d.Close(); err != nil || !h.closed {
		t.Fatalf("err=%v closed=%v", err, h.closed)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	var nilDev *Device
	if err := nilDev.Close(); err != nil {
		t.Fatal(err)
	}
	nilDev.Detach()
}

func TestReadPacket(t *testing.T) {
	pkt := protocol.EncodePacket(protocol.CmdInButton, []byte{0, 1, 1, 1})
	h := &fakeHandle{reads: []readResult{
		{data: pkt[:]},
		{err: errors.New("hid_read_timeout: error waiting for more data")},
		{data: nil},
		{err: errors.New("boom")},
	}}
	installFakeEnv(t, &fakeEnv{})
	d := &Device{dev: h}

	raw, err := d.ReadPacket(time.Millisecond)
	if err != nil || len(raw) != protocol.PacketSize {
		t.Fatalf("raw=%d err=%v", len(raw), err)
	}
	if raw, err := d.ReadPacket(time.Millisecond); raw != nil || err != nil {
		t.Fatalf("interrupted wait must be a soft miss: %v %v", raw, err)
	}
	if raw, err := d.ReadPacket(time.Millisecond); raw != nil || err != nil {
		t.Fatalf("empty read must be a soft miss: %v %v", raw, err)
	}
	if _, err := d.ReadPacket(time.Millisecond); err == nil {
		t.Fatal("expected hard read error")
	}
}

func TestReadPacketReopensStaleHandle(t *testing.T) {
	fresh := &fakeHandle{}
	installFakeEnv(t, &fakeEnv{opens: []func() (hidHandle, string, error){ok(fresh)}})
	d := &Device{dev: &fakeHandle{reads: []readResult{{err: errDisconnected}}}}
	raw, err := d.ReadPacket(time.Millisecond)
	if raw != nil || err != nil {
		t.Fatalf("raw=%v err=%v", raw, err)
	}
	if d.dev != fresh {
		t.Fatal("expected fresh handle")
	}
}

func TestReadPacketStaleHandleWithResetOrFailure(t *testing.T) {
	opens := []func() (hidHandle, string, error){}
	for i := 0; i < 8; i++ {
		opens = append(opens, fail(errNotFound))
	}
	opens = append(opens, ok(&fakeHandle{}))
	installFakeEnv(t, &fakeEnv{opens: opens})
	d := &Device{dev: &fakeHandle{reads: []readResult{{err: errDisconnected}}}}
	if _, err := d.ReadPacket(time.Millisecond); !errors.Is(err, ErrDeviceReset) {
		t.Fatalf("expected ErrDeviceReset, got %v", err)
	}

	installFakeEnv(t, &fakeEnv{opens: []func() (hidHandle, string, error){fail("permission denied")}})
	d = &Device{dev: &fakeHandle{reads: []readResult{{err: errDisconnected}}}}
	if _, err := d.ReadPacket(time.Millisecond); err == nil {
		t.Fatal("expected reopen failure")
	}
}

func TestInfoRequestsAndReturnsDeviceInfo(t *testing.T) {
	info := protocol.EncodePacket(protocol.CmdInDeviceInfo, []byte("fw 1.0\x00"))
	btn := protocol.EncodePacket(protocol.CmdInButton, []byte{0, 1, 1, 1})
	h := &fakeHandle{reads: []readResult{
		{data: []byte{1, 2, 3}}, {data: btn[:]}, {data: info[:]},
	}}
	d := &Device{dev: h}
	got, err := d.Info(time.Second)
	if err != nil || got != "fw 1.0" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if cmd, _, _ := protocol.DecodeHeader(h.lastWrite[1:]); cmd != protocol.CmdGetDeviceInfo {
		t.Fatalf("Info must request the info first, sent %v", cmd)
	}
}

func TestInfoTimesOutAndPropagatesErrors(t *testing.T) {
	installFakeEnv(t, &fakeEnv{})
	d := &Device{dev: &fakeHandle{}}
	if _, err := d.Info(10 * time.Millisecond); err == nil {
		t.Fatal("expected timeout error")
	}
	d = &Device{}
	if _, err := d.Info(time.Second); err == nil {
		t.Fatal("expected detached error")
	}
}

func TestErrorClassifiers(t *testing.T) {
	if !isNotFoundErr(errors.New(errNotFound)) || isNotFoundErr(errors.New("busy")) {
		t.Fatal("isNotFoundErr misclassifies")
	}
	if isStaleHandleErr(nil) || !isStaleHandleErr(errors.New("no such device")) {
		t.Fatal("isStaleHandleErr misclassifies")
	}
	if shouldRetryOpen(nil) || shouldRetryWrite(nil) {
		t.Fatal("nil must not be retried")
	}
	for _, msg := range []string{"0xE00002ED", "not ready", "0xE0005000"} {
		if !shouldRetryWrite(errors.New(msg)) {
			t.Fatalf("expected %q to be retried", msg)
		}
	}
}

func TestReconnectDetectsReEnumerationByPath(t *testing.T) {
	// Back within milliseconds, but under a new IORegistry entry: the
	// device rebooted and must get its page again.
	installFakeEnv(t, &fakeEnv{opens: []func() (hidHandle, string, error){okAt(&fakeHandle{}, "DevSrvsID:2")}})
	d := &Device{dev: &fakeHandle{}, path: "DevSrvsID:1"}
	reset, err := d.Reconnect()
	if err != nil || !reset {
		t.Fatalf("reset=%v err=%v", reset, err)
	}
	if d.path != "DevSrvsID:2" {
		t.Fatal("path not updated")
	}

	// Same entry: only the handle was stale.
	installFakeEnv(t, &fakeEnv{opens: []func() (hidHandle, string, error){okAt(&fakeHandle{}, "DevSrvsID:2")}})
	if reset, err := d.Reconnect(); err != nil || reset {
		t.Fatalf("same path must not count as reset: reset=%v err=%v", reset, err)
	}
}

func TestStaleReadAfterRebootReportsReset(t *testing.T) {
	installFakeEnv(t, &fakeEnv{opens: []func() (hidHandle, string, error){okAt(&fakeHandle{}, "DevSrvsID:9")}})
	d := &Device{dev: &fakeHandle{reads: []readResult{{err: errDisconnected}}}, path: "DevSrvsID:8"}
	if _, err := d.ReadPacket(time.Millisecond); !errors.Is(err, ErrDeviceReset) {
		t.Fatalf("expected ErrDeviceReset after re-enumeration, got %v", err)
	}
}

func TestOpenRecordsPath(t *testing.T) {
	installFakeEnv(t, &fakeEnv{opens: []func() (hidHandle, string, error){okAt(&fakeHandle{}, "DevSrvsID:5")}})
	d, err := Open()
	if err != nil || d.path != "DevSrvsID:5" {
		t.Fatalf("path=%q err=%v", d.path, err)
	}
}
