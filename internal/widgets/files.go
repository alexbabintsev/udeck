package widgets

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
)

func writePNGAtomic(path string, img image.Image) error {
	_, err := writePNGAtomicIfChanged(path, img)
	return err
}

func writePNGAtomicIfChanged(path string, img image.Image) (bool, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return false, fmt.Errorf("encode png: %w", err)
	}
	data := buf.Bytes()
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return false, nil
	}

	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-*.png")
	if err != nil {
		return false, fmt.Errorf("create temp png: %w", err)
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	if _, err := f.Write(data); err != nil {
		f.Close()
		return false, fmt.Errorf("write temp png: %w", err)
	}
	if err := f.Close(); err != nil {
		return false, fmt.Errorf("close temp png: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return false, fmt.Errorf("rename temp png: %w", err)
	}
	return true, nil
}
