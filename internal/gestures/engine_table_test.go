package gestures

import (
	"testing"
	"time"
)

var t0 = time.Unix(1000, 0)

func ms(n int) time.Time { return t0.Add(time.Duration(n) * time.Millisecond) }

func TestEngineTapOnly(t *testing.T) {
	e := New()
	o := Options{HasTap: true}
	if e.HandleEvent("a", o, false, t0) != None {
		t.Fatal("release without press")
	}
	e.HandleEvent("a", o, true, t0)
	if e.HandleEvent("a", o, false, ms(50)) != Tap {
		t.Fatal("expected tap on release")
	}
	if (New()).HandleEvent("a", Options{}, true, t0) != None {
		t.Fatal("no options")
	}
	e.HandleEvent("b", Options{}, true, t0)
	if e.HandleEvent("b", Options{}, false, ms(10)) != None {
		t.Fatal("button without gestures must stay silent")
	}
}

func TestEngineSingleTapWithDoubleTapEnabledFiresFromTick(t *testing.T) {
	e := New()
	o := Options{HasTap: true, HasDoubleTap: true, DoubleTapDelay: 300 * time.Millisecond}
	b := []Binding{{ID: "a", Options: o}, {ID: "unused", Options: o}}
	e.HandleEvent("a", o, true, t0)
	e.HandleEvent("a", o, false, ms(40))
	if id, k := e.Tick(b, ms(100)); k != None || id != "" {
		t.Fatal("tap must wait for the double-tap window")
	}
	if id, k := e.Tick(b, ms(400)); id != "a" || k != Tap {
		t.Fatalf("got %q %q", id, k)
	}
	// Second press after the window: a fresh single tap, not a double tap.
	e.HandleEvent("a", o, true, ms(900))
	if e.HandleEvent("a", o, false, ms(950)) != None {
		t.Fatal("must start a new pending tap")
	}
}

func TestEngineHoldOnReleaseAndNoTapAfterHold(t *testing.T) {
	e := New()
	o := Options{HasTap: true, HasHold: true, HoldDelay: 500 * time.Millisecond}
	e.HandleEvent("a", o, true, t0)
	if e.HandleEvent("a", o, false, ms(600)) != Hold {
		t.Fatal("long press released after the delay is a hold")
	}
	// Hold fired from Tick: the release must not add a tap.
	e.HandleEvent("a", o, true, ms(1000))
	if _, k := e.Tick([]Binding{{ID: "a", Options: o}}, ms(1600)); k != Hold {
		t.Fatal("tick hold")
	}
	if e.HandleEvent("a", o, false, ms(1700)) != None {
		t.Fatal("release after a fired hold must be silent")
	}
}

func TestEngineRepeatWithoutHoldAndReset(t *testing.T) {
	e := New()
	o := Options{HasRepeat: true, RepeatEvery: 100 * time.Millisecond}
	b := []Binding{{ID: "a", Options: o}}
	e.HandleEvent("a", o, true, t0)
	if _, k := e.Tick(b, ms(50)); k != None {
		t.Fatal("too early")
	}
	if _, k := e.Tick(b, ms(100)); k != Repeat {
		t.Fatal("first repeat after RepeatEvery when there is no hold")
	}
	if _, k := e.Tick(b, ms(150)); k != None {
		t.Fatal("next repeat not due yet")
	}
	e.Reset()
	if _, k := e.Tick(b, ms(1000)); k != None {
		t.Fatal("reset must clear state")
	}
}
