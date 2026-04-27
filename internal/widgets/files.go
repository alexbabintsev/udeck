package widgets

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
)

func writePNGAtomic(path string, img image.Image) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-*.png")
	if err != nil {
		return fmt.Errorf("create temp png: %w", err)
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	if err := png.Encode(f, img); err != nil {
		f.Close()
		return fmt.Errorf("encode png: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp png: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp png: %w", err)
	}
	return nil
}
