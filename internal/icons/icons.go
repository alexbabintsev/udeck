package icons

import (
	"bytes"
	"fmt"
	"image"
	stddraw "image/draw"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

const (
	IconWidth  = 196
	IconHeight = 196
	WideWidth  = 458
)

type Insets struct {
	Top    int
	Right  int
	Bottom int
	Left   int
}

// SlotDimensions returns the physical pixel size for a button slot.
// The D200 uses 196×196 buttons everywhere except slot [3,2], which is
// a double-wide 458×196 display.
func SlotDimensions(col, row int) (width, height int) {
	if col == 3 && row == 2 {
		return WideWidth, IconHeight
	}
	return IconWidth, IconHeight
}

// Load reads an image file from path (~ expanded), resizes it to 196×196
// using bilinear interpolation, and returns JPEG-encoded bytes.
func Load(path string, margins Insets) ([]byte, error) {
	return loadSized(path, margins, IconWidth, IconHeight)
}

// LoadForButton reads an image file and resizes it to the physical pixel
// size of the target button slot before encoding it as JPEG.
func LoadForButton(path string, margins Insets, at [2]int) ([]byte, error) {
	width, height := SlotDimensions(at[0], at[1])
	return loadSized(path, margins, width, height)
}

func loadSized(path string, margins Insets, width, height int) ([]byte, error) {
	path = expandTilde(path)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open icon %q: %w", path, err)
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode icon %q: %w", path, err)
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	stddraw.Draw(dst, dst.Bounds(), image.Black, image.Point{}, stddraw.Src)
	target := image.Rect(
		margins.Left,
		margins.Top,
		width-margins.Right,
		height-margins.Bottom,
	)
	if width != height {
		target = aspectFitRect(target, src.Bounds())
	}
	draw.BiLinear.Scale(dst, target, src, src.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 90}); err != nil {
		return nil, fmt.Errorf("encode icon: %w", err)
	}
	return buf.Bytes(), nil
}

func aspectFitRect(target image.Rectangle, src image.Rectangle) image.Rectangle {
	if target.Empty() || src.Empty() {
		return target
	}

	targetW := float64(target.Dx())
	targetH := float64(target.Dy())
	srcW := float64(src.Dx())
	srcH := float64(src.Dy())
	scale := math.Min(targetW/srcW, targetH/srcH)
	if scale <= 0 {
		return target
	}

	fittedW := int(math.Round(srcW * scale))
	fittedH := int(math.Round(srcH * scale))
	if fittedW < 1 {
		fittedW = 1
	}
	if fittedH < 1 {
		fittedH = 1
	}

	left := target.Min.X + (target.Dx()-fittedW)/2
	top := target.Min.Y + (target.Dy()-fittedH)/2
	return image.Rect(left, top, left+fittedW, top+fittedH)
}

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
