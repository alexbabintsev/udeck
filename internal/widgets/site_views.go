package widgets

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
)

// siteViewsStats is one metrics window as returned by the API.
type siteViewsStats struct {
	Window        string  `json:"window"`
	Views         int     `json:"views"`
	Visitors      int     `json:"visitors"`
	PreviousViews int     `json:"previous_views"`
	Change        float64 `json:"change"`
}

// siteViewsData holds both windows fetched together.
type siteViewsData struct {
	Daily  *siteViewsStats
	Hourly *siteViewsStats
}

type SiteViewsWidget struct {
	*widgetBase
	api    siteAPI
	title  string
	pollMS time.Duration

	mu           sync.Mutex
	data         siteViewsData
	lastFrameKey string
}

func NewSiteViewsWidget(id string, cfg *config.Widget, notify func(string)) (*SiteViewsWidget, error) {
	base, err := newWidgetBase(id, "site-views", "site_views.png", notify)
	if err != nil {
		return nil, err
	}
	w := &SiteViewsWidget{
		widgetBase: base,
		api:        newSiteAPI(cfg),
		title:      strings.TrimSpace(cfg.Title),
		pollMS:     sitePoll(cfg.PollMS),
	}
	// Render the placeholder so the icon exists before the first fetch.
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write site views icon: %w", err)
	}
	w.every(w.pollMS, func(time.Time) { w.refresh() })
	go w.refresh()
	return w, nil
}

// HandleGesture forces an immediate refresh on tap.
func (w *SiteViewsWidget) HandleGesture(kind ig.Kind) {
	if kind == ig.Tap {
		go w.refresh()
	}
}

// refresh fetches both windows concurrently. A failed window keeps its
// previous value rather than blanking the card.
func (w *SiteViewsWidget) refresh() {
	var (
		wg            sync.WaitGroup
		daily, hourly *siteViewsStats
	)
	fetch := func(path string, dst **siteViewsStats) {
		defer wg.Done()
		var s siteViewsStats
		if err := w.api.get(path, &s); err != nil {
			debug.Logf("widget %s: fetch: %v", w.id, err)
			return
		}
		*dst = &s
	}
	wg.Add(2)
	go fetch("/api/metrics/views/daily", &daily)
	go fetch("/api/metrics/views/hourly", &hourly)
	wg.Wait()

	w.mu.Lock()
	if daily != nil {
		w.data.Daily = daily
	}
	if hourly != nil {
		w.data.Hourly = hourly
	}
	w.mu.Unlock()
	if err := w.render(); err != nil {
		debug.Logf("widget %s: %v", w.id, err)
	}
}

func (w *SiteViewsWidget) render() error {
	w.mu.Lock()
	key := siteViewsFrameKey(w.title, w.data)
	same := w.lastFrameKey == key
	w.mu.Unlock()
	if same {
		return nil
	}
	if _, err := w.publish(w.renderImage()); err != nil {
		return fmt.Errorf("write site views icon: %w", err)
	}
	w.mu.Lock()
	w.lastFrameKey = key
	w.mu.Unlock()
	return nil
}

func (w *SiteViewsWidget) renderImage() *image.RGBA {
	w.mu.Lock()
	data, title := w.data, w.title
	w.mu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, icons.IconWidth, icons.IconHeight))
	renderSiteViewsCard(img, title, data)
	return img
}

func siteViewsFrameKey(title string, d siteViewsData) string {
	part := func(s *siteViewsStats) string {
		if s == nil {
			return "-"
		}
		return fmt.Sprintf("%d/%d/%.1f", s.Views, s.Visitors, s.Change)
	}
	return fmt.Sprintf("%s|%s|%s", title, part(d.Daily), part(d.Hourly))
}

// ── Rendering ────────────────────────────────────────────────────────────────

const (
	// siteViewsMargin insets all content from the icon edges.
	siteViewsMargin = 20
	// Height of the sub-row holding the percentage and visitor count.
	siteViewsSubH = 28
	// Starting sizes; each shrinks to fit its block.
	siteViewsDailySize  = 82
	siteViewsHourlySize = 44
	siteViewsSubSize    = 26
	// Inset pulling the sub-row cells toward the center, so the small values
	// tuck under the big number instead of hugging the outer edges.
	siteViewsSubInset = 14
)

// renderSiteViewsCard draws two blocks on plain black, each with its own shape:
//
//	daily  — large count centered, percentage and visitors on a row beneath it
//	hourly — large count on the left, percentage stacked over visitors to its right
func renderSiteViewsCard(img *image.RGBA, _ string, d siteViewsData) {
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)

	W := icons.IconWidth
	H := icons.IconHeight

	innerW := W - siteViewsMargin*2
	innerH := H - siteViewsMargin*2

	// Daily takes the larger share of the vertical space.
	dailyH := innerH * 11 / 20
	hourlyH := innerH - dailyH

	drawSiteViewsDaily(img, siteViewsMargin, siteViewsMargin, innerW, dailyH, d.Daily)
	drawSiteViewsHourly(img, siteViewsMargin, siteViewsMargin+dailyH, innerW, hourlyH, d.Hourly)
}

// drawSiteViewsDaily renders the count centered with a sub-row beneath it
// holding the percentage (left) and visitor count (right).
func drawSiteViewsDaily(img *image.RGBA, x, y, w, h int, s *siteViewsStats) {
	countH := h - siteViewsSubH
	if countH < 1 {
		countH = h
	}

	if s == nil {
		drawText(img, "--",
			FromLeft(x), FromTop(y),
			w, countH, AlignMiddleCenter,
			rgb("444444"), siteViewsDailySize, true)
		return
	}

	drawText(img, formatSiteViewsCount(s.Views),
		FromLeft(x), FromTop(y),
		w, countH, AlignMiddleCenter,
		color.White, siteViewsDailySize, true)

	// Sub-row cells are inset so the small values tuck under the big number
	// rather than hugging the outer edges.
	subY := y + countH
	subX := x + siteViewsSubInset
	subW := w - siteViewsSubInset*2
	half := subW / 2

	drawText(img, formatSiteViewsChange(s.Change),
		FromLeft(subX), FromTop(subY),
		half, siteViewsSubH, AlignMiddleLeft,
		siteViewsChangeColor(s.Change), siteViewsSubSize, true)
	drawText(img, formatSiteViewsCount(s.Visitors),
		FromLeft(subX+half), FromTop(subY),
		subW-half, siteViewsSubH, AlignMiddleRight,
		color.White, siteViewsSubSize, true)
}

// drawSiteViewsHourly renders the count on the left with the percentage stacked
// directly above the visitor count in a column to its right.
func drawSiteViewsHourly(img *image.RGBA, x, y, w, h int, s *siteViewsStats) {
	// Right column holds the two stacked small values; the count keeps the
	// rest, less a gap so a wide count cannot crowd the column.
	const sideGap = 8
	sideW := w * 2 / 5
	countW := w - sideW - sideGap

	if s == nil {
		drawText(img, "--",
			FromLeft(x), FromTop(y),
			countW, h, AlignMiddleLeft,
			rgb("444444"), siteViewsHourlySize, true)
		return
	}

	drawText(img, formatSiteViewsCount(s.Views),
		FromLeft(x), FromTop(y),
		countW, h, AlignMiddleCenter,
		color.White, siteViewsHourlySize, true)

	// Percentage on top, visitors below, both left-aligned in the side column.
	sideX := x + countW + sideGap
	halfH := h / 2

	drawText(img, formatSiteViewsChange(s.Change),
		FromLeft(sideX), FromTop(y),
		sideW, halfH, AlignMiddleLeft,
		siteViewsChangeColor(s.Change), siteViewsSubSize, true)
	drawText(img, formatSiteViewsCount(s.Visitors),
		FromLeft(sideX), FromTop(y+halfH),
		sideW, h-halfH, AlignMiddleLeft,
		color.White, siteViewsSubSize, true)
}

// siteViewsChangeColor is green for a rise, red for a fall.
func siteViewsChangeColor(pct float64) color.Color {
	if pct < 0 {
		return rgb("ff4136")
	}
	return rgb("2ecc40")
}

// formatSiteViewsCount abbreviates large counts so they fit the row.
func formatSiteViewsCount(v int) string {
	switch {
	case v >= 1000000:
		return fmt.Sprintf("%.1fM", float64(v)/1000000)
	case v >= 10000:
		return fmt.Sprintf("%.0fk", float64(v)/1000)
	case v >= 1000:
		return fmt.Sprintf("%.1fk", float64(v)/1000)
	default:
		return fmt.Sprintf("%d", v)
	}
}

// formatSiteViewsChange renders a signed whole-number percentage, e.g. "+10%".
func formatSiteViewsChange(pct float64) string {
	sign := "+"
	if pct < 0 {
		sign = "-"
	}
	return fmt.Sprintf("%s%.0f%%", sign, math.Abs(pct))
}
