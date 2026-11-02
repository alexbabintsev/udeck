package widgets

import (
	"errors"
	"sync"
	"testing"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/macos"
)

type fakeMedia struct {
	mu      sync.Mutex
	pressed []macos.MediaKey
	asked   int
}

func useFakeMedia(t *testing.T, trusted bool, pressErr error) *fakeMedia {
	t.Helper()
	f := &fakeMedia{}
	old := systemMedia
	systemMedia = mediaSystem{
		press: func(k macos.MediaKey) error {
			f.mu.Lock()
			f.pressed = append(f.pressed, k)
			f.mu.Unlock()
			return pressErr
		},
		trusted: func() bool {
			f.mu.Lock()
			f.asked++
			f.mu.Unlock()
			return trusted
		},
	}
	t.Cleanup(func() { systemMedia = old })
	return f
}

func TestMediaWidgetPressesItsKey(t *testing.T) {
	f := useFakeMedia(t, true, nil)
	for action, key := range map[string]macos.MediaKey{"play_pause": macos.MediaPlayPause, "next": macos.MediaNext, "previous": macos.MediaPrevious} {
		w, err := NewMediaWidget("m-"+action, &config.Widget{Type: "media", Action: action}, nil)
		if err != nil {
			t.Fatal(err)
		}
		readIcon(t, w.IconPath())
		f.pressed = nil
		w.HandleGesture(ig.Tap)
		w.HandleGesture(ig.Hold) // ignored
		if len(f.pressed) != 1 || f.pressed[0] != key {
			t.Fatalf("%s pressed %v", action, f.pressed)
		}
		_ = w.Close()
		w.HandleGesture(ig.Tap) // closed: ignored
		if len(f.pressed) != 1 {
			t.Fatal("a closed widget must not press")
		}
	}
	if _, err := NewMediaWidget("bad", &config.Widget{Type: "media", Action: "stop"}, nil); err == nil {
		t.Fatal("expected an unknown action error")
	}
}

func TestMediaWidgetUntrustedStillPresses(t *testing.T) {
	f := useFakeMedia(t, false, errors.New("denied"))
	w, err := NewMediaWidget("m", &config.Widget{Type: "media", Action: "next"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.HandleGesture(ig.Tap)
	w.HandleGesture(ig.Tap)
	if len(f.pressed) != 2 {
		t.Fatalf("pressed %v", f.pressed)
	}
}

func TestRenderMediaVariants(t *testing.T) {
	seen := map[string]bool{}
	for _, action := range []string{"play_pause", "next", "previous"} {
		for _, title := range []string{"", "Music"} {
			img := renderMedia(action, title)
			if !hasColor(img, styleText) {
				t.Fatalf("%s/%q: nothing drawn", action, title)
			}
			seen[string(img.Pix)] = true
		}
	}
	if len(seen) != 6 {
		t.Fatalf("expected 6 distinct images, got %d", len(seen))
	}
}
