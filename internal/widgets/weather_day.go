package widgets

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
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

type weatherCityCache struct {
	mu        sync.Mutex
	coords    *dashboardWeatherCoords
	days      [weatherDayMaxForecast]*weatherDayData
	lastFetch time.Time
	fetching  bool
	// subscribers notified after fetch
	subs []func([weatherDayMaxForecast]*weatherDayData)
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
	c := &weatherCityCache{}
	weatherCaches[city] = c
	return c
}

// subscribe registers a callback invoked whenever new data arrives.
// Returns current data immediately (may be nil entries).
func (c *weatherCityCache) subscribe(fn func([weatherDayMaxForecast]*weatherDayData)) [weatherDayMaxForecast]*weatherDayData {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs = append(c.subs, fn)
	return c.days
}

func (c *weatherCityCache) unsubscribe(fn func([weatherDayMaxForecast]*weatherDayData)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, s := range c.subs {
		if &s == &fn {
			c.subs = append(c.subs[:i], c.subs[i+1:]...)
			return
		}
	}
}

// refresh fetches if stale. Only one fetch runs at a time; others return immediately.
func (c *weatherCityCache) refresh(city string, poll time.Duration) {
	c.mu.Lock()
	if c.fetching || (!c.lastFetch.IsZero() && time.Since(c.lastFetch) < poll) {
		c.mu.Unlock()
		return
	}
	c.fetching = true
	coords := c.coords
	c.mu.Unlock()

	// geocode if needed
	if coords == nil {
		resolved, err := fetchDashboardCoords(city)
		if err != nil {
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

	c.mu.Lock()
	c.fetching = false
	c.lastFetch = time.Now()
	if err == nil {
		c.days = days
	}
	subs := make([]func([weatherDayMaxForecast]*weatherDayData), len(c.subs))
	copy(subs, c.subs)
	current := c.days
	c.mu.Unlock()

	for _, fn := range subs {
		fn(current)
	}
}

// ── Widget ───────────────────────────────────────────────────────────────────

type WeatherDayWidget struct {
	id       string
	city     string
	dayIndex int
	pollMS   time.Duration
	iconPath string
	notify   func(string)
	cache    *weatherCityCache
	onData   func([weatherDayMaxForecast]*weatherDayData)

	mu           sync.Mutex
	closed       bool
	forecast     *weatherDayData
	lastFrameKey string
}

func NewWeatherDayWidget(id string, cfg *config.Widget, notify func(string)) (*WeatherDayWidget, error) {
	dir, err := os.MkdirTemp("", "ulanzi-weather-day-")
	if err != nil {
		return nil, fmt.Errorf("create weather day widget temp dir: %w", err)
	}
	dayIndex := cfg.DayIndex
	if dayIndex < 0 {
		dayIndex = 0
	} else if dayIndex >= weatherDayMaxForecast {
		dayIndex = weatherDayMaxForecast - 1
	}

	w := &WeatherDayWidget{
		id:       id,
		city:     strings.TrimSpace(cfg.City),
		dayIndex: dayIndex,
		pollMS:   weatherDayPoll(cfg.PollMS),
		iconPath: filepath.Join(dir, "weather_day.png"),
		notify:   notify,
		cache:    weatherCacheForCity(strings.TrimSpace(cfg.City)),
	}

	w.onData = func(days [weatherDayMaxForecast]*weatherDayData) {
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return
		}
		w.forecast = days[w.dayIndex]
		w.mu.Unlock()
		_ = w.render()
	}

	// Render placeholder immediately so icon file exists (not black)
	_ = w.renderPlaceholder()

	// Subscribe — get any already-cached data
	existing := w.cache.subscribe(w.onData)
	if existing[dayIndex] != nil {
		w.mu.Lock()
		w.forecast = existing[dayIndex]
		w.mu.Unlock()
		_ = w.render()
	}

	go w.loop()
	// Trigger initial fetch (shared — only one HTTP call for all widgets same city)
	go w.cache.refresh(w.city, w.pollMS)

	return w, nil
}

func (w *WeatherDayWidget) IconPath() string { return w.iconPath }

func (w *WeatherDayWidget) HandleGesture(_ ig.Kind) {}

func (w *WeatherDayWidget) SetContext(_ Context) {
	_ = w.render()
}

func (w *WeatherDayWidget) Close() error {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	w.cache.mu.Lock()
	for i, s := range w.cache.subs {
		_ = s
		if fmt.Sprintf("%p", w.cache.subs[i]) == fmt.Sprintf("%p", w.onData) {
			w.cache.subs = append(w.cache.subs[:i], w.cache.subs[i+1:]...)
			break
		}
	}
	w.cache.mu.Unlock()
	return os.RemoveAll(filepath.Dir(w.iconPath))
}

func (w *WeatherDayWidget) loop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		w.mu.Lock()
		closed := w.closed
		w.mu.Unlock()
		if closed {
			return
		}
		go w.cache.refresh(w.city, w.pollMS)
	}
}

// renderPlaceholder writes a non-black loading card so the icon is visible immediately.
func (w *WeatherDayWidget) renderPlaceholder() error {
	img := image.NewRGBA(image.Rect(0, 0, icons.IconWidth, icons.IconHeight))
	renderWeatherDayCard(img, w.dayIndex, nil, w.city)
	_, err := writePNGAtomicIfChanged(w.iconPath, img)
	return err
}

func (w *WeatherDayWidget) render() error {
	w.mu.Lock()
	forecast := w.forecast
	w.mu.Unlock()

	frameKey := weatherDayFrameKey(w.city, w.dayIndex, forecast)
	w.mu.Lock()
	same := w.lastFrameKey == frameKey
	w.mu.Unlock()
	if same {
		return nil
	}

	img := image.NewRGBA(image.Rect(0, 0, icons.IconWidth, icons.IconHeight))
	renderWeatherDayCard(img, w.dayIndex, forecast, w.city)

	changed, err := writePNGAtomicIfChanged(w.iconPath, img)
	if err != nil {
		return fmt.Errorf("write weather day icon: %w", err)
	}
	if changed {
		w.mu.Lock()
		w.lastFrameKey = frameKey
		w.mu.Unlock()
		w.notify(w.id)
	}
	return nil
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

func fetchWeatherDayAll(coords dashboardWeatherCoords) ([weatherDayMaxForecast]*weatherDayData, error) {
	u, err := url.Parse(dashboardForecastURL)
	if err != nil {
		return [weatherDayMaxForecast]*weatherDayData{}, err
	}
	q := u.Query()
	q.Set("latitude", fmt.Sprintf("%.4f", coords.Latitude))
	q.Set("longitude", fmt.Sprintf("%.4f", coords.Longitude))
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,apparent_temperature_mean,relative_humidity_2m_mean,wind_speed_10m_max")
	q.Set("forecast_days", fmt.Sprintf("%d", weatherDayMaxForecast))
	if coords.Timezone != "" {
		q.Set("timezone", coords.Timezone)
	} else {
		q.Set("timezone", "auto")
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return [weatherDayMaxForecast]*weatherDayData{}, err
	}
	resp, err := dashboardHTTPClient.Do(req)
	if err != nil {
		return [weatherDayMaxForecast]*weatherDayData{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return [weatherDayMaxForecast]*weatherDayData{}, fmt.Errorf("forecast status: %s", resp.Status)
	}

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
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return [weatherDayMaxForecast]*weatherDayData{}, err
	}

	d := payload.Daily
	var result [weatherDayMaxForecast]*weatherDayData
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
