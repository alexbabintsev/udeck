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

	data, err := LoadForButton(path, Insets{Top: 20, Right: 30, Bottom: 40, Left: 50}, [2]int{0, 0}, "")
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

	data, err := LoadForButton(path, Insets{Left: 20, Right: 30, Top: 10, Bottom: 15}, [2]int{3, 2}, "")
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

func TestLoadForButtonLoadsLucideIconPackAsset(t *testing.T) {
	data, err := LoadForButton("lucide/a-arrow-down", Insets{Top: 18, Right: 18, Bottom: 18, Left: 18}, [2]int{0, 0}, "")
	if err != nil {
		t.Fatalf("load lucide icon: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode jpeg: %v", err)
	}

	if got := img.Bounds().Dx(); got != IconWidth {
		t.Fatalf("expected width %d, got %d", IconWidth, got)
	}
	if got := img.Bounds().Dy(); got != IconHeight {
		t.Fatalf("expected height %d, got %d", IconHeight, got)
	}

	corner := color.RGBAModel.Convert(img.At(4, 4)).(color.RGBA)
	if corner.R > 20 || corner.G > 20 || corner.B > 20 {
		t.Fatalf("expected black margin corner, got %#v", corner)
	}

	topGap := color.RGBAModel.Convert(img.At(IconWidth/2, 20)).(color.RGBA)
	if topGap.R > 20 || topGap.G > 20 || topGap.B > 20 {
		t.Fatalf("expected centered top gap outside 128x128 icon, got %#v", topGap)
	}

	bottomGap := color.RGBAModel.Convert(img.At(IconWidth/2, IconHeight-20)).(color.RGBA)
	if bottomGap.R > 20 || bottomGap.G > 20 || bottomGap.B > 20 {
		t.Fatalf("expected centered bottom gap outside 128x128 icon, got %#v", bottomGap)
	}

	var litPixels int
	for y := 36; y < 160; y += 8 {
		for x := 36; x < 160; x += 8 {
			px := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if px.R > 60 || px.G > 60 || px.B > 60 {
				litPixels++
			}
		}
	}
	if litPixels == 0 {
		t.Fatal("expected rendered lucide strokes in icon body")
	}
}

func TestLoadForButtonAppliesLucideIconColor(t *testing.T) {
	data, err := LoadForButton("lucide/a-arrow-down", Insets{Top: 18, Right: 18, Bottom: 18, Left: 18}, [2]int{0, 0}, "33BBFF")
	if err != nil {
		t.Fatalf("load lucide icon: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode jpeg: %v", err)
	}

	var found bool
	for y := 40; y < 156 && !found; y += 4 {
		for x := 40; x < 156; x += 4 {
			px := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if px.B > 120 && px.G > 100 && px.R < 100 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("expected blue-tinted lucide stroke pixels")
	}
}

func TestLoadForButtonCentersLucideIconInWideSlot(t *testing.T) {
	data, err := LoadForButton("lucide/a-arrow-down", Insets{}, [2]int{3, 2}, "33BBFF")
	if err != nil {
		t.Fatalf("load lucide icon: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode jpeg: %v", err)
	}

	leftGap := color.RGBAModel.Convert(img.At(80, IconHeight/2)).(color.RGBA)
	if leftGap.R > 20 || leftGap.G > 20 || leftGap.B > 20 {
		t.Fatalf("expected left gap on wide slot, got %#v", leftGap)
	}

	rightGap := color.RGBAModel.Convert(img.At(WideWidth-80, IconHeight/2)).(color.RGBA)
	if rightGap.R > 20 || rightGap.G > 20 || rightGap.B > 20 {
		t.Fatalf("expected right gap on wide slot, got %#v", rightGap)
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
