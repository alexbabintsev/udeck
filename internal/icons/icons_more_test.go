package icons

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestDrawPackIcon(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	if err := DrawPackIcon(img, "lucide/play", img.Bounds(), color.White); err != nil {
		t.Fatal(err)
	}
	if err := DrawPackIcon(img, "lucide/play", image.Rectangle{}, color.White); err != nil {
		t.Fatal("empty target is a no-op")
	}
	for _, bad := range []string{"plain", "other/x", "lucide/does-not-exist", "lucide/../../etc/passwd", "lucide/a/b"} {
		if err := DrawPackIcon(img, bad, img.Bounds(), color.White); err == nil {
			t.Fatalf("%q: expected error", bad)
		}
	}
}

func TestLoadForButtonErrors(t *testing.T) {
	if _, err := LoadForButton("/nonexistent.png", Insets{}, [2]int{0, 0}, ""); err == nil {
		t.Fatal("expected open error")
	}
	bad := filepath.Join(t.TempDir(), "bad.png")
	os.WriteFile(bad, []byte("not an image"), 0o600)
	if _, err := LoadForButton(bad, Insets{}, [2]int{0, 0}, ""); err == nil {
		t.Fatal("expected decode error")
	}
	if _, err := LoadForButton("lucide/nope-nope", Insets{}, [2]int{0, 0}, ""); err == nil {
		t.Fatal("expected missing pack icon error")
	}
}

func TestExpandTildeAndHelpers(t *testing.T) {
	home, _ := os.UserHomeDir()
	if expandTilde("~/a") != filepath.Join(home, "a") || expandTilde("/b") != "/b" || expandTilde("~x") != "~x" {
		t.Fatal("expandTilde")
	}
	if !IsPackIcon("lucide/x") || IsPackIcon("lucide/") || IsPackIcon("/abs.png") {
		t.Fatal("IsPackIcon")
	}
	if r := aspectFitRect(image.Rect(0, 0, 100, 50), image.Rect(0, 0, 10, 10)); r != image.Rect(25, 0, 75, 50) {
		t.Fatalf("aspectFitRect = %v", r)
	}
	if r := aspectFitRect(image.Rectangle{}, image.Rect(0, 0, 1, 1)); r != (image.Rectangle{}) {
		t.Fatal("empty target")
	}
	if lucideStrokeScale(image.Rect(0, 0, 12, 12)) != 1 || lucideStrokeScale(image.Rect(0, 0, 48, 96)) != 2 {
		t.Fatal("lucideStrokeScale")
	}
	if parseIconColor("bad") != color.White || parseIconColor("zzzzzz") != color.White {
		t.Fatal("parseIconColor fallback")
	}
	if w, h := SlotDimensions(3, 2); w != WideWidth || h != IconHeight {
		t.Fatal("SlotDimensions")
	}
}
