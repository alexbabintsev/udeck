//go:build personal

package widgets

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ig "github.com/alexbabintsev/udeck/internal/gestures"

	"github.com/alexbabintsev/udeck/internal/config"
	"github.com/alexbabintsev/udeck/internal/debug"
)

const (
	freqtradeWidgetW       = 196
	freqtradeWidgetH       = 196
	defaultFreqtradePollMS = 10000
	freqtradeHTTPTimeout   = 8 * time.Second
	freqtradeMaxTrades     = 3
	freqtradeFetchN        = 20
	freqtradeModeLog       = 0
	freqtradeModeSummary   = 1
)

var freqtradeHTTPClient = &http.Client{Timeout: freqtradeHTTPTimeout}

type freqtradeAPI struct {
	User string
	Pass string
	URL  string
	Name string
}

type freqtradeTrade struct {
	ProfitAbs      float64
	Pair           string
	Source         string
	CloseTimestamp int64
}

type freqtradeSummary struct {
	OpenProfit   *float64
	ClosedProfit *float64
}

type FreqtradeWidget struct {
	*widgetBase
	title string
	apis  []freqtradeAPI
	poll  time.Duration

	mu      sync.RWMutex
	mode    int
	state   freqtradeState
	trades  []freqtradeTrade
	summary *freqtradeSummary
}

// freqtradeState tells "still loading" from "no bot answered", which
// would otherwise both show the loading dots forever.
type freqtradeState int

const (
	freqtradeLoading freqtradeState = iota
	freqtradeReady
	freqtradeOffline
)

func NewFreqtradeWidget(id string, cfg *config.Widget, notify func(string)) (*FreqtradeWidget, error) {
	apis := parseFreqtradeDSNs(cfg.DSNs)
	if len(apis) == 0 {
		return nil, fmt.Errorf("freqtrade widget has no valid dsns")
	}
	for _, api := range apis {
		warnClearCredentials("freqtrade", api.URL)
	}
	base, err := newWidgetBase(id, "freqtrade-widget", notify)
	if err != nil {
		return nil, err
	}
	w := &FreqtradeWidget{
		widgetBase: base,
		title:      strings.TrimSpace(cfg.Title),
		apis:       apis,
		poll:       pollDurationOrDefault(cfg.PollMS, defaultFreqtradePollMS),
		mode:       freqtradeModeSummary,
	}
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write freqtrade widget icon: %w", err)
	}
	w.every(w.poll, func(time.Time) { w.refresh() })
	go w.refresh()
	return w, nil
}

// HandleGesture toggles between the summary and the trade log on tap.
func (w *FreqtradeWidget) HandleGesture(kind ig.Kind) {
	if kind != ig.Tap || w.isClosed() {
		return
	}
	w.mu.Lock()
	if w.mode == freqtradeModeSummary {
		w.mode = freqtradeModeLog
	} else {
		w.mode = freqtradeModeSummary
	}
	w.mu.Unlock()
	_, _ = w.render()
}

func (w *FreqtradeWidget) resume() { w.refresh() }

func (w *FreqtradeWidget) refresh() {
	trades, summary, reachable := fetchAllFreqtradeData(w.apis)
	w.mu.Lock()
	w.trades = trades
	w.summary = summary
	w.state = freqtradeReady
	if reachable == 0 {
		// Stale profits would be misleading: say the bots are down.
		w.state = freqtradeOffline
	}
	w.mu.Unlock()
	_, _ = w.render()
}

func (w *FreqtradeWidget) render() (bool, error) {
	changed, err := w.publish(w.renderImage())
	if err != nil {
		return false, fmt.Errorf("write freqtrade widget icon: %w", err)
	}
	return changed, nil
}

func (w *FreqtradeWidget) renderImage() *image.RGBA {
	w.mu.RLock()
	title := w.title
	mode := w.mode
	state := w.state
	trades := append([]freqtradeTrade(nil), w.trades...)
	var summary *freqtradeSummary
	if w.summary != nil {
		copySummary := *w.summary
		summary = &copySummary
	}
	w.mu.RUnlock()

	img := image.NewRGBA(image.Rect(0, 0, freqtradeWidgetW, freqtradeWidgetH))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0x00, 0x00, 0x00, 0xff}}, image.Point{}, draw.Src)
	switch {
	case state == freqtradeOffline:
		drawFreqtradeTitle(img, title)
		drawFreqtradeMessage(img, "OFFLINE", color.RGBA{0xe7, 0x4c, 0x3c, 0xff})
	case state == freqtradeLoading:
		drawFreqtradeTitle(img, title)
		drawFreqtradeLoading(img)
	case mode == freqtradeModeLog:
		renderFreqtradeLog(img, trades, title)
	default:
		renderFreqtradeSummary(img, summary, title)
	}
	return img
}

// drawFreqtradeTitle draws the optional title and returns the top of the
// content area below it.
func drawFreqtradeTitle(img *image.RGBA, title string) int {
	if title == "" {
		return 25
	}
	drawFreqtradeText(img, title, image.Rect(15, 5, freqtradeWidgetW-25, 28), color.White, 24)
	return 40
}

func drawFreqtradeMessage(img *image.RGBA, text string, clr color.Color) {
	drawFreqtradeText(img, text, image.Rect(15, 70, freqtradeWidgetW-15, 126), clr, 34)
}

func renderFreqtradeLog(img *image.RGBA, trades []freqtradeTrade, title string) {
	mr, mb, ml := 25, 25, 15
	mt := drawFreqtradeTitle(img, title)

	if len(trades) == 0 {
		drawFreqtradeMessage(img, "NO TRADES", color.RGBA{0x60, 0x60, 0x60, 0xff})
		return
	}

	gap := 5
	innerW := freqtradeWidgetW - ml - mr
	innerH := freqtradeWidgetH - mt - mb
	count := len(trades)
	if count > 3 {
		count = 3
	}
	bubbleH := (innerH - gap*(count-1)) / count
	for i := 0; i < count; i++ {
		trade := trades[i]
		bubbleY := mt + i*(bubbleH+gap)
		drawFreqtradeBubble(img, image.Rect(ml, bubbleY, ml+innerW, bubbleY+bubbleH), &trade.ProfitAbs)
	}
}

func renderFreqtradeSummary(img *image.RGBA, summary *freqtradeSummary, title string) {
	mr, mb, ml := 25, 25, 15
	mt := drawFreqtradeTitle(img, title)

	if summary == nil {
		drawFreqtradeMessage(img, "NO TRADES", color.RGBA{0x60, 0x60, 0x60, 0xff})
		return
	}

	gap := 5
	innerW := freqtradeWidgetW - ml - mr
	innerH := freqtradeWidgetH - mt - mb
	closedH := ((innerH - gap) * 2) / 3
	openH := innerH - gap - closedH

	drawFreqtradeBubble(img, image.Rect(ml, mt, ml+innerW, mt+closedH), summary.ClosedProfit)
	drawFreqtradeBubble(img, image.Rect(ml, mt+closedH+gap, ml+innerW, mt+closedH+gap+openH), summary.OpenProfit)
}

func drawFreqtradeBubble(img *image.RGBA, rect image.Rectangle, value *float64) {
	fill := color.RGBA{0x2a, 0x2a, 0x2a, 0xff}
	if value != nil {
		if *value >= 0 {
			fill = color.RGBA{0x0e, 0xab, 0x2d, 0xff}
		} else {
			fill = color.RGBA{0xd7, 0x32, 0x32, 0xff}
		}
	}
	fillRoundRect(img, rect, 10, fill)

	text := "-"
	if value != nil {
		text = formatFreqtradeValue(*value)
	}
	drawFreqtradeText(img, text, insetRect(rect, 8), color.White, 36)
}

func drawFreqtradeLoading(img *image.RGBA) {
	centerY := freqtradeWidgetH / 2
	radius := 7
	gap := 18
	startX := freqtradeWidgetW/2 - ((radius*2*3)+(gap*2))/2 + radius
	clr := color.RGBA{0x60, 0x60, 0x60, 0xff}
	for i := 0; i < 3; i++ {
		fillCircle(img, startX+i*(radius*2+gap), centerY, radius, clr)
	}
}

func parseFreqtradeDSNs(raw string) []freqtradeAPI {
	var apis []freqtradeAPI
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// The URL starts at the last "@http": passwords may contain "@".
		mid := strings.LastIndex(line, "@http://")
		if https := strings.LastIndex(line, "@https://"); https > mid {
			mid = https
		}
		colon := strings.Index(line, ":")
		if colon <= 0 || mid <= colon {
			debug.Logf("freqtrade: skipping dsn %d: want user:password@http(s)://host", len(apis)+1)
			continue
		}
		base := strings.TrimRight(line[mid+1:], "/")
		apis = append(apis, freqtradeAPI{
			User: line[:colon],
			Pass: line[colon+1 : mid],
			URL:  base,
			Name: fmt.Sprintf("bot%d", len(apis)+1),
		})
	}
	return apis
}

// fetchAllFreqtradeData merges the bots' latest closed trades and
// profit summaries; reachable counts the bots that answered at all.
func fetchAllFreqtradeData(apis []freqtradeAPI) (trades []freqtradeTrade, summary *freqtradeSummary, reachable int) {
	tradesCh := make(chan []freqtradeTrade, len(apis))
	summaryCh := make(chan *freqtradeSummary, len(apis))
	var answered atomic.Int32

	var wg sync.WaitGroup
	for _, api := range apis {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok := false
			if trades, err := fetchFreqtradeClosedTrades(api); err == nil {
				tradesCh <- trades
				ok = true
			} else {
				debug.Logf("freqtrade %s: trades: %v", api.Name, err)
			}
			if summary, err := fetchFreqtradeSummary(api); err == nil {
				summaryCh <- summary
				ok = true
			} else {
				debug.Logf("freqtrade %s: summary: %v", api.Name, err)
			}
			if ok {
				answered.Add(1)
			}
		}()
	}
	wg.Wait()
	close(tradesCh)
	close(summaryCh)

	var allTrades []freqtradeTrade
	for trades := range tradesCh {
		allTrades = append(allTrades, trades...)
	}
	sort.Slice(allTrades, func(i, j int) bool {
		return allTrades[i].CloseTimestamp > allTrades[j].CloseTimestamp
	})
	if len(allTrades) > freqtradeMaxTrades {
		allTrades = allTrades[:freqtradeMaxTrades]
	}

	var openSum float64
	var closedSum float64
	var haveOpen bool
	var haveClosed bool
	for summary := range summaryCh {
		if summary == nil {
			continue
		}
		if summary.OpenProfit != nil {
			openSum += *summary.OpenProfit
			haveOpen = true
		}
		if summary.ClosedProfit != nil {
			closedSum += *summary.ClosedProfit
			haveClosed = true
		}
	}

	reachable = int(answered.Load())
	if !haveOpen && !haveClosed {
		return allTrades, nil, reachable
	}
	out := &freqtradeSummary{}
	if haveOpen {
		v := round4(openSum)
		out.OpenProfit = &v
	}
	if haveClosed {
		v := round4(closedSum)
		out.ClosedProfit = &v
	}
	return allTrades, out, reachable
}

func fetchFreqtradeClosedTrades(api freqtradeAPI) ([]freqtradeTrade, error) {
	var meta struct {
		TotalTrades int `json:"total_trades"`
	}
	if err := freqtradeFetchJSON(api, "/api/v1/trades?limit=1", &meta); err != nil {
		return nil, err
	}

	offset := meta.TotalTrades - freqtradeFetchN
	if offset < 0 {
		offset = 0
	}
	var data struct {
		Trades []struct {
			IsOpen         bool    `json:"is_open"`
			ProfitAbs      float64 `json:"profit_abs"`
			Pair           string  `json:"pair"`
			CloseTimestamp int64   `json:"close_timestamp"`
		} `json:"trades"`
	}
	path := fmt.Sprintf("/api/v1/trades?limit=%d&offset=%d", freqtradeFetchN, offset)
	if err := freqtradeFetchJSON(api, path, &data); err != nil {
		return nil, err
	}

	var closed []freqtradeTrade
	for _, trade := range data.Trades {
		if trade.IsOpen {
			continue
		}
		closed = append(closed, freqtradeTrade{
			ProfitAbs:      round4(trade.ProfitAbs),
			Pair:           trade.Pair,
			Source:         api.Name,
			CloseTimestamp: trade.CloseTimestamp,
		})
	}
	sort.Slice(closed, func(i, j int) bool {
		return closed[i].CloseTimestamp > closed[j].CloseTimestamp
	})
	if len(closed) > freqtradeMaxTrades {
		closed = closed[:freqtradeMaxTrades]
	}
	return closed, nil
}

func fetchFreqtradeSummary(api freqtradeAPI) (*freqtradeSummary, error) {
	var (
		statusData []struct {
			ProfitAbs float64 `json:"profit_abs"`
		}
		profitData struct {
			ClosedTradeCount *int    `json:"closed_trade_count"`
			TradeCount       int     `json:"trade_count"`
			ProfitClosedCoin float64 `json:"profit_closed_coin"`
		}
	)

	var wg sync.WaitGroup
	var statusErr error
	var profitErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		statusErr = freqtradeFetchJSON(api, "/api/v1/status", &statusData)
	}()
	go func() {
		defer wg.Done()
		profitErr = freqtradeFetchJSON(api, "/api/v1/profit", &profitData)
	}()
	wg.Wait()
	if statusErr != nil {
		return nil, statusErr
	}
	if profitErr != nil {
		return nil, profitErr
	}

	out := &freqtradeSummary{}
	if len(statusData) > 0 {
		sum := 0.0
		for _, trade := range statusData {
			sum += trade.ProfitAbs
		}
		v := round4(sum)
		out.OpenProfit = &v
	}

	closedCount := profitData.TradeCount
	if profitData.ClosedTradeCount != nil {
		closedCount = *profitData.ClosedTradeCount
	}
	if closedCount > 0 {
		v := round4(profitData.ProfitClosedCoin)
		out.ClosedProfit = &v
	}
	return out, nil
}

func freqtradeFetchJSON(api freqtradeAPI, path string, out any) error {
	return getJSON(freqtradeHTTPClient, api.URL+path, map[string]string{
		"Accept":        "application/json",
		"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(api.User+":"+api.Pass)),
	}, out)
}

func formatFreqtradeValue(v float64) string {
	sign := "+"
	if v < 0 {
		sign = "-"
	}
	abs := v
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= 1000:
		return fmt.Sprintf("%s%.0f", sign, abs)
	case abs >= 100:
		return fmt.Sprintf("%s%.1f", sign, abs)
	default:
		return fmt.Sprintf("%s%.2f", sign, abs)
	}
}

func round4(v float64) float64 {
	if v >= 0 {
		return float64(int(v*10000+0.5)) / 10000
	}
	return float64(int(v*10000-0.5)) / 10000
}

func insetRect(r image.Rectangle, pad int) image.Rectangle {
	return image.Rect(r.Min.X+pad, r.Min.Y+pad, r.Max.X-pad, r.Max.Y-pad)
}

func fillCircle(img *image.RGBA, cx, cy, radius int, clr color.Color) {
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx := x - cx
			dy := y - cy
			if dx*dx+dy*dy <= radius*radius {
				img.Set(x, y, clr)
			}
		}
	}
}

func drawFreqtradeText(dst *image.RGBA, text string, target image.Rectangle, clr color.Color, startSize float64) {
	face, display := fitFace(text, target.Dx(), startSize, 14, 2, false)
	if face == nil {
		return
	}
	defer face.Close()
	drawCenteredFace(dst, display, target.Min.X+target.Dx()/2, target.Min.Y+target.Dy()/2+face.Metrics().Ascent.Ceil()/2-2, clr, face)
}
