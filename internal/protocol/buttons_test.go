package protocol

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
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

func manifestIconPath(t *testing.T, zipData []byte, key string) string {
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
	return manifest[key].ViewParam[0].Icon
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
