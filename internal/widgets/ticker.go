package widgets

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
	"github.com/alexbabintsev/udeck/internal/debug"
	ig "github.com/alexbabintsev/udeck/internal/gestures"
)

const (
	tickerDefaultPoll = time.Minute
	tickerHTTPTimeout = 8 * time.Second
	// Yahoo answers 429 to requests without a User-Agent.
	tickerUserAgent = "udeck"
)

var (
	tickerHTTPClient = &http.Client{Timeout: tickerHTTPTimeout}
	tickerYahooURL   = "https://query1.finance.yahoo.com/v8/finance/chart/"
	tickerBinanceURL = "https://api.binance.com/api/v3/"
)

// tickerQuote is one reading: the price, what the change is measured
// from (the previous close, or the price 24 hours ago) and the recent
// prices for the chart.
type tickerQuote struct {
	Price  float64
	Base   float64
	Series []float64
}

// change is the move from Base as a fraction.
func (q tickerQuote) change() float64 {
	if q.Base == 0 {
		return 0
	}
	return (q.Price - q.Base) / q.Base
}

// tickerSources are the quote providers by config name; each widget
// copies its own when created, so tests can substitute them.
var tickerSources = map[string]func(symbol string) (tickerQuote, error){
	"yahoo":   fetchYahooQuote,
	"binance": fetchBinanceQuote,
}

// TickerWidget shows the price of a stock, index, currency pair or coin.
type TickerWidget struct {
	*widgetBase
	symbol string
	label  string
	poll   time.Duration
	fetch  func(string) (tickerQuote, error)
	now    func() time.Time

	fetching atomic.Bool // one request at a time

	mu      sync.Mutex
	quote   *tickerQuote
	stale   bool // the last request failed: quote is an old one
	fetched time.Time
}

func NewTickerWidget(id string, cfg *config.Widget, notify func(string)) (*TickerWidget, error) {
	source := strings.TrimSpace(cfg.Source)
	if source == "" {
		source = "yahoo"
	}
	fetch, ok := tickerSources[source]
	if !ok {
		return nil, fmt.Errorf("unknown ticker source %q", source)
	}
	symbol := strings.ToUpper(strings.TrimSpace(cfg.Symbol))
	if symbol == "" {
		return nil, fmt.Errorf("ticker has no symbol")
	}
	base, err := newWidgetBase(id, "ticker", notify)
	if err != nil {
		return nil, err
	}
	label := strings.TrimSpace(cfg.Title)
	if label == "" {
		label = symbol
	}
	w := &TickerWidget{
		widgetBase: base,
		symbol:     symbol,
		label:      label,
		poll:       pollOr(cfg.PollMS, tickerDefaultPoll),
		fetch:      fetch,
		now:        time.Now,
	}
	if _, err := w.writeIcon(w.draw()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write ticker icon: %w", err)
	}
	w.every(w.poll, func(time.Time) { w.refresh() })
	go w.refresh()
	return w, nil
}

// HandleGesture fetches the price now.
func (w *TickerWidget) HandleGesture(kind ig.Kind) {
	if kind == ig.Tap && !w.isClosed() {
		go w.refresh()
	}
}

// resume refreshes a price that went out of date while the page was
// hidden; flipping through pages does not cost a request each time.
func (w *TickerWidget) resume() {
	w.mu.Lock()
	fresh := !w.fetched.IsZero() && w.now().Sub(w.fetched) < w.poll
	w.mu.Unlock()
	if !fresh {
		w.refresh()
	}
}

func (w *TickerWidget) refresh() {
	if !w.fetching.CompareAndSwap(false, true) {
		return
	}
	defer w.fetching.Store(false)
	q, err := w.fetch(w.symbol)
	w.mu.Lock()
	w.fetched = w.now()
	w.stale = err != nil
	if err == nil {
		w.quote = &q
	}
	w.mu.Unlock()
	if err != nil {
		debug.Logf("widget %s: %v", w.id, err)
	}
	if _, err := w.publish(w.draw()); err != nil {
		debug.Logf("widget %s: %v", w.id, err)
	}
}

func (w *TickerWidget) draw() *image.RGBA {
	w.mu.Lock()
	defer w.mu.Unlock()
	return renderTicker(w.label, w.quote, w.stale)
}

// renderTicker draws the symbol, the price, the change and the chart.
// The change is green or red; a price that could not be updated is all
// grey, so it does not pass for a current one.
func renderTicker(label string, q *tickerQuote, stale bool) *image.RGBA {
	img := newCard()
	if q == nil {
		text := "…"
		if stale {
			text = "no data"
		}
		drawIcon(img, "lucide/chart-line", CenterX(0), FromTop(styleIconTop), styleIconSize, styleIconSize, styleDim)
		drawCardLabel(img, label, styleDim)
		drawText(img, text, FromLeft(8), FromBottom(8), img.Bounds().Dx()-16, 22, AlignMiddleCenter, styleDim, 18, true)
		return img
	}
	change := math.Round(q.change()*10000) / 100 // percent, as displayed
	moveClr := color.RGBA(styleDim)
	switch {
	case stale:
	case change > 0:
		moveClr = styleLive
	case change < 0:
		moveClr = styleAlert
	}
	labelClr, priceClr := color.RGBA(styleAccent), color.RGBA(styleText)
	if stale {
		labelClr, priceClr = styleDim, styleDim
	}
	w := img.Bounds().Dx()
	drawText(img, strings.ToUpper(label), FromLeft(8), FromTop(12), w-16, 30, AlignMiddleCenter, labelClr, 24, true)
	drawText(img, formatPrice(q.Price), FromLeft(8), FromTop(42), w-16, 54, AlignMiddleCenter, priceClr, 46, true)
	drawText(img, formatChange(change), FromLeft(8), FromTop(96), w-16, 32, AlignMiddleCenter, moveClr, 28, true)
	drawSparkline(img, image.Rect(14, 136, w-14, 184), q.Series, q.Base, moveClr)
	return img
}

// drawSparkline plots series in rect with a dotted line at base; the
// scale includes base, so a chart above the line is a rise.
func drawSparkline(img *image.RGBA, rect image.Rectangle, series []float64, base float64, clr color.Color) {
	if len(series) < 2 {
		return
	}
	lo, hi := base, base
	if base == 0 {
		lo, hi = series[0], series[0]
	}
	for _, v := range series {
		lo, hi = min(lo, v), max(hi, v)
	}
	span := hi - lo
	yOf := func(v float64) float64 {
		if span == 0 {
			return float64(rect.Min.Y+rect.Max.Y) / 2
		}
		return float64(rect.Max.Y-1) - (v-lo)/span*float64(rect.Dy()-1)
	}
	if base != 0 {
		y := int(math.Round(yOf(base)))
		for x := rect.Min.X; x < rect.Max.X; x += 6 {
			img.Set(x, y, styleDim)
			img.Set(x+1, y, styleDim)
		}
	}
	// One column at a time, joined to the previous one so that steep
	// moves stay a line.
	at := func(x int) float64 {
		pos := float64(x-rect.Min.X) / float64(rect.Dx()-1) * float64(len(series)-1)
		i := min(int(pos), len(series)-2)
		return yOf(series[i] + (series[i+1]-series[i])*(pos-float64(i)))
	}
	prev := at(rect.Min.X)
	for x := rect.Min.X; x < rect.Max.X; x++ {
		y := at(x)
		top, bottom := int(math.Round(min(prev, y))), int(math.Round(max(prev, y)))
		for py := top - 1; py <= bottom+1; py++ {
			img.Set(x, py, clr)
		}
		prev = y
	}
}

// formatPrice keeps about the digits that matter at each size: 78,300,
// 4,321.50, 331.90, 1.0832, 0.1234, 0.00001234.
func formatPrice(p float64) string {
	v := math.Abs(p)
	decimals := 2
	switch {
	case v >= 10000:
		decimals = 0
	case v >= 10:
	case v >= 1:
		decimals = 4
	case v > 0:
		decimals = 3 - int(math.Floor(math.Log10(v)))
	}
	text := strconv.FormatFloat(v, 'f', decimals, 64)
	whole, frac, _ := strings.Cut(text, ".")
	var b strings.Builder
	if p < 0 {
		b.WriteByte('-')
	}
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if frac != "" {
		b.WriteString("." + frac)
	}
	return b.String()
}

// formatChange writes a percentage with its sign: +1.68%, -0.40%, 0.00%.
func formatChange(percent float64) string {
	if percent == 0 {
		return "0.00%"
	}
	return fmt.Sprintf("%+.2f%%", percent)
}

func tickerHeaders() map[string]string {
	return map[string]string{"User-Agent": tickerUserAgent}
}

// fetchYahooQuote reads today's chart: stocks (AAPL), indices (^GSPC),
// currencies (EURUSD=X) and coins (BTC-USD).
func fetchYahooQuote(symbol string) (tickerQuote, error) {
	var out struct {
		Chart struct {
			Result []struct {
				Meta struct {
					RegularMarketPrice float64 `json:"regularMarketPrice"`
					ChartPreviousClose float64 `json:"chartPreviousClose"`
				} `json:"meta"`
				Indicators struct {
					Quote []struct {
						Close []*float64 `json:"close"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
		} `json:"chart"`
	}
	u := tickerYahooURL + url.PathEscape(symbol) + "?range=1d&interval=5m"
	if err := getJSON(tickerHTTPClient, u, tickerHeaders(), &out); err != nil {
		return tickerQuote{}, err
	}
	if len(out.Chart.Result) == 0 || out.Chart.Result[0].Meta.RegularMarketPrice == 0 {
		return tickerQuote{}, fmt.Errorf("yahoo: no price for %s", symbol)
	}
	r := out.Chart.Result[0]
	q := tickerQuote{Price: r.Meta.RegularMarketPrice, Base: r.Meta.ChartPreviousClose}
	if len(r.Indicators.Quote) > 0 {
		for _, c := range r.Indicators.Quote[0].Close {
			if c != nil { // no trades in that interval
				q.Series = append(q.Series, *c)
			}
		}
	}
	return q, nil
}

// fetchBinanceQuote reads a spot pair (BTCUSDT): the price and its
// change over the last 24 hours, as Binance shows them. The chart is
// optional: without it the key still shows the price.
func fetchBinanceQuote(symbol string) (tickerQuote, error) {
	var stats struct {
		LastPrice string `json:"lastPrice"`
		OpenPrice string `json:"openPrice"`
	}
	sym := url.QueryEscape(symbol)
	if err := getJSON(tickerHTTPClient, tickerBinanceURL+"ticker/24hr?symbol="+sym, tickerHeaders(), &stats); err != nil {
		return tickerQuote{}, err
	}
	price, err := strconv.ParseFloat(stats.LastPrice, 64)
	if err != nil || price == 0 {
		return tickerQuote{}, fmt.Errorf("binance: no price for %s", symbol)
	}
	open, _ := strconv.ParseFloat(stats.OpenPrice, 64)
	q := tickerQuote{Price: price, Base: open}

	var candles [][]any
	if err := getJSON(tickerHTTPClient, tickerBinanceURL+"klines?interval=15m&limit=96&symbol="+sym, tickerHeaders(), &candles); err != nil {
		debug.Logf("ticker %s chart: %v", symbol, err)
		return q, nil
	}
	const closeField = 4
	for _, c := range candles {
		if len(c) <= closeField {
			continue
		}
		if s, ok := c[closeField].(string); ok {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				q.Series = append(q.Series, v)
			}
		}
	}
	return q, nil
}
