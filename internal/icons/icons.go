package icons

import (
	"bytes"
	"fmt"
	"image"
	stddraw "image/draw"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

const (
	IconWidth  = 196
	IconHeight = 196
)

type Insets struct {
	Top    int
	Right  int
	Bottom int
	Left   int
}

// Load reads an image file from path (~ expanded), resizes it to 196×196
// using bilinear interpolation, and returns JPEG-encoded bytes.
func Load(path string, margins Insets) ([]byte, error) {
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

	dst := image.NewRGBA(image.Rect(0, 0, IconWidth, IconHeight))
	stddraw.Draw(dst, dst.Bounds(), image.Black, image.Point{}, stddraw.Src)
	target := image.Rect(
		margins.Left,
		margins.Top,
		IconWidth-margins.Right,
		IconHeight-margins.Bottom,
	)
	draw.BiLinear.Scale(dst, target, src, src.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 90}); err != nil {
		return nil, fmt.Errorf("encode icon: %w", err)
	}
	return buf.Bytes(), nil
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
