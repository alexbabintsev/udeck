package protocol

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

func TestPageZIPUsesDifferentIconNamesForDifferentPages(t *testing.T) {
	dir := t.TempDir()
	iconA := filepath.Join(dir, "a.png")
	iconB := filepath.Join(dir, "b.png")
	writeTestPNG(t, iconA, color.RGBA{0xff, 0x00, 0x00, 0xff})
	writeTestPNG(t, iconB, color.RGBA{0x00, 0x00, 0xff, 0xff})

	zipA, err := PageZIP([]config.Button{
		{At: [2]int{0, 0}, Label: "Apps", Icon: iconA},
	})
	if err != nil {
		t.Fatalf("page zip A: %v", err)
	}

	zipB, err := PageZIP([]config.Button{
		{At: [2]int{0, 0}, Label: "Terminal", Icon: iconB},
	})
	if err != nil {
		t.Fatalf("page zip B: %v", err)
	}

	iconPathA := manifestIconPath(t, zipA, "0_0")
	iconPathB := manifestIconPath(t, zipB, "0_0")
	if iconPathA == iconPathB {
		t.Fatalf("expected different icon filenames across pages, got %q", iconPathA)
	}
}

func TestPageZIPUsesDifferentIconNamesForDifferentMargins(t *testing.T) {
	dir := t.TempDir()
	iconPath := filepath.Join(dir, "a.png")
	writeTestPNG(t, iconPath, color.RGBA{0xff, 0x00, 0x00, 0xff})

	zipA, err := PageZIP([]config.Button{
		{At: [2]int{0, 0}, Label: "Apps", Icon: iconPath, IconMargins: &config.Insets{Top: 4, Right: 4, Bottom: 4, Left: 4}},
	})
	if err != nil {
		t.Fatalf("page zip A: %v", err)
	}

	zipB, err := PageZIP([]config.Button{
		{At: [2]int{0, 0}, Label: "Apps", Icon: iconPath, IconMargins: &config.Insets{Top: 12, Right: 12, Bottom: 12, Left: 12}},
	})
	if err != nil {
		t.Fatalf("page zip B: %v", err)
	}

	iconPathA := manifestIconPath(t, zipA, "0_0")
	iconPathB := manifestIconPath(t, zipB, "0_0")
	if iconPathA == iconPathB {
		t.Fatalf("expected different icon filenames across margin changes, got %q", iconPathA)
	}
}

func TestPageZIPBuildsWorkFolderFromExampleConfig(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", "..", "examples", "test-config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	var work *config.Folder
	for _, page := range cfg.PagesOrDefault() {
		for _, btn := range page.Buttons {
			if btn.Label == "Work" {
				work = btn.Folder
				break
			}
		}
		if work != nil {
			break
		}
	}
	if work == nil {
		t.Fatal("work folder not found in example config")
	}

	if _, err := PageZIP(append([]config.Button(nil), work.Buttons...)); err != nil {
		t.Fatalf("expected Work folder ZIP to build, got %v", err)
	}
}

func TestPageZIPUsesWideButtonIconDimensions(t *testing.T) {
	dir := t.TempDir()
	iconPath := filepath.Join(dir, "wide.png")
	writeTestPNG(t, iconPath, color.RGBA{0x00, 0x80, 0xff, 0xff})

	zipData, err := PageZIP([]config.Button{
		{At: [2]int{3, 2}, Label: "Wide", Icon: iconPath, IconMargins: &config.Insets{Left: 20, Right: 30, Top: 10, Bottom: 15}},
	})
	if err != nil {
		t.Fatalf("page zip: %v", err)
	}

	iconName := manifestIconPath(t, zipData, "3_2")
	jpegData := zipEntryBytes(t, zipData, iconName)
	img, err := jpeg.Decode(bytes.NewReader(jpegData))
	if err != nil {
		t.Fatalf("decode wide button jpeg: %v", err)
	}

	if got := img.Bounds().Dx(); got != 458 {
		t.Fatalf("expected wide button width 458, got %d", got)
	}
	if got := img.Bounds().Dy(); got != 196 {
		t.Fatalf("expected wide button height 196, got %d", got)
	}

	leftGap := color.RGBAModel.Convert(img.At(40, 98)).(color.RGBA)
	if leftGap.R > 20 || leftGap.G > 20 || leftGap.B > 20 {
		t.Fatalf("expected wide button letterbox gap, got %#v", leftGap)
	}
}

func TestPageZIPOmitsNonexistentBottomRightSlot(t *testing.T) {
	zipData, err := PageZIP(nil)
	if err != nil {
		t.Fatalf("page zip: %v", err)
	}

	manifest := manifestData(t, zipData)
	if _, ok := manifest["4_2"]; ok {
		t.Fatal("expected manifest to omit nonexistent slot 4_2")
	}
	if _, ok := manifest["3_2"]; !ok {
		t.Fatal("expected manifest to include wide slot 3_2")
	}
}

func manifestIconPath(t *testing.T, zipData []byte, key string) string {
	t.Helper()
	return manifestData(t, zipData)[key].ViewParam[0].Icon
}

func manifestData(t *testing.T, zipData []byte) map[string]struct {
	ViewParam []struct {
		Icon string `json:"Icon,omitempty"`
	} `json:"ViewParam"`
} {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	var manifestBytes []byte
	for _, f := range zr.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open manifest: %v", err)
		}
		manifestBytes, err = io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read manifest: %v", err)
		}
		break
	}
	if len(manifestBytes) == 0 {
		t.Fatal("manifest.json not found")
	}

	var manifest map[string]struct {
		ViewParam []struct {
			Icon string `json:"Icon,omitempty"`
		} `json:"ViewParam"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	return manifest
}

func zipEntryBytes(t *testing.T, zipData []byte, name string) []byte {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return data
	}

	t.Fatalf("zip entry %q not found", name)
	return nil
}

func writeTestPNG(t *testing.T, path string, c color.Color) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, c)
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
