//go:build personal

package widgets

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
	"github.com/alexbabintsev/udeck/internal/debug"
	ig "github.com/alexbabintsev/udeck/internal/gestures"
	"github.com/alexbabintsev/udeck/internal/icons"
)

const sitePath = "/api/metrics"

// siteStats is the metrics payload: who is on the site now and the last 24
// hours against a typical day.
type siteStats struct {
	Online struct {
		Visitors int `json:"visitors"`
		Views    int `json:"views"`
	} `json:"online"`
	Daily struct {
		Views    int `json:"views"`
		Visitors int `json:"visitors"`
		// Change is the percentage against a typical day of the past week;
		// nil when that week had nothing to compare with.
		Change *float64 `json:"change"`
	} `json:"daily"`
}

// SiteWidget shows a site's visitors online now and its page views over the
// last 24 hours, from one request.
type SiteWidget struct {
	*widgetBase
	api   siteAPI
	title string
	poll  time.Duration

	mu           sync.Mutex
	stats        *siteStats
	lastFrameKey string
}

func NewSiteWidget(id string, cfg *config.Widget, notify func(string)) (*SiteWidget, error) {
	base, err := newWidgetBase(id, "site", notify)
	if err != nil {
		return nil, err
	}
	w := &SiteWidget{
		widgetBase: base,
		api:        newSiteAPI(cfg),
		title:      siteTitle(cfg.Title, cfg.URL),
		poll:       sitePoll(cfg.PollMS),
	}
	// Render the placeholder so the icon exists before the first fetch.
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write site icon: %w", err)
	}
	w.every(w.poll, func(time.Time) { w.refresh() })
	go w.refresh()
	return w, nil
}

func (w *SiteWidget) resume() { w.refresh() }

// siteTitle is the configured title, or else the site's name from its URL:
// "https://www.xmlvalid.com" reads "xmlvalid".
func siteTitle(title, rawURL string) string {
	if strings.TrimSpace(title) != "" {
		return strings.TrimSpace(title)
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	if i := strings.LastIndex(host, "."); i > 0 {
		host = host[:i]
	}
	return host
}

// HandleGesture forces an immediate refresh on tap.
func (w *SiteWidget) HandleGesture(kind ig.Kind) {
	if kind == ig.Tap {
		go w.refresh()
	}
}

// refresh fetches the metrics. A failed fetch keeps the previous values
// rather than blanking the card.
func (w *SiteWidget) refresh() {
	var stats siteStats
	if err := w.api.get(sitePath, &stats); err != nil {
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

func (w *SiteWidget) frameKey() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return siteFrameKey(w.stats)
}

func siteFrameKey(s *siteStats) string {
	if s == nil {
		return "-"
	}
	change := "-"
	if s.Daily.Change != nil {
		change = formatSiteChange(*s.Daily.Change)
	}
	return fmt.Sprintf("%d|%s|%s", s.Online.Visitors, formatSiteCount(s.Daily.Views), change)
}

func (w *SiteWidget) render() error {
	key := w.frameKey()
	w.mu.Lock()
	same := w.lastFrameKey == key
	w.mu.Unlock()
	if same {
		return nil
	}
	if _, err := w.publish(w.renderImage()); err != nil {
		return fmt.Errorf("write site icon: %w", err)
	}
	w.mu.Lock()
	w.lastFrameKey = key
	w.mu.Unlock()
	return nil
}

func (w *SiteWidget) renderImage() *image.RGBA {
	w.mu.Lock()
	stats, title := w.stats, w.title
	w.mu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, icons.IconWidth, icons.IconHeight))
	renderSiteCard(img, title, stats)
	return img
}

// ── Rendering ────────────────────────────────────────────────────────────────

const (
	// siteMargin insets all content from the icon edges.
	siteMargin = 20
	// The title along the top, as the project widget writes its name below
	// its icon; the numbers fill the rest down to siteBottom.
	siteTitleTop = 14
	siteTitleH   = 34
	siteBodyTop  = 54
	siteBottom   = 16
	// Starting sizes; each shrinks to fit its block.
	siteTitleSize  = 28
	siteOnlineSize = 60
	siteViewsSize  = 40
	siteBadgeSize  = 26
	// The live dot before the online count.
	siteDotRadius = 8
	siteDotGap    = 10
	// The change badge: height, corner radius, padding and width per digit.
	siteBadgeH      = 36
	siteBadgeRadius = 6
	siteBadgePad    = 22
	siteBadgeDigitW = 15
)

var (
	siteTitleColor = color.RGBA{0xf0, 0xf0, 0xf0, 0xff}
	siteLive       = rgb("2ecc40")
	siteViews      = rgb("bbbbbb")
	siteEmpty      = rgb("444444")
	siteRise       = rgb("27ae60")
	siteFall       = rgb("e74c3c")
)

// renderSiteCard draws the title and two rows on plain black:
//
//	title  — the site's name, upper-cased
//	online — a green live dot and the visitors on the site now, large
//	daily  — the last 24 hours' page views, with the change against a typical
//	         day as a badge on the right: green for a rise, red for a fall
func renderSiteCard(img *image.RGBA, title string, s *siteStats) {
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)

	drawText(img, strings.ToUpper(title),
		FromLeft(8), FromTop(siteTitleTop),
		icons.IconWidth-16, siteTitleH, AlignTopCenter,
		siteTitleColor, siteTitleSize, true)

	innerW := icons.IconWidth - siteMargin*2
	innerH := icons.IconHeight - siteBodyTop - siteBottom
	// The online count takes the larger share of the vertical space.
	topH := innerH * 11 / 20
	bottomH := innerH - topH
	bottomY := siteBodyTop + topH

	if s == nil {
		drawText(img, "--",
			FromLeft(siteMargin), FromTop(siteBodyTop),
			innerW, topH, AlignMiddleCenter,
			siteEmpty, siteOnlineSize, true)
		drawText(img, "--",
			FromLeft(siteMargin), FromTop(bottomY),
			innerW, bottomH, AlignMiddleCenter,
			siteEmpty, siteViewsSize, true)
		return
	}

	// Exact count, not abbreviated: this is a live reading where the precise
	// number is the point, and it shrinks to fit rather than losing digits.
	fillCircle(img, siteMargin+siteDotRadius, siteBodyTop+topH/2, siteDotRadius, siteLive)
	countX := siteMargin + siteDotRadius*2 + siteDotGap
	drawText(img, fmt.Sprintf("%d", s.Online.Visitors),
		FromLeft(countX), FromTop(siteBodyTop),
		siteMargin+innerW-countX, topH, AlignMiddleLeft,
		color.White, siteOnlineSize, true)

	viewsW := innerW * 3 / 5
	drawText(img, formatSiteCount(s.Daily.Views),
		FromLeft(siteMargin), FromTop(bottomY),
		viewsW, bottomH, AlignMiddleLeft,
		siteViews, siteViewsSize, true)
	if s.Daily.Change != nil {
		drawSiteBadge(img, *s.Daily.Change, siteMargin+innerW, bottomY+bottomH/2)
	}
}

// drawSiteBadge draws the change as a rounded badge ending at right and
// centered on cy. The color carries the sign, so the badge holds the number
// alone: "12", not "+12%".
func drawSiteBadge(img *image.RGBA, pct float64, right, cy int) {
	text := formatSiteChange(pct)
	w := siteBadgePad + siteBadgeDigitW*len(text)
	x, y := right-w, cy-siteBadgeH/2
	fill := siteRise
	if pct < 0 {
		fill = siteFall
	}
	fillRoundedRect(img, image.Rect(x, y, x+w, y+siteBadgeH), siteBadgeRadius, fill)
	drawText(img, text,
		FromLeft(x), FromTop(y),
		w, siteBadgeH, AlignMiddleCenter,
		color.Black, siteBadgeSize, true)
}

func fillRoundedRect(img *image.RGBA, rect image.Rectangle, r int, clr color.Color) {
	w, h := rect.Dx(), rect.Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Distance into the corner square, zero along the straight edges.
			dx, dy := 0, 0
			if x < r {
				dx = r - x
			} else if x >= w-r {
				dx = x - (w - r - 1)
			}
			if y < r {
				dy = r - y
			} else if y >= h-r {
				dy = y - (h - r - 1)
			}
			if dx*dx+dy*dy <= r*r {
				img.Set(rect.Min.X+x, rect.Min.Y+y, clr)
			}
		}
	}
}

// formatSiteCount abbreviates large counts so they fit the row.
func formatSiteCount(v int) string {
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

// formatSiteChange is the whole-number size of a percentage change, without
// its sign, which the badge's color shows.
func formatSiteChange(pct float64) string {
	return fmt.Sprintf("%.0f", math.Abs(pct))
}
