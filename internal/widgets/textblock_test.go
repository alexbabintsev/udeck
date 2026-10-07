package widgets

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font"
)

func TestFitFaceShrinksThenTruncatesAndTerminates(t *testing.T) {
	SetFont("")
	face, got := fitFace("Hi", 500, 40, 10, 1, false)
	if face == nil || got != "Hi" {
		t.Fatalf("short text must fit unchanged, got %q", got)
	}
	face.Close()

	// Regression: text that cannot fit even at the minimum size used to
	// loop forever re-appending "…" to a same-length candidate.
	done := make(chan string, 1)
	go func() {
		_, display := fitFace(strings.Repeat("Very long track title ", 10), 40, 30, 10, 1.5, true)
		done <- display
	}()
	select {
	case display := <-done:
		if !strings.HasSuffix(display, "…") || len([]rune(display)) >= 30 {
			t.Fatalf("expected a short ellipsized string, got %q", display)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fitFace did not terminate")
	}

	// Nothing fits at all: still returns a face and the shortest candidate.
	face, display := fitFace("WWWW", 1, 20, 10, 1, false)
	if face == nil || display != "W…" {
		t.Fatalf("got %q", display)
	}
	face.Close()

	if face, display := fitFace("   ", 100, 20, 10, 1, false); face != nil || display != "" {
		t.Fatal("blank text must yield no face")
	}
}

func TestFitFacePicksLargestFittingSize(t *testing.T) {
	small, _ := fitFace("12345", 60, 60, 10, 1, false)
	large, _ := fitFace("12345", 300, 60, 10, 1, false)
	defer small.Close()
	defer large.Close()
	if font.MeasureString(small, "12345").Round() > 60 {
		t.Fatal("chosen face must fit the width")
	}
	if large.Metrics().Height <= small.Metrics().Height {
		t.Fatal("wider box must allow a larger size")
	}
}

func TestTextDrawingHelpers(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 458, 196))
	for _, a := range []TextAlign{AlignTopLeft, AlignTopCenter, AlignTopRight, AlignMiddleLeft, AlignMiddleCenter, AlignMiddleRight, AlignBottomLeft, AlignBottomCenter, AlignBottomRight} {
		drawText(img, "Ab", CenterX(0), CenterY(0), 100, 50, a, color.White, 30, false, 2)
	}
	drawText(img, "", FromRight(0), FromBottom(0), 10, 10, AlignTopLeft, color.White, 10, false)
	drawText(img, "x", FromRight(5), FromBottom(5), 50, 50, AlignTopLeft, color.White, 20, true)
	drawIcon(img, "lucide/wind", FromLeft(0), FromTop(0), 40, 40, color.White)
	drawWeatherIconAt(img, 61, FromLeft(10), FromTop(10), 40, 40, color.White)
	drawFittedLeftText(img, "Left", image.Rect(0, 0, 100, 30), color.White, 20, false)
	drawFittedLeftTextTracked(img, "Tracked", image.Rect(0, 0, 100, 30), color.White, 20, true, 2)
	drawClockText(img, "12:34", image.Rect(0, 0, 300, 120), color.White, color.Gray{0x77}, 100, -4)
	drawClockText(img, "noon", image.Rect(0, 0, 300, 120), color.White, color.Gray{0x77}, 100, 0)
	drawDashboardCenteredText(img, "42", image.Rect(0, 0, 100, 100), color.White, 60)
	drawDashboardLeftText(img, "", image.Rect(0, 0, 100, 100), color.White, 60)
	drawFreqtradeText(img, "+1.23%", image.Rect(0, 0, 100, 40), color.White, 30)
	drawFreqtradeText(img, "", image.Rect(0, 0, 100, 40), color.White, 30)
	if trackedTextWidth(nil, "", 2) != 0 {
		t.Fatal("empty text width")
	}
}
