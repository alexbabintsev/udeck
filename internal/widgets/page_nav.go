package widgets

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"sync"

	"github.com/alexbabintsev/udeck/internal/config"
	"github.com/alexbabintsev/udeck/internal/icons"
)

// pageNavOverlay renders page-navigation button icons with the current page
// number badge. Icons are cached per (iconID, iconColor, pageNumber) tuple as
// in-memory images.
type pageNavOverlay struct {
	mu    sync.Mutex
	cache map[string]string // key → icons mem: reference
}

func newPageNavOverlay() (*pageNavOverlay, error) {
	return &pageNavOverlay{cache: map[string]string{}}, nil
}

func (o *pageNavOverlay) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, ref := range o.cache {
		icons.DeleteImage(ref)
	}
	clear(o.cache)
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

	ref := icons.NewMemRef("page-nav")
	icons.PutImage(ref, img)
	o.cache[key] = ref
	return ref
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

func fillRoundRect(img *image.RGBA, rect image.Rectangle, radius int, clr color.Color) {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if pointInRoundedRect(x, y, rect, radius) {
				img.Set(x, y, clr)
			}
		}
	}
}

func pointInRoundedRect(x, y int, rect image.Rectangle, radius int) bool {
	if radius <= 0 {
		return true
	}
	left, top, right, bottom := rect.Min.X, rect.Min.Y, rect.Max.X-1, rect.Max.Y-1
	if x >= left+radius && x <= right-radius {
		return true
	}
	if y >= top+radius && y <= bottom-radius {
		return true
	}

	corners := [][2]int{
		{left + radius, top + radius},
		{right - radius, top + radius},
		{left + radius, bottom - radius},
		{right - radius, bottom - radius},
	}
	for _, c := range corners {
		dx := x - c[0]
		dy := y - c[1]
		if dx*dx+dy*dy <= radius*radius {
			return true
		}
	}
	return false
}
