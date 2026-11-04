package macos

import "errors"

// Rect is a rectangle in global screen coordinates as the Accessibility
// API uses them: origin at the top-left of the main display, y down.
type Rect struct {
	X, Y, W, H float64
}

// Center returns the rectangle's center point.
func (r Rect) Center() (float64, float64) { return r.X + r.W/2, r.Y + r.H/2 }

// Contains reports whether the point is inside r.
func (r Rect) Contains(x, y float64) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Intersection is the area r and o share.
func (r Rect) Intersection(o Rect) float64 {
	w := min(r.X+r.W, o.X+o.W) - max(r.X, o.X)
	h := min(r.Y+r.H, o.Y+o.H) - max(r.Y, o.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// Screen is a display: its full frame and the part windows may use
// (without the menu bar and the Dock).
type Screen struct {
	Frame, Visible Rect
}

// Window errors.
var (
	ErrNoWindow     = errors.New("no focused window")
	ErrNotTrusted   = errors.New("no Accessibility access (System Settings → Privacy & Security → Accessibility)")
	ErrWindowLocked = errors.New("the window cannot be moved or resized")
)
