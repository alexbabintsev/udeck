package widgets

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"sync"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
)

const siteOnlinePath = "/api/metrics/visitors/online"

// siteOnlineStats is the live-visitors payload. It carries no comparison
// window, so there is no percentage change to render.
type siteOnlineStats struct {
	Window        string `json:"window"`
	Visitors      int    `json:"visitors"`
	Views         int    `json:"views"`
	WindowMinutes int    `json:"window_minutes"`
}

// SiteOnlineWidget shows the number of visitors currently on the site.
type SiteOnlineWidget struct {
	*widgetBase
	api    siteAPI
	pollMS time.Duration

	mu           sync.Mutex
	stats        *siteOnlineStats
	lastFrameKey string
}

func NewSiteOnlineWidget(id string, cfg *config.Widget, notify func(string)) (*SiteOnlineWidget, error) {
	base, err := newWidgetBase(id, "site-online", "site_online.png", notify)
	if err != nil {
		return nil, err
	}
	w := &SiteOnlineWidget{widgetBase: base, api: newSiteAPI(cfg), pollMS: sitePoll(cfg.PollMS)}
	// Render the placeholder so the icon exists before the first fetch.
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write site online icon: %w", err)
	}
	w.every(w.pollMS, func(time.Time) { w.refresh() })
	go w.refresh()
	return w, nil
}

// HandleGesture forces an immediate refresh on tap.
func (w *SiteOnlineWidget) HandleGesture(kind ig.Kind) {
	if kind == ig.Tap {
		go w.refresh()
	}
}

// refresh fetches the live count. A failed fetch keeps the previous value
// rather than blanking the card.
func (w *SiteOnlineWidget) refresh() {
	var stats siteOnlineStats
	if err := w.api.get(siteOnlinePath, &stats); err != nil {
		debug.Logf("widget %s: fetch: %v", w.id, err)
		return
	}
	w.mu.Lock()
	w.stats = &stats
	w.mu.Unlock()
	if err := w.render(); err != nil {
		debug.Logf("widget %s: %v", w.id, err)
	}
}

func (w *SiteOnlineWidget) frameKey() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stats == nil {
		return "-"
	}
	return fmt.Sprintf("%d", w.stats.Visitors)
}

func (w *SiteOnlineWidget) render() error {
	key := w.frameKey()
	w.mu.Lock()
	same := w.lastFrameKey == key
	w.mu.Unlock()
	if same {
		return nil
	}
	if _, err := w.publish(w.renderImage()); err != nil {
		return fmt.Errorf("write site online icon: %w", err)
	}
	w.mu.Lock()
	w.lastFrameKey = key
	w.mu.Unlock()
	return nil
}

func (w *SiteOnlineWidget) renderImage() *image.RGBA {
	w.mu.Lock()
	stats := w.stats
	w.mu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, icons.IconWidth, icons.IconHeight))
	renderSiteOnlineCard(img, stats)
	return img
}

// ── Rendering ────────────────────────────────────────────────────────────────

const siteOnlineSize = 110

// renderSiteOnlineCard draws the live visitor count centered on black. Zero is
// shown as "0" like any other value.
func renderSiteOnlineCard(img *image.RGBA, s *siteOnlineStats) {
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)

	W := icons.IconWidth
	H := icons.IconHeight
	innerW := W - siteViewsMargin*2
	innerH := H - siteViewsMargin*2

	if s == nil {
		drawText(img, "--",
			FromLeft(siteViewsMargin), FromTop(siteViewsMargin),
			innerW, innerH, AlignMiddleCenter,
			rgb("444444"), siteOnlineSize, true)
		return
	}

	// Exact count, not abbreviated: this is a live reading where the precise
	// number is the point, and it shrinks to fit rather than losing digits.
	drawText(img, fmt.Sprintf("%d", s.Visitors),
		FromLeft(siteViewsMargin), FromTop(siteViewsMargin),
		innerW, innerH, AlignMiddleCenter,
		color.White, siteOnlineSize, true)
}
