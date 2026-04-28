package icons

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesMargins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "icon.png")
	writeSolidPNG(t, path, color.RGBA{0xff, 0x20, 0x20, 0xff})

	data, err := Load(path, Insets{Top: 20, Right: 30, Bottom: 40, Left: 50})
	if err != nil {
		t.Fatalf("load icon: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode jpeg: %v", err)
	}

	corner := color.RGBAModel.Convert(img.At(5, 5)).(color.RGBA)
	if corner.R > 20 || corner.G > 20 || corner.B > 20 {
		t.Fatalf("expected black margin corner, got %#v", corner)
	}

	center := color.RGBAModel.Convert(img.At(98, 98)).(color.RGBA)
	if center.R < 150 || center.G > 80 || center.B > 80 {
		t.Fatalf("expected centered icon color, got %#v", center)
	}
}

func TestLoadForButtonUsesWideSlotDimensions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "icon.png")
	writeSolidPNG(t, path, color.RGBA{0x20, 0x80, 0xff, 0xff})

	data, err := LoadForButton(path, Insets{Left: 20, Right: 30, Top: 10, Bottom: 15}, [2]int{3, 2})
	if err != nil {
		t.Fatalf("load wide button icon: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode jpeg: %v", err)
	}

	if got := img.Bounds().Dx(); got != WideWidth {
		t.Fatalf("expected width %d, got %d", WideWidth, got)
	}
	if got := img.Bounds().Dy(); got != IconHeight {
		t.Fatalf("expected height %d, got %d", IconHeight, got)
	}

	corner := color.RGBAModel.Convert(img.At(5, 5)).(color.RGBA)
	if corner.R > 20 || corner.G > 20 || corner.B > 20 {
		t.Fatalf("expected black margin corner, got %#v", corner)
	}

	center := color.RGBAModel.Convert(img.At(WideWidth/2, IconHeight/2)).(color.RGBA)
	if center.B < 150 {
		t.Fatalf("expected centered icon body, got %#v", center)
	}

	leftGap := color.RGBAModel.Convert(img.At(40, IconHeight/2)).(color.RGBA)
	if leftGap.R > 20 || leftGap.G > 20 || leftGap.B > 20 {
		t.Fatalf("expected left letterbox gap, got %#v", leftGap)
	}
}

func writeSolidPNG(t *testing.T, path string, clr color.Color) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, clr)
		}
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer f.Close()

	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
}
