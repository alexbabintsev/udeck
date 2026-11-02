package widgets

import (
	"fmt"
	"image"
	"log"
	"strings"
	"sync"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/macos"
)

// mediaSystem posts media keys; tests replace systemMedia.
type mediaSystem struct {
	press   func(macos.MediaKey) error
	trusted func() bool
}

var systemMedia = mediaSystem{press: macos.PressMediaKey, trusted: macos.AccessibilityTrusted}

// MediaWidget is a play/pause, next or previous key that works like the
// keyboard's media keys, for whatever app is playing: Music, Spotify, a
// browser tab, a podcast app.
type MediaWidget struct {
	*widgetBase
	sys    mediaSystem
	key    macos.MediaKey
	action string

	warnOnce sync.Once
}

func NewMediaWidget(id string, cfg *config.Widget, notify func(string)) (*MediaWidget, error) {
	key, err := macos.ParseMediaKey(cfg.Action)
	if err != nil {
		return nil, err
	}
	base, err := newWidgetBase(id, "media", notify)
	if err != nil {
		return nil, err
	}
	w := &MediaWidget{widgetBase: base, sys: systemMedia, key: key, action: cfg.Action}
	if _, err := w.writeIcon(renderMedia(cfg.Action, strings.TrimSpace(cfg.Title))); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write media icon: %w", err)
	}
	return w, nil
}

func (w *MediaWidget) HandleGesture(kind ig.Kind) {
	if kind != ig.Tap || w.isClosed() {
		return
	}
	if !w.sys.trusted() {
		w.warnOnce.Do(func() {
			log.Printf("media keys need Accessibility access for the app running ulanzi-deck (System Settings → Privacy & Security → Accessibility)")
		})
	}
	if err := w.sys.press(w.key); err != nil {
		debug.Logf("media %s: %v", w.action, err)
	}
}

// renderMedia draws the transport symbol; play_pause shows both halves
// because the key cannot know what is playing.
func renderMedia(action, title string) *image.RGBA {
	img := newCard()
	switch action {
	case "play_pause":
		if title != "" {
			drawIcon(img, "lucide/play", CenterX(-26), FromTop(styleIconTop+12), 72, 72, styleText)
			drawIcon(img, "lucide/pause", CenterX(30), FromTop(styleIconTop+12), 72, 72, styleText)
			drawCardLabel(img, title, styleText)
			return img
		}
		drawIcon(img, "lucide/play", CenterX(-34), CenterY(0), 84, 84, styleText)
		drawIcon(img, "lucide/pause", CenterX(38), CenterY(0), 84, 84, styleText)
	case "next":
		drawIconCard(img, "lucide/skip-forward", styleText, title, styleText)
	case "previous":
		drawIconCard(img, "lucide/skip-back", styleText, title, styleText)
	}
	return img
}
