package widgets

import (
	"image"
	"image/color"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// TextAnchorEdge specifies which edge (or center axis) an anchor is relative to.
type TextAnchorEdge int

const (
	AnchorLeft    TextAnchorEdge = iota // offset from left edge
	AnchorRight                         // offset from right edge
	AnchorTop                           // offset from top edge
	AnchorBottom                        // offset from bottom edge
	AnchorCenterH                       // offset from horizontal center
	AnchorCenterV                       // offset from vertical center
)

// TextAnchor positions a block relative to a canvas edge.
// Offset is in pixels inward from the named edge (or from center).
type TextAnchor struct {
	Edge   TextAnchorEdge
	Offset int
}

// Convenience constructors.
func FromLeft(px int) TextAnchor   { return TextAnchor{AnchorLeft, px} }
func FromRight(px int) TextAnchor  { return TextAnchor{AnchorRight, px} }
func FromTop(px int) TextAnchor    { return TextAnchor{AnchorTop, px} }
func FromBottom(px int) TextAnchor { return TextAnchor{AnchorBottom, px} }
func CenterX(offset int) TextAnchor { return TextAnchor{AnchorCenterH, offset} }
func CenterY(offset int) TextAnchor { return TextAnchor{AnchorCenterV, offset} }

// TextAlign controls where text sits within the block (9 positions).
type TextAlign int

const (
	AlignTopLeft      TextAlign = iota
	AlignTopCenter
	AlignTopRight
	AlignMiddleLeft
	AlignMiddleCenter
	AlignMiddleRight
	AlignBottomLeft
	AlignBottomCenter
	AlignBottomRight
)

const textMinSize = 18.0

// drawText renders text on img.
//
//   anchorX, anchorY — position the top-left corner of the text block
//   blockW, blockH   — block dimensions; text shrinks to fit width
//   align            — where text sits inside the block (9 positions)
//   size             — starting font size; shrinks to fit, minimum 18
//   bold             — use bold/regular face
//   tracking         — optional letter-spacing in pixels (default 0)
func drawText(
	img *image.RGBA,
	text string,
	anchorX, anchorY TextAnchor,
	blockW, blockH int,
	align TextAlign,
	clr color.Color,
	size float64,
	bold bool,
	tracking ...float64,
) {
	if text == "" {
		return
	}
	bounds := img.Bounds()
	canvasW := bounds.Dx()
	canvasH := bounds.Dy()

	blockX := resolveAnchorX(anchorX, blockW, canvasW)
	blockY := resolveAnchorY(anchorY, blockH, canvasH)

	face, display := textFaceForBlock(text, blockW, size, bold)
	if face == nil {
		return
	}
	defer face.Close()

	tr := 0.0
	if len(tracking) > 0 {
		tr = tracking[0]
	}

	metrics := face.Metrics()
	textH := metrics.Height.Ceil()
	textW := trackedTextWidth(face, display, tr)

	// Horizontal position within block
	var textX int
	switch align {
	case AlignTopLeft, AlignMiddleLeft, AlignBottomLeft:
		textX = blockX
	case AlignTopCenter, AlignMiddleCenter, AlignBottomCenter:
		textX = blockX + (blockW-textW)/2
	case AlignTopRight, AlignMiddleRight, AlignBottomRight:
		textX = blockX + blockW - textW
	}

	// Vertical baseline within block
	var baseline int
	switch align {
	case AlignTopLeft, AlignTopCenter, AlignTopRight:
		baseline = blockY + metrics.Ascent.Ceil()
	case AlignMiddleLeft, AlignMiddleCenter, AlignMiddleRight:
		baseline = blockY + (blockH-textH)/2 + metrics.Ascent.Ceil()
	case AlignBottomLeft, AlignBottomCenter, AlignBottomRight:
		baseline = blockY + blockH - metrics.Descent.Ceil()
	}

	drawLeftFaceTracked(img, display, textX, baseline, clr, face, tr)
}

func resolveAnchorX(a TextAnchor, blockW, canvasW int) int {
	switch a.Edge {
	case AnchorRight:
		return canvasW - a.Offset - blockW
	case AnchorCenterH:
		return canvasW/2 - blockW/2 + a.Offset
	default: // AnchorLeft
		return a.Offset
	}
}

func resolveAnchorY(a TextAnchor, blockH, canvasH int) int {
	switch a.Edge {
	case AnchorBottom:
		return canvasH - a.Offset - blockH
	case AnchorCenterV:
		return canvasH/2 - blockH/2 + a.Offset
	default: // AnchorTop
		return a.Offset
	}
}

// textFaceForBlock returns a face that fits text within maxWidth, min size 18.
func textFaceForBlock(text string, maxWidth int, startSize float64, bold bool) (font.Face, string) {
	ft := loadSpaceMonoFont(bold)
	if ft == nil {
		return nil, text
	}
	if startSize < textMinSize {
		startSize = textMinSize
	}

	display := text
	runes := []rune(display)
	for len(runes) > 0 {
		candidate := string(runes)
		size := startSize
		for size >= textMinSize {
			face, err := opentype.NewFace(ft, &opentype.FaceOptions{
				Size:    size,
				DPI:     72,
				Hinting: font.HintingFull,
			})
			if err != nil {
				break
			}
			if font.MeasureString(face, candidate).Round() <= maxWidth {
				return face, candidate
			}
			face.Close()
			size -= 1
		}
		if len(runes) == 1 {
			break
		}
		runes = runes[:len(runes)-1]
		runes = []rune(string(runes) + "…")
	}

	// fallback at min size
	face, err := opentype.NewFace(ft, &opentype.FaceOptions{
		Size:    textMinSize,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, text
	}
	return face, string(runes)
}

// drawIcon draws a lucide icon by ID into a square inscribed in the block.
// The icon is centered within blockW×blockH; aspect ratio is always 1:1.
func drawIcon(
	img *image.RGBA,
	id string,
	anchorX, anchorY TextAnchor,
	blockW, blockH int,
	clr color.Color,
) {
	bounds := img.Bounds()
	bx := resolveAnchorX(anchorX, blockW, bounds.Dx())
	by := resolveAnchorY(anchorY, blockH, bounds.Dy())
	// inscribe a square, centered
	side := blockW
	if blockH < side {
		side = blockH
	}
	x := bx + (blockW-side)/2
	y := by + (blockH-side)/2
	rect := image.Rect(x, y, x+side, y+side)
	_ = icons.DrawPackIcon(img, id, rect, clr)
}

// drawWeatherIconAt draws the weather icon for a WMO code using drawIcon.
func drawWeatherIconAt(
	img *image.RGBA,
	code int,
	anchorX, anchorY TextAnchor,
	blockW, blockH int,
	clr color.Color,
) {
	drawIcon(img, weatherCodeToLucideID(code), anchorX, anchorY, blockW, blockH, clr)
}

func weatherCodeToLucideID(code int) string {
	switch {
	case code == 0:
		return "lucide/sun"
	case code == 1 || code == 2:
		return "lucide/cloud-sun"
	case code >= 95:
		return "lucide/cloud-lightning"
	case code >= 51 && code <= 67, code >= 80 && code <= 82:
		return "lucide/cloud-rain"
	case code >= 71 && code <= 77:
		return "lucide/cloud-snow"
	case code >= 45 && code <= 48:
		return "lucide/cloud-fog"
	default:
		return "lucide/cloud"
	}
}
