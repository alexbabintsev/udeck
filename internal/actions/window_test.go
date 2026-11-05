package actions

import (
	"errors"
	"testing"

	"github.com/alexbabintsev/udeck/internal/config"
	"github.com/alexbabintsev/udeck/internal/macos"
)

// A main display with the menu bar and a portrait display to its right,
// placed higher (Accessibility coordinates).
var testScreens = []macos.Screen{
	{Frame: macos.Rect{X: 0, Y: 0, W: 2560, H: 1440}, Visible: macos.Rect{X: 0, Y: 25, W: 2560, H: 1415}},
	{Frame: macos.Rect{X: 2560, Y: -304, W: 1080, H: 1920}, Visible: macos.Rect{X: 2560, Y: -304, W: 1080, H: 1920}},
}

// fakeWindows is one window of one app; set moves it, like an app that
// takes any frame, unless minW makes it refuse to get narrower.
type fakeWindows struct {
	win  macos.Rect
	pid  int
	minW float64
	sets int
	err  error
}

func useFakeWindows(t *testing.T, f *fakeWindows) {
	t.Helper()
	old := windows
	windows = windowSystem{
		focused: func() (macos.Rect, int, error) { return f.win, f.pid, f.err },
		set: func(r macos.Rect) error {
			f.sets++
			r.W = max(r.W, f.minW)
			f.win = r
			return nil
		},
		screens: func() ([]macos.Screen, error) { return testScreens, nil },
	}
	lastMoveMu.Lock()
	clear(lastMove)
	lastMoveMu.Unlock()
	t.Cleanup(func() { windows = old })
}

func TestWindowTargets(t *testing.T) {
	win := macos.Rect{X: 300, Y: 200, W: 800, H: 600}
	cases := map[string]macos.Rect{
		"left_half":        {X: 0, Y: 25, W: 1280, H: 1415},
		"right_half":       {X: 1280, Y: 25, W: 1280, H: 1415},
		"top_half":         {X: 0, Y: 25, W: 2560, H: 707.5},
		"bottom_half":      {X: 0, Y: 732.5, W: 2560, H: 707.5},
		"top_right":        {X: 1280, Y: 25, W: 1280, H: 707.5},
		"bottom_left":      {X: 0, Y: 732.5, W: 1280, H: 707.5},
		"maximize":         {X: 0, Y: 25, W: 2560, H: 1415},
		"center":           {X: 880, Y: 432.5, W: 800, H: 600},
		"right_two_thirds": {X: 853, Y: 25, W: 1707, H: 1415},
	}
	for to, want := range cases {
		got, err := windowTarget(to, win, testScreens)
		if to == "right_two_thirds" {
			got = roundRect(got)
		}
		if err != nil || got != want {
			t.Fatalf("%s: got %+v %v, want %+v", to, got, err, want)
		}
	}
	// Every configured position is handled (restore is not a layout).
	for _, to := range config.WindowPositions {
		if to == "restore" {
			continue
		}
		if _, err := windowTarget(to, win, testScreens); err != nil {
			t.Fatalf("%s: %v", to, err)
		}
	}
	if _, err := windowTarget("diagonal", win, testScreens); err == nil {
		t.Fatal("expected an unknown position error")
	}
	if _, err := windowTarget("maximize", win, nil); err == nil {
		t.Fatal("expected a no displays error")
	}
}

func TestWindowTargetUsesTheWindowsScreen(t *testing.T) {
	portrait := macos.Rect{X: 2700, Y: 100, W: 500, H: 500}
	got, _ := windowTarget("top_half", portrait, testScreens)
	if got != (macos.Rect{X: 2560, Y: -304, W: 1080, H: 960}) {
		t.Fatalf("top half of the portrait display: %+v", got)
	}
	// Mostly off-screen: the screen it overlaps most.
	off := macos.Rect{X: 2400, Y: -900, W: 400, H: 700}
	if got, _ := windowTarget("maximize", off, testScreens); got != testScreens[1].Visible {
		t.Fatalf("got %+v", got)
	}
	// Nowhere: the main display.
	lost := macos.Rect{X: -5000, Y: -5000, W: 10, H: 10}
	if got, _ := windowTarget("maximize", lost, testScreens); got != testScreens[0].Visible {
		t.Fatalf("got %+v", got)
	}
}

func TestWindowNextDisplayKeepsRelativeFrame(t *testing.T) {
	// The left half of the main display goes to the left half of the
	// portrait one, and back.
	left := macos.Rect{X: 0, Y: 25, W: 1280, H: 1415}
	got, _ := windowTarget("next_display", left, testScreens)
	if got != (macos.Rect{X: 2560, Y: -304, W: 540, H: 1920}) {
		t.Fatalf("next: %+v", got)
	}
	back, _ := windowTarget("next_display", got, testScreens)
	if back != left {
		t.Fatalf("next again wraps around: %+v", back)
	}
	if prev, _ := windowTarget("previous_display", left, testScreens); prev != got {
		t.Fatalf("previous with two displays: %+v", prev)
	}
	// Never larger than the target area.
	huge := macos.Rect{X: -100, Y: 0, W: 3000, H: 2000}
	if r := carryOver(huge, testScreens[0].Visible, testScreens[1].Visible); r.W > 1080 || r.H > 1920 || r.X < 2560 {
		t.Fatalf("carried over %+v", r)
	}
}

func TestMoveWindowAndRestore(t *testing.T) {
	start := macos.Rect{X: 300, Y: 200, W: 801, H: 600}
	f := &fakeWindows{win: start, pid: 42}
	useFakeWindows(t, f)
	if err := Run(&config.Action{Type: "window", To: "right_third"}); err != nil {
		t.Fatal(err)
	}
	if f.win != (macos.Rect{X: 1707, Y: 25, W: 853, H: 1415}) {
		t.Fatalf("frames are rounded to pixels: %+v", f.win)
	}
	if err := moveWindow("restore"); err != nil || f.win != start {
		t.Fatalf("restore: %+v %v", f.win, err)
	}
	sets := f.sets
	if err := moveWindow("restore"); err != nil || f.sets != sets {
		t.Fatal("a second restore has nothing to undo")
	}

	// Moved by hand after our move: restore leaves it alone.
	_ = moveWindow("left_half")
	f.win.X += 50
	sets = f.sets
	_ = moveWindow("restore")
	if f.sets != sets {
		t.Fatal("restore must not undo a manual move")
	}

	// An app that keeps a minimum width: restore still recognises the
	// frame it actually took.
	f.minW = 1500
	_ = moveWindow("left_third")
	if err := moveWindow("restore"); err != nil || f.win.X != 50 {
		t.Fatalf("restore after a capped move: %+v", f.win)
	}
}

func TestMoveWindowErrors(t *testing.T) {
	f := &fakeWindows{err: macos.ErrNoWindow}
	useFakeWindows(t, f)
	if err := moveWindow("maximize"); !errors.Is(err, macos.ErrNoWindow) {
		t.Fatalf("got %v", err)
	}
	f.err = nil
	windows.set = func(macos.Rect) error { return macos.ErrWindowLocked }
	if err := moveWindow("maximize"); !errors.Is(err, macos.ErrWindowLocked) {
		t.Fatalf("got %v", err)
	}
	windows.screens = func() ([]macos.Screen, error) { return nil, errors.New("x") }
	if err := moveWindow("maximize"); err == nil {
		t.Fatal("expected a screens error")
	}
}

func TestRectHelpers(t *testing.T) {
	r := macos.Rect{X: 0, Y: 0, W: 10, H: 10}
	if !r.Contains(5, 5) || r.Contains(10, 5) {
		t.Fatal("Contains")
	}
	if r.Intersection(macos.Rect{X: 5, Y: 5, W: 10, H: 10}) != 25 || r.Intersection(macos.Rect{X: 20, Y: 0, W: 1, H: 1}) != 0 {
		t.Fatal("Intersection")
	}
	if !sameFrame(r, macos.Rect{X: 1, Y: 2, W: 9, H: 11}) || sameFrame(r, macos.Rect{X: 3}) {
		t.Fatal("sameFrame")
	}
}
