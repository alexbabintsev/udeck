package widgets

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	"github.com/alexbabintsev/udeck/internal/icons"
)

// The shared look of the general-purpose widgets (volume, microphone,
// audio output, media). Every key is drawn on black with one of two
// layouts:
//
//   - gauge card: a ring showing a level, a large value inside it and a
//     small icon under the value (like the memory widget);
//   - icon card: a large Lucide icon with an optional upper-case label
//     below it (like the project widget).
//
// Colors carry the state, the same way everywhere: green when something
// is live, red when it is off or failing, blue for neutral information,
// grey for "no data".
var (
	styleText   = color.RGBA{0xf0, 0xf0, 0xf0, 0xff}
	styleDim    = color.RGBA{0x77, 0x77, 0x77, 0xff}
	styleTrack  = color.RGBA{0x1f, 0x1f, 0x1f, 0xff}
	styleLive   = color.RGBA{0x2e, 0xcc, 0x40, 0xff}
	styleAlert  = color.RGBA{0xe7, 0x4c, 0x3c, 0xff}
	styleWarn   = color.RGBA{0xe0, 0xa0, 0x20, 0xff}
	styleAccent = color.RGBA{0x33, 0xbb, 0xff, 0xff}
)

const (
	// Gauge card geometry: the ring and the blocks inside it.
	styleRingOuter = 72.0
	styleRingInner = 61.0
	styleValueSize = 44
	// Icon card geometry: icon square, its top, and the label block.
	styleIconSize  = 96
	styleIconTop   = 24
	styleLabelTop  = 132
	styleLabelH    = 40
	styleLabelSize = 28
	// Below styleLabelMinSize a one-line label wraps onto two lines of
	// styleLabelSmall.
	styleLabelMinSize = 22
	styleLabelSmall   = 22
)

// newCard returns a black key-sized canvas.
func newCard() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, icons.IconWidth, icons.IconHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)
	return img
}

// drawGaugeCard draws a ring filled to frac (0..1) in clr, value inside
// it and a small icon below the value. An empty value leaves room for
// a larger icon in the middle (e.g. a "muted" symbol).
func drawGaugeCard(img *image.RGBA, frac float64, clr color.Color, value string, valueClr color.Color, iconID string, iconClr color.Color) {
	drawRing(img, frac, clr)
	w := img.Bounds().Dx()
	if value == "" {
		drawIcon(img, iconID, CenterX(0), CenterY(0), 64, 64, iconClr)
		return
	}
	drawText(img, value, CenterX(0), FromTop(66), w-90, 48, AlignMiddleCenter, valueClr, styleValueSize, true)
	if iconID != "" {
		drawIcon(img, iconID, CenterX(0), FromTop(118), 30, 30, iconClr)
	}
}

// drawGaugeCardText is drawGaugeCard with a short caption under the
// value instead of an icon.
func drawGaugeCardText(img *image.RGBA, frac float64, clr color.Color, value string, valueClr color.Color, caption string, captionClr color.Color) {
	drawRing(img, frac, clr)
	w := img.Bounds().Dx()
	drawText(img, value, CenterX(0), FromTop(66), w-90, 48, AlignMiddleCenter, valueClr, styleValueSize, true)
	drawText(img, strings.ToUpper(caption), CenterX(0), FromTop(116), w-100, 26, AlignMiddleCenter, captionClr, 20, true)
}

// drawIconCard draws a large icon, centred when there is no label.
func drawIconCard(img *image.RGBA, iconID string, iconClr color.Color, label string, labelClr color.Color) {
	if label == "" {
		drawIcon(img, iconID, CenterX(0), CenterY(0), 112, 112, iconClr)
		return
	}
	drawIcon(img, iconID, CenterX(0), FromTop(styleIconTop), styleIconSize, styleIconSize, iconClr)
	drawCardLabel(img, label, labelClr)
}

// drawCardLabel writes the upper-case label under the icon. A label
// too long for one line at a readable size (a device name, say) wraps
// onto two shorter lines instead of being cut off.
func drawCardLabel(img *image.RGBA, label string, clr color.Color) {
	text := strings.ToUpper(strings.TrimSpace(label))
	w := img.Bounds().Dx() - 16
	if face, fitted := fitFace(text, w, styleLabelSize, styleLabelMinSize, 1, true); face != nil {
		face.Close()
		if fitted == text {
			drawText(img, text, FromLeft(8), FromTop(styleLabelTop), w, styleLabelH, AlignTopCenter, clr, styleLabelSize, true)
			return
		}
	}
	first, second := splitLabel(text)
	drawText(img, first, FromLeft(8), FromTop(styleLabelTop-6), w, 26, AlignMiddleCenter, clr, styleLabelSmall, true)
	drawText(img, second, FromLeft(8), FromTop(styleLabelTop+20), w, 26, AlignMiddleCenter, clr, styleLabelSmall, true)
}

// splitLabel breaks text at the space nearest its middle, or in the
// middle when it has no spaces.
func splitLabel(text string) (string, string) {
	runes := []rune(text)
	mid := len(runes) / 2
	best := -1
	for i, r := range runes {
		if r == ' ' && (best < 0 || abs(i-mid) < abs(best-mid)) {
			best = i
		}
	}
	if best < 0 {
		return string(runes[:mid]), string(runes[mid:])
	}
	return strings.TrimSpace(string(runes[:best])), strings.TrimSpace(string(runes[best+1:]))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// drawRing draws the level ring around the key's centre, filled
// clockwise from 12 o'clock.
func drawRing(img *image.RGBA, frac float64, clr color.Color) {
	frac = min(max(frac, 0), 1)
	b := img.Bounds()
	cx, cy := float64(b.Dx())/2, float64(b.Dy())/2
	fillDeg := 360 * frac
	for y := b.Min.Y; y < b.Max.Y; y++ {
		dy := float64(y) + 0.5 - cy
		for x := b.Min.X; x < b.Max.X; x++ {
			dx := float64(x) + 0.5 - cx
			dist := math.Hypot(dx, dy)
			if dist < styleRingInner || dist > styleRingOuter {
				continue
			}
			ang := math.Atan2(dy, dx)*180/math.Pi + 90
			if ang < 0 {
				ang += 360
			}
			if frac > 0 && ang <= fillDeg {
				img.Set(x, y, clr)
			} else {
				img.Set(x, y, styleTrack)
			}
		}
	}
}
