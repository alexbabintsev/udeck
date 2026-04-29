package icons

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/draw"
)

const (
	IconWidth    = 196
	IconHeight   = 196
	WideWidth    = 458
	PackIconSize = 128
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
	return loadSized(path, margins, IconWidth, IconHeight, "")
}

// LoadForButton reads an image file and resizes it to the physical pixel
// size of the target button slot before encoding it as JPEG.
func LoadForButton(path string, margins Insets, at [2]int, iconColor string) ([]byte, error) {
	width, height := SlotDimensions(at[0], at[1])
	return loadSized(path, margins, width, height, iconColor)
}

func loadSized(path string, margins Insets, width, height int, iconColor string) ([]byte, error) {
	if IsPackIcon(path) {
		return loadPackSized(path, margins, width, height, iconColor)
	}

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

type iconRef struct {
	Pack string
	Name string
}

func IsPackIcon(id string) bool {
	_, ok := parseIconRef(id)
	return ok
}

func parseIconRef(id string) (iconRef, bool) {
	pack, name, ok := strings.Cut(strings.TrimSpace(id), "/")
	if !ok || pack == "" || name == "" {
		return iconRef{}, false
	}
	switch pack {
	case "lucide":
		return iconRef{Pack: pack, Name: name}, true
	default:
		return iconRef{}, false
	}
}

func loadPackSized(id string, margins Insets, width, height int, iconColor string) ([]byte, error) {
	ref, ok := parseIconRef(id)
	if !ok {
		return nil, fmt.Errorf("unknown icon pack id %q", id)
	}

	switch ref.Pack {
	case "lucide":
		return loadLucideSized(ref.Name, margins, width, height, iconColor)
	default:
		return nil, fmt.Errorf("unsupported icon pack %q", ref.Pack)
	}
}

func DrawPackIcon(dst stddraw.Image, id string, target image.Rectangle, clr color.Color) error {
	ref, ok := parseIconRef(id)
	if !ok {
		return fmt.Errorf("unknown icon pack id %q", id)
	}
	if target.Empty() {
		return nil
	}

	switch ref.Pack {
	case "lucide":
		return drawLucideIcon(dst, ref.Name, target, clr)
	default:
		return fmt.Errorf("unsupported icon pack %q", ref.Pack)
	}
}

func loadLucideSized(name string, _ Insets, width, height int, iconColor string) ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	stddraw.Draw(dst, dst.Bounds(), image.Black, image.Point{}, stddraw.Src)

	size := PackIconSize
	if width < size {
		size = width
	}
	if height < size {
		size = height
	}
	left := (width - size) / 2
	top := (height - size) / 2
	target := image.Rect(left, top, left+size, top+size)

	if err := drawLucideIcon(dst, name, target, parseIconColor(iconColor)); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 90}); err != nil {
		return nil, fmt.Errorf("encode icon: %w", err)
	}
	return buf.Bytes(), nil
}

func lucideAssetDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("assets", "lucide", "icons")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "assets", "lucide", "icons")
}

func drawLucideIcon(dst stddraw.Image, name string, target image.Rectangle, clr color.Color) error {
	scale := lucideStrokeScale(target)
	icon, err := readLucideIcon(name, clr, scale)
	if err != nil {
		return err
	}

	// Lucide icons use a 24x24 viewBox. By rendering into the requested
	// target rectangle, the SVG transform naturally scales both geometry
	// and stroke width relative to the final icon size.
	canvas := image.NewRGBA(image.Rect(0, 0, target.Dx(), target.Dy()))
	icon.SetTarget(0, 0, float64(target.Dx()), float64(target.Dy()))
	scanner := rasterx.NewScannerGV(target.Dx(), target.Dy(), canvas, canvas.Bounds())
	raster := rasterx.NewDasher(target.Dx(), target.Dy(), scanner)
	icon.Draw(raster, 1)
	stddraw.Draw(dst, target, canvas, image.Point{}, stddraw.Over)
	return nil
}

func parseIconColor(s string) color.Color {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return color.White
	}
	var rgb uint32
	if _, err := fmt.Sscanf(s, "%06x", &rgb); err != nil {
		return color.White
	}
	return color.RGBA{
		R: uint8(rgb >> 16),
		G: uint8((rgb >> 8) & 0xff),
		B: uint8(rgb & 0xff),
		A: 0xff,
	}
}

func readLucideIcon(name string, clr color.Color, strokeScale float64) (*oksvg.SvgIcon, error) {
	iconPath := filepath.Join(lucideAssetDir(), name+".svg")
	data, err := os.ReadFile(iconPath)
	if err != nil {
		return nil, fmt.Errorf("read lucide icon %q: %w", name, err)
	}

	svg := normalizeLucideSVG(data, clr, strokeScale)
	icon, err := oksvg.ReadIconStream(strings.NewReader(svg))
	if err != nil {
		return nil, fmt.Errorf("parse lucide icon %q: %w", name, err)
	}
	return icon, nil
}

func normalizeLucideSVG(data []byte, clr color.Color, strokeScale float64) string {
	svg := string(data)
	if idx := strings.Index(svg, "<svg"); idx >= 0 {
		svg = svg[idx:]
	}
	svg = strings.ReplaceAll(svg, "currentColor", hexColor(clr))
	if strokeScale > 1 {
		svg = strings.ReplaceAll(svg, `stroke-width="2"`, fmt.Sprintf(`stroke-width="%.3f"`, 2*strokeScale))
	}
	return svg
}

func lucideStrokeScale(target image.Rectangle) float64 {
	size := target.Dx()
	if target.Dy() < size {
		size = target.Dy()
	}
	if size <= 24 {
		return 1
	}
	return float64(size) / 24.0
}

func hexColor(clr color.Color) string {
	r, g, b, _ := clr.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}
