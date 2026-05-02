package widgets

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"sync"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
)

// pageNavOverlay renders page-navigation button icons with the current page
// number badge. Icons are cached per (iconID, iconColor, pageNumber) tuple and
// written as PNGs to a shared temp directory.
type pageNavOverlay struct {
	mu    sync.Mutex
	dir   string
	cache map[string]string // key → file path
}

func newPageNavOverlay() (*pageNavOverlay, error) {
	dir, err := os.MkdirTemp("", "ulanzi-page-nav-")
	if err != nil {
		return nil, err
	}
	return &pageNavOverlay{dir: dir, cache: map[string]string{}}, nil
}

func (o *pageNavOverlay) close() {
	_ = os.RemoveAll(o.dir)
}

// resolve returns an icon path for the given button with a page number badge.
// iconID is the lucide icon (e.g. "lucide/arrow-right"), iconColor the hex
// color string, pageNumber the 1-based page index to display.
func (o *pageNavOverlay) resolve(iconID, iconColor string, pageNumber int) string {
	key := fmt.Sprintf("%s|%s|%d", iconID, iconColor, pageNumber)

	o.mu.Lock()
	defer o.mu.Unlock()

	if path, ok := o.cache[key]; ok {
		return path
	}

	img := image.NewRGBA(image.Rect(0, 0, icons.IconHeight, icons.IconHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)

	arrowClr := color.Color(rgb("33bbff"))
	if iconColor != "" {
		arrowClr = rgb(iconColor)
	}

	// Arrow icon — full canvas.
	drawIcon(img, iconID, CenterX(0), CenterY(0), 120, 120, arrowClr)

	// Page number badge — white rounded rect, top-right, 10px from edge.
	const badgeSize = 44
	const badgeMargin = 20
	bx := badgeMargin
	by := badgeMargin
	fillRoundRect(img, image.Rect(bx, by, bx+badgeSize, by+badgeSize), 5, color.White)
	drawText(img, fmt.Sprintf("%d", pageNumber),
		FromLeft(badgeMargin), FromTop(badgeMargin), badgeSize, badgeSize, AlignMiddleCenter, color.Black, 34, true)

	path := filepath.Join(o.dir, fmt.Sprintf("page_nav_%s.png", sanitizeKey(key)))
	if err := writePNGAtomic(path, img); err != nil {
		return ""
	}
	o.cache[key] = path
	return path
}

func sanitizeKey(s string) string {
	out := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			out[i] = c
		} else {
			out[i] = '_'
		}
	}
	return string(out)
}

// isPageNavBinding returns true if any gesture on the button has type
// "page_next" or "page_prev", and the button has no widget.
func isPageNavButton(btn config.Button) bool {
	if btn.Widget != nil || btn.On == nil {
		return false
	}
	for _, g := range []*config.GestureBinding{
		btn.On.Tap, btn.On.DoubleTap, btn.On.Hold, btn.On.Repeat,
	} {
		if g != nil && (g.Type == "page_next" || g.Type == "page_prev") {
			return true
		}
	}
	return false
}

// pageNavIconID returns the lucide icon for the primary page-nav gesture.
func pageNavIconID(btn config.Button) string {
	if btn.On == nil {
		return "lucide/arrow-right"
	}
	for _, g := range []*config.GestureBinding{
		btn.On.Tap, btn.On.DoubleTap, btn.On.Hold, btn.On.Repeat,
	} {
		if g == nil {
			continue
		}
		if g.Type == "page_next" {
			return "lucide/arrow-right"
		}
		if g.Type == "page_prev" {
			return "lucide/arrow-left"
		}
	}
	return "lucide/arrow-right"
}
