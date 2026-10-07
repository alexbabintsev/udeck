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

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
)

const (
	weatherDayDefaultPoll = 10 * time.Minute
	weatherDayMinPoll     = 60 * time.Second
	weatherDayMaxForecast = 14
)

// ── Shared per-city forecast cache ──────────────────────────────────────────

type weatherDayData struct {
	Date        time.Time
	TempMaxC    float64
	TempMinC    float64
	FeelsLikeC  float64
	HumidityPct int
	WindSpeedMS float64
	Code        int
}

type weatherForecast = [weatherDayMaxForecast]*weatherDayData

type weatherCityCache struct {
	mu        sync.Mutex
	coords    *dashboardWeatherCoords
	days      weatherForecast
	lastFetch time.Time
	fetching  bool
	nextSub   int
	subs      map[int]func(weatherForecast)
}

var (
	weatherCacheMu sync.Mutex
	weatherCaches  = map[string]*weatherCityCache{}
)

func weatherCacheForCity(city string) *weatherCityCache {
	weatherCacheMu.Lock()
	defer weatherCacheMu.Unlock()
	if c, ok := weatherCaches[city]; ok {
		return c
	}
	c := &weatherCityCache{subs: map[int]func(weatherForecast){}}
	weatherCaches[city] = c
	return c
}

// subscribe registers fn for future data and returns a token for
// unsubscribe plus the data cached so far (entries may be nil).
func (c *weatherCityCache) subscribe(fn func(weatherForecast)) (int, weatherForecast) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextSub++
	c.subs[c.nextSub] = fn
	return c.nextSub, c.days
}

func (c *weatherCityCache) unsubscribe(token int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.subs, token)
}

// refresh fetches if stale. Only one fetch runs at a time; others return
// immediately.
func (c *weatherCityCache) refresh(city string, poll time.Duration) {
	c.mu.Lock()
	if c.fetching || (!c.lastFetch.IsZero() && time.Since(c.lastFetch) < poll) {
		c.mu.Unlock()
		return
	}
	c.fetching = true
	coords := c.coords
	c.mu.Unlock()

	if coords == nil {
		resolved, err := fetchDashboardCoords(city)
		if err != nil {
			debug.Logf("weather %q: geocode: %v", city, err)
			c.mu.Lock()
			c.fetching = false
			c.lastFetch = time.Now()
			c.mu.Unlock()
			return
		}
		c.mu.Lock()
		c.coords = resolved
		coords = resolved
		c.mu.Unlock()
	}

	days, err := fetchWeatherDayAll(*coords)
	if err != nil {
		debug.Logf("weather %q: forecast: %v", city, err)
	}

	c.mu.Lock()
	c.fetching = false
	c.lastFetch = time.Now()
	if err == nil {
		c.days = days
	}
	subs := make([]func(weatherForecast), 0, len(c.subs))
	for _, fn := range c.subs {
		subs = append(subs, fn)
	}
	current := c.days
	c.mu.Unlock()

	for _, fn := range subs {
		fn(current)
	}
}

// ── Widget ───────────────────────────────────────────────────────────────────

type WeatherDayWidget struct {
	*widgetBase
	city     string
	dayIndex int
	pollMS   time.Duration
	cache    *weatherCityCache
	subToken int

	mu           sync.Mutex
	forecast     *weatherDayData
	lastFrameKey string
}

func NewWeatherDayWidget(id string, cfg *config.Widget, notify func(string)) (*WeatherDayWidget, error) {
	base, err := newWidgetBase(id, "weather-day", "weather_day.png", notify)
	if err != nil {
		return nil, err
	}
	dayIndex := min(max(cfg.DayIndex, 0), weatherDayMaxForecast-1)
	city := strings.TrimSpace(cfg.City)
	w := &WeatherDayWidget{
		widgetBase: base,
		city:       city,
		dayIndex:   dayIndex,
		pollMS:     weatherDayPoll(cfg.PollMS),
		cache:      weatherCacheForCity(city),
	}

	// Subscribe first so no fetch result is missed; cached data, if any,
	// is drawn right away instead of the loading card.
	token, existing := w.cache.subscribe(w.onData)
	w.subToken = token
	w.forecast = existing[dayIndex]
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write weather day icon: %w", err)
	}
	w.lastFrameKey = weatherDayFrameKey(w.city, w.dayIndex, w.forecast)

	// The cache is shared per city: only one HTTP call for all widgets.
	go w.cache.refresh(w.city, w.pollMS)
	w.every(time.Minute, func(time.Time) { w.cache.refresh(w.city, w.pollMS) })
	return w, nil
}

func (w *WeatherDayWidget) onData(days weatherForecast) {
	if w.isClosed() {
		return
	}
	w.mu.Lock()
	w.forecast = days[w.dayIndex]
	w.mu.Unlock()
	if err := w.render(); err != nil {
		debug.Logf("widget %s: %v", w.id, err)
	}
}

func (w *WeatherDayWidget) HandleGesture(_ ig.Kind) {}

func (w *WeatherDayWidget) resume() { w.cache.refresh(w.city, w.pollMS) }

func (w *WeatherDayWidget) Close() error {
	w.cache.unsubscribe(w.subToken)
	return w.widgetBase.Close()
}

func (w *WeatherDayWidget) render() error {
	w.mu.Lock()
	key := weatherDayFrameKey(w.city, w.dayIndex, w.forecast)
	same := w.lastFrameKey == key
	w.mu.Unlock()
	if same {
		return nil
	}
	if _, err := w.publish(w.renderImage()); err != nil {
		return fmt.Errorf("write weather day icon: %w", err)
	}
	w.mu.Lock()
	w.lastFrameKey = key
	w.mu.Unlock()
	return nil
}

func (w *WeatherDayWidget) renderImage() *image.RGBA {
	w.mu.Lock()
	forecast := w.forecast
	w.mu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, icons.IconWidth, icons.IconHeight))
	renderWeatherDayCard(img, w.dayIndex, forecast, w.city)
	return img
}

func weatherDayFrameKey(city string, dayIndex int, fc *weatherDayData) string {
	if fc == nil {
		return fmt.Sprintf("loading:%s:%d", city, dayIndex)
	}
	return fmt.Sprintf("%s:%d:%s:%.1f:%.1f:%d:%.1f:%d",
		city, dayIndex, fc.Date.Format("2006-01-02"),
		fc.TempMaxC, fc.TempMinC,
		fc.HumidityPct, fc.WindSpeedMS, fc.Code)
}

// ── Rendering ────────────────────────────────────────────────────────────────

func renderWeatherDayCard(img *image.RGBA, dayIndex int, fc *weatherDayData, city string) {
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)

	const (
		headerH      = 45
		row2Y        = headerH + 4
		row2H        = 85
		divY         = row2Y + row2H + 2
		row3Y        = divY + 6
		row3H        = 30
		statIconOffX = 20
	)

	W := icons.IconWidth

	if fc == nil {
		label := city
		if label == "" {
			label = "Weather"
		}
		drawText(img, label,
			CenterX(0), FromTop(4),
			W-8, 22, AlignMiddleCenter,
			color.White, 22, true)
		days := "Today"
		if dayIndex > 0 {
			days = fmt.Sprintf("+%d days", dayIndex)
		}
		drawText(img, days,
			CenterX(0), FromTop(28),
			W-8, 22, AlignMiddleCenter,
			rgb("888888"), 20, false)
		drawText(img, "--°",
			CenterX(0), CenterY(20),
			W-8, 60, AlignMiddleCenter,
			rgb("444444"), 44, true)
		return
	}

	// ── Row 1: date header ──────────────────────────────────────────
	isWeekend := fc.Date.Weekday() == time.Saturday || fc.Date.Weekday() == time.Sunday
	headerBg := rgb("1a4fa8")
	if isWeekend {
		headerBg = rgb("a82020")
	}
	draw.Draw(img, rectWH(0, 0, W, headerH), &image.Uniform{headerBg}, image.Point{}, draw.Src)
	drawText(img, strings.ToUpper(fc.Date.Format("2 Jan")),
		FromLeft(10), FromTop(0),
		W/2, headerH, AlignMiddleLeft,
		color.White, 30, true)
	drawText(img, strings.ToUpper(fc.Date.Format("Mon")),
		FromRight(20), FromTop(0),
		W/2, headerH, AlignMiddleRight,
		color.White, 30, false)

	// ── Row 2: temp + icon ──────────────────────────────────────────
	drawText(img, fmt.Sprintf("%.0f°", math.Round(fc.TempMaxC)),
		FromLeft(10), FromTop(row2Y),
		110, row2H, AlignMiddleLeft,
		color.White, 62, true)

	drawWeatherIconAt(img, fc.Code,
		FromRight(12), FromTop(row2Y),
		56, row2H,
		weatherDayIconColor(fc.Code))

	// ── Divider ─────────────────────────────────────────────────────
	draw.Draw(img, rectWH(10, divY, W-20, 1), &image.Uniform{rgb("222222")}, image.Point{}, draw.Src)

	// ── Row 3: humidity (left) + wind (right) ───────────────────────
	half := W / 2
	const iconSz = 22

	// humidity icon + value
	drawIcon(img, "lucide/droplets",
		FromLeft(statIconOffX-iconSz/2), FromTop(row3Y+(row3H-iconSz)/2),
		iconSz, iconSz, rgb("33bbff"))
	drawText(img, fmt.Sprintf("%d%%", fc.HumidityPct),
		FromLeft(statIconOffX+iconSz/2+6), FromTop(row3Y),
		half-statIconOffX-iconSz/2-8, row3H, AlignMiddleLeft,
		rgb("eeeeee"), 28, true)

	// wind icon + value
	drawIcon(img, "lucide/wind",
		FromRight(20), FromTop(row3Y+(row3H-iconSz)/2),
		iconSz, iconSz, rgb("33bbff"))
	drawText(img, fmt.Sprintf("%.0f", fc.WindSpeedMS),
		FromRight(20+iconSz+6), FromTop(row3Y),
		half-statIconOffX-iconSz/2-8, row3H, AlignMiddleRight,
		rgb("eeeeee"), 28, true)
}

func weatherDayIconColor(code int) color.RGBA {
	switch {
	case code == 0:
		return rgb("ffcc00") // clear — yellow
	case code <= 2:
		return rgb("ff8800") // partly cloudy / cloud-sun — orange
	case code == 3:
		return rgb("888888") // overcast — gray
	case code >= 45 && code <= 48:
		return rgb("99aaaa")
	case code >= 51 && code <= 67, code >= 80 && code <= 82:
		return rgb("4488ff")
	case code >= 71 && code <= 77:
		return rgb("aaddff")
	case code >= 95:
		return rgb("aa44ff")
	default:
		return rgb("aaaaaa")
	}
}

// ── API fetch ────────────────────────────────────────────────────────────────

func fetchWeatherDayAll(coords dashboardWeatherCoords) (weatherForecast, error) {
	u, err := url.Parse(dashboardForecastURL)
	if err != nil {
		return weatherForecast{}, err
	}
	q := u.Query()
	q.Set("latitude", fmt.Sprintf("%.4f", coords.Latitude))
	q.Set("longitude", fmt.Sprintf("%.4f", coords.Longitude))
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,apparent_temperature_mean,relative_humidity_2m_mean,wind_speed_10m_max")
	q.Set("forecast_days", fmt.Sprintf("%d", weatherDayMaxForecast))
	// Match the field name: Open-Meteo defaults to km/h.
	q.Set("wind_speed_unit", "ms")
	if coords.Timezone != "" {
		q.Set("timezone", coords.Timezone)
	} else {
		q.Set("timezone", "auto")
	}
	u.RawQuery = q.Encode()

	var payload struct {
		Daily struct {
			Time         []string  `json:"time"`
			Code         []int     `json:"weather_code"`
			TempMax      []float64 `json:"temperature_2m_max"`
			TempMin      []float64 `json:"temperature_2m_min"`
			ApparentTemp []float64 `json:"apparent_temperature_mean"`
			Humidity     []float64 `json:"relative_humidity_2m_mean"`
			WindSpeed    []float64 `json:"wind_speed_10m_max"`
		} `json:"daily"`
	}
	if err := getJSON(dashboardHTTPClient, u.String(), nil, &payload); err != nil {
		return weatherForecast{}, fmt.Errorf("forecast: %w", err)
	}

	d := payload.Daily
	var result weatherForecast
	n := len(d.Time)
	if n > weatherDayMaxForecast {
		n = weatherDayMaxForecast
	}
	for i := 0; i < n; i++ {
		if i >= len(d.Code) || i >= len(d.TempMax) || i >= len(d.TempMin) {
			break
		}
		date, err := time.Parse("2006-01-02", d.Time[i])
		if err != nil {
			date = time.Now().AddDate(0, 0, i)
		}
		humidity := 0
		if i < len(d.Humidity) {
			humidity = int(math.Round(d.Humidity[i]))
		}
		feelsLike := d.TempMax[i]
		if i < len(d.ApparentTemp) {
			feelsLike = d.ApparentTemp[i]
		}
		windSpeed := 0.0
		if i < len(d.WindSpeed) {
			windSpeed = d.WindSpeed[i]
		}
		entry := &weatherDayData{
			Date:        date,
			TempMaxC:    d.TempMax[i],
			TempMinC:    d.TempMin[i],
			FeelsLikeC:  feelsLike,
			HumidityPct: humidity,
			WindSpeedMS: windSpeed,
			Code:        d.Code[i],
		}
		result[i] = entry
	}
	return result, nil
}

func weatherDayPoll(ms int) time.Duration {
	if ms <= 0 {
		return weatherDayDefaultPoll
	}
	d := time.Duration(ms) * time.Millisecond
	if d < weatherDayMinPoll {
		return weatherDayMinPoll
	}
	return d
}
