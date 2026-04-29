package widgets

import (
	"bytes"
	"image/color"
	"image/png"
	"os"
	"sync"
	"testing"

	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
)

func TestMemoryWidgetRendersRingAndDigits(t *testing.T) {
	dir := t.TempDir()
	w := &MemoryWidget{
		id:       "test",
		iconPath: dir + "/memory.png",
		percent:  72,
	}

	changed, err := w.render()
	if err != nil {
		t.Fatalf("render memory widget: %v", err)
	}
	if !changed {
		t.Fatal("expected first render to write image")
	}

	data, err := os.ReadFile(w.iconPath)
	if err != nil {
		t.Fatalf("read rendered image: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode rendered image: %v", err)
	}

	bg := color.RGBAModel.Convert(img.At(8, 8)).(color.RGBA)
	if bg.R > 4 || bg.G > 4 || bg.B > 4 {
		t.Fatalf("expected black background, got %#v", bg)
	}

	ring := color.RGBAModel.Convert(img.At(memoryWidgetW/2, 30)).(color.RGBA)
	if ring.R < 100 || ring.G < 100 {
		t.Fatalf("expected colored ring pixel, got %#v", ring)
	}

	foundDigits := false
	for y := 68; y < 128 && !foundDigits; y++ {
		for x := 56; x < 140; x++ {
			px := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if px.R > 100 || px.G > 100 || px.B > 100 {
				foundDigits = true
				break
			}
		}
	}
	if !foundDigits {
		t.Fatal("expected colored center digits")
	}
}

func TestMemoryWidgetBusyRendersLucideIcon(t *testing.T) {
	dir := t.TempDir()
	w := &MemoryWidget{
		id:       "test",
		iconPath: dir + "/memory.png",
		percent:  86,
		busy:     true,
	}

	if _, err := w.render(); err != nil {
		t.Fatalf("render busy memory widget: %v", err)
	}

	data, err := os.ReadFile(w.iconPath)
	if err != nil {
		t.Fatalf("read rendered image: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode rendered image: %v", err)
	}

	foundAccent := false
	for y := 58; y < 138 && !foundAccent; y++ {
		for x := 58; x < 138; x++ {
			px := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if px.R > 150 && px.G < 120 && px.B < 120 {
				foundAccent = true
				break
			}
		}
	}
	if !foundAccent {
		t.Fatal("expected lucide busy icon pixels")
	}
}

func TestMemoryWidgetTapShowsBusyIconImmediately(t *testing.T) {
	oldRestart := restartAppFunc
	defer func() { restartAppFunc = oldRestart }()

	block := make(chan struct{})
	restartAppFunc = func(appName string) error {
		<-block
		return nil
	}

	dir := t.TempDir()
	var (
		mu      sync.Mutex
		updates []string
	)
	w := &MemoryWidget{
		id:       "test",
		iconPath: dir + "/memory.png",
		percent:  63,
		apps:     []string{"Dummy"},
		notify: func(id string) {
			mu.Lock()
			updates = append(updates, id)
			mu.Unlock()
		},
	}
	if _, err := w.render(); err != nil {
		t.Fatalf("initial render: %v", err)
	}

	w.HandleGesture(ig.Tap)

	mu.Lock()
	gotUpdates := len(updates)
	mu.Unlock()
	if gotUpdates == 0 {
		t.Fatal("expected immediate widget update on tap")
	}

	data, err := os.ReadFile(w.iconPath)
	if err != nil {
		t.Fatalf("read busy image: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode busy image: %v", err)
	}

	foundAccent := false
	for y := 58; y < 138 && !foundAccent; y++ {
		for x := 58; x < 138; x++ {
			px := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if px.G > 120 && px.G > px.R {
				foundAccent = true
				break
			}
		}
	}
	close(block)
	if !foundAccent {
		t.Fatal("expected busy icon to render immediately after tap")
	}
}
