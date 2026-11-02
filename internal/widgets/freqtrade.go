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
	"time"

	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
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

var ()

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
	title  string
	apis   []freqtradeAPI
	pollMS time.Duration

	mu      sync.RWMutex
	mode    int
	trades  []freqtradeTrade
	summary *freqtradeSummary
}

func NewFreqtradeWidget(id string, cfg *config.Widget, notify func(string)) (*FreqtradeWidget, error) {
	apis := parseFreqtradeDSNs(cfg.DSNs)
	if len(apis) == 0 {
		return nil, fmt.Errorf("freqtrade widget has no valid dsns")
	}
	for _, api := range apis {
		warnClearCredentials("freqtrade", api.URL)
	}
	base, err := newWidgetBase(id, "freqtrade-widget", "freqtrade.png", notify)
	if err != nil {
		return nil, err
	}
	w := &FreqtradeWidget{
		widgetBase: base,
		title:      strings.TrimSpace(cfg.Title),
		apis:       apis,
		pollMS:     pollDurationOrDefault(cfg.PollMS, defaultFreqtradePollMS),
		mode:       freqtradeModeSummary,
	}
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write freqtrade widget icon: %w", err)
	}
	w.every(w.pollMS, func(time.Time) { w.refresh() })
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
	trades, summary := fetchAllFreqtradeData(w.apis)
	w.mu.Lock()
	w.trades = trades
	w.summary = summary
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
	trades := append([]freqtradeTrade(nil), w.trades...)
	var summary *freqtradeSummary
	if w.summary != nil {
		copySummary := *w.summary
		summary = &copySummary
	}
	w.mu.RUnlock()

	img := image.NewRGBA(image.Rect(0, 0, freqtradeWidgetW, freqtradeWidgetH))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0x00, 0x00, 0x00, 0xff}}, image.Point{}, draw.Src)
	if mode == freqtradeModeLog {
		renderFreqtradeLog(img, trades, title)
	} else {
		renderFreqtradeSummary(img, summary, title)
	}
	return img
}

func renderFreqtradeLog(img *image.RGBA, trades []freqtradeTrade, title string) {
	mt, mr, mb, ml := 25, 25, 25, 15
	if title != "" {
		drawFreqtradeText(img, title, image.Rect(ml, 5, freqtradeWidgetW-mr, 28), color.White, 24)
		mt = 40
	}

	if len(trades) == 0 {
		drawFreqtradeLoading(img)
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
	mt, mr, mb, ml := 25, 25, 25, 15
	if title != "" {
		drawFreqtradeText(img, title, image.Rect(ml, 5, freqtradeWidgetW-mr, 28), color.White, 24)
		mt = 40
	}

	if summary == nil {
		drawFreqtradeLoading(img)
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

func fetchAllFreqtradeData(apis []freqtradeAPI) ([]freqtradeTrade, *freqtradeSummary) {
	tradesCh := make(chan []freqtradeTrade, len(apis))
	summaryCh := make(chan *freqtradeSummary, len(apis))

	var wg sync.WaitGroup
	for _, api := range apis {
		api := api
		wg.Add(1)
		go func() {
			defer wg.Done()
			if trades, err := fetchFreqtradeClosedTrades(api); err == nil {
				tradesCh <- trades
			} else {
				debug.Logf("freqtrade %s: trades: %v", api.Name, err)
			}
			if summary, err := fetchFreqtradeSummary(api); err == nil {
				summaryCh <- summary
			} else {
				debug.Logf("freqtrade %s: summary: %v", api.Name, err)
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

	if !haveOpen && !haveClosed {
		return allTrades, nil
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
	return allTrades, out
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

func fillRoundRect(img *image.RGBA, rect image.Rectangle, radius int, clr color.Color) {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if pointInRoundedRect(x, y, rect, radius) {
				img.Set(x, y, clr)
			}
		}
	}
}

func pointInRoundedRect(x, y int, rect image.Rectangle, radius int) bool {
	if radius <= 0 {
		return true
	}
	left, top, right, bottom := rect.Min.X, rect.Min.Y, rect.Max.X-1, rect.Max.Y-1
	if x >= left+radius && x <= right-radius {
		return true
	}
	if y >= top+radius && y <= bottom-radius {
		return true
	}

	corners := [][2]int{
		{left + radius, top + radius},
		{right - radius, top + radius},
		{left + radius, bottom - radius},
		{right - radius, bottom - radius},
	}
	for _, c := range corners {
		dx := x - c[0]
		dy := y - c[1]
		if dx*dx+dy*dy <= radius*radius {
			return true
		}
	}
	return false
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
