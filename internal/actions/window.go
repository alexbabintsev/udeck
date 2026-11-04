package actions

import (
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/macos"
)

// windowSystem is the Accessibility API as window actions use it; tests
// replace it.
type windowSystem struct {
	focused func() (macos.Rect, int, error)
	set     func(macos.Rect) error
	screens func() ([]macos.Screen, error)
}

var windows = windowSystem{
	focused: macos.FocusedWindow,
	set:     macos.SetFocusedWindow,
	screens: macos.Screens,
}

// lastMove remembers, per app, where the last window action put a window
// and where it was before, for `to: restore`.
var (
	lastMoveMu sync.Mutex
	lastMove   = map[int]windowMove{}
)

type windowMove struct{ from, to macos.Rect }

// moveWindow places the focused window of the frontmost app.
func moveWindow(to string) error {
	win, pid, err := windows.focused()
	if err != nil {
		return fmt.Errorf("window %s: %w", to, err)
	}
	var target macos.Rect
	if to == "restore" {
		lastMoveMu.Lock()
		m, ok := lastMove[pid]
		lastMoveMu.Unlock()
		// Only undo our own move: if the window was moved by hand since,
		// there is nothing to restore.
		if !ok || !sameFrame(win, m.to) {
			return nil
		}
		target = m.from
	} else {
		screens, err := windows.screens()
		if err != nil {
			return fmt.Errorf("window %s: %w", to, err)
		}
		target, err = windowTarget(to, win, screens)
		if err != nil {
			return err
		}
	}
	target = roundRect(target)
	if err := windows.set(target); err != nil {
		return fmt.Errorf("window %s: %w", to, err)
	}
	if to != "restore" {
		// Read back where the app actually put it: apps with a minimum
		// size or size steps (terminals) may not take the frame exactly.
		got, _, err := windows.focused()
		if err != nil {
			got = target
		}
		lastMoveMu.Lock()
		lastMove[pid] = windowMove{from: win, to: got}
		lastMoveMu.Unlock()
	} else {
		lastMoveMu.Lock()
		delete(lastMove, pid)
		lastMoveMu.Unlock()
	}
	return nil
}

// windowTarget computes the frame for a position on the window's screen.
func windowTarget(to string, win macos.Rect, screens []macos.Screen) (macos.Rect, error) {
	if len(screens) == 0 {
		return macos.Rect{}, fmt.Errorf("window %s: no displays", to)
	}
	cur := screenOf(win, screens)
	v := screens[cur].Visible
	part := func(x, y, w, h float64) macos.Rect {
		return macos.Rect{X: v.X + v.W*x, Y: v.Y + v.H*y, W: v.W * w, H: v.H * h}
	}
	switch to {
	case "left_half":
		return part(0, 0, 0.5, 1), nil
	case "right_half":
		return part(0.5, 0, 0.5, 1), nil
	case "top_half":
		return part(0, 0, 1, 0.5), nil
	case "bottom_half":
		return part(0, 0.5, 1, 0.5), nil
	case "top_left":
		return part(0, 0, 0.5, 0.5), nil
	case "top_right":
		return part(0.5, 0, 0.5, 0.5), nil
	case "bottom_left":
		return part(0, 0.5, 0.5, 0.5), nil
	case "bottom_right":
		return part(0.5, 0.5, 0.5, 0.5), nil
	case "left_third":
		return part(0, 0, 1.0/3, 1), nil
	case "center_third":
		return part(1.0/3, 0, 1.0/3, 1), nil
	case "right_third":
		return part(2.0/3, 0, 1.0/3, 1), nil
	case "left_two_thirds":
		return part(0, 0, 2.0/3, 1), nil
	case "right_two_thirds":
		return part(1.0/3, 0, 2.0/3, 1), nil
	case "maximize":
		return v, nil
	case "center":
		w, h := min(win.W, v.W), min(win.H, v.H)
		return macos.Rect{X: v.X + (v.W-w)/2, Y: v.Y + (v.H-h)/2, W: w, H: h}, nil
	case "next_display", "previous_display":
		step := 1
		if to == "previous_display" {
			step = -1
		}
		order := displayOrder(screens)
		pos := 0
		for i, idx := range order {
			if idx == cur {
				pos = i
			}
		}
		next := screens[order[(pos+step+len(order))%len(order)]].Visible
		return carryOver(win, v, next), nil
	}
	return macos.Rect{}, fmt.Errorf("unknown window position %q", to)
}

// screenOf is the index of the screen holding the window's center, or
// else the one it overlaps most, or else the main display.
func screenOf(win macos.Rect, screens []macos.Screen) int {
	cx, cy := win.Center()
	for i, s := range screens {
		if s.Frame.Contains(cx, cy) {
			return i
		}
	}
	best, area := 0, 0.0
	for i, s := range screens {
		if a := s.Frame.Intersection(win); a > area {
			best, area = i, a
		}
	}
	return best
}

// displayOrder lists screen indexes left to right, then top to bottom.
func displayOrder(screens []macos.Screen) []int {
	order := make([]int, len(screens))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		fa, fb := screens[order[a]].Frame, screens[order[b]].Frame
		if fa.X != fb.X {
			return fa.X < fb.X
		}
		return fa.Y < fb.Y
	})
	return order
}

// carryOver moves win from one visible area to another, keeping its
// relative place and size; it never ends up larger than the new area.
func carryOver(win, from, to macos.Rect) macos.Rect {
	rx, ry := (win.X-from.X)/from.W, (win.Y-from.Y)/from.H
	rw, rh := win.W/from.W, win.H/from.H
	w, h := min(rw*to.W, to.W), min(rh*to.H, to.H)
	x := min(max(to.X+rx*to.W, to.X), to.X+to.W-w)
	y := min(max(to.Y+ry*to.H, to.Y), to.Y+to.H-h)
	return macos.Rect{X: x, Y: y, W: w, H: h}
}

func roundRect(r macos.Rect) macos.Rect {
	return macos.Rect{X: math.Round(r.X), Y: math.Round(r.Y), W: math.Round(r.W), H: math.Round(r.H)}
}

// sameFrame allows for the pixel or two apps round frames by.
func sameFrame(a, b macos.Rect) bool {
	near := func(x, y float64) bool { return math.Abs(x-y) <= 2 }
	return near(a.X, b.X) && near(a.Y, b.Y) && near(a.W, b.W) && near(a.H, b.H)
}
