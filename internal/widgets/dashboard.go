package widgets

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

const (
	dashboardTickInterval       = time.Second
	dashboardWeatherTimeout     = 8 * time.Second
	dashboardWeatherDefaultPoll = 10 * time.Minute
	dashboardWeatherMinPoll     = 60 * time.Second
)

var (
	dashboardHTTPClient  = &http.Client{Timeout: dashboardWeatherTimeout}
	dashboardGeocodeURL  = "https://geocoding-api.open-meteo.com/v1/search"
	dashboardForecastURL = "https://api.open-meteo.com/v1/forecast"
)

type dashboardWeatherState struct {
	CityName string
	TempC    *float64
	Code     int
	Ready    bool
	Error    string
}

type dashboardWeatherCoords struct {
	Name      string
	Latitude  float64
	Longitude float64
	Timezone  string
}

type WideDashboardWidget struct {
	*widgetBase
	city        string
	weatherPoll time.Duration

	// cache is the shared per-city weather (nil without a city).
	cache    *weatherCityCache
	subToken int

	mu           sync.RWMutex
	context      Context
	weather      dashboardWeatherState
	lastFrameKey string
}

func NewWideDashboardWidget(id string, cfg *config.Widget, notify func(string)) (*WideDashboardWidget, error) {
	base, err := newWidgetBase(id, "wide-dashboard", "dashboard.png", notify)
	if err != nil {
		return nil, err
	}
	w := &WideDashboardWidget{
		widgetBase:  base,
		city:        strings.TrimSpace(cfg.City),
		weatherPoll: dashboardWeatherPoll(cfg.PollMS),
	}
	if w.city == "" {
		w.weather = dashboardWeatherState{CityName: "Weather", Error: "Set city"}
	} else {
		// Shared with weather_day keys of the same city: one request
		// serves all of them.
		w.cache = weatherCacheForCity(w.city)
		token, cached := w.cache.subscribe(w.onWeather)
		w.subToken = token
		w.weather = dashboardStateFrom(cached, w.city)
	}
	if _, err := w.draw(time.Now(), false); err != nil {
		_ = w.Close()
		return nil, err
	}
	w.every(dashboardTickInterval, w.tick)
	if w.cache != nil {
		go w.cache.refresh(w.city, w.weatherPoll)
		w.every(time.Minute, func(time.Time) { w.cache.refresh(w.city, w.weatherPoll) })
	}
	return w, nil
}

// Close stops the widget and its weather subscription.
func (w *WideDashboardWidget) Close() error {
	if w.cache != nil {
		w.cache.unsubscribe(w.subToken)
	}
	return w.widgetBase.Close()
}

func (w *WideDashboardWidget) onWeather(u weatherUpdate) {
	if w.isClosed() {
		return
	}
	w.mu.Lock()
	w.weather = dashboardStateFrom(u, w.city)
	w.mu.Unlock()
	_, _ = w.render(time.Now())
}

// dashboardStateFrom maps the shared city weather to what the dashboard
// shows.
func dashboardStateFrom(u weatherUpdate, city string) dashboardWeatherState {
	state := dashboardWeatherState{CityName: u.CityName}
	if state.CityName == "" {
		state.CityName = city
	}
	switch {
	case u.Current != nil:
		temp := u.Current.TempC
		state.TempC = &temp
		state.Code = u.Current.Code
		state.Ready = true
	case u.Failed:
		state.Error = "Weather unavailable"
	}
	return state
}

// HandleGesture does nothing: the dashboard is display-only (inside a
// folder the event loop treats it as the Back key).
func (w *WideDashboardWidget) HandleGesture(ig.Kind) {}

// SetContext redraws for the new page/folder. It does not notify: the
// event loop calls it right before uploading the page, which already
// carries the fresh icon.
func (w *WideDashboardWidget) SetContext(ctx Context) {
	w.mu.Lock()
	if w.context == ctx {
		w.mu.Unlock()
		return
	}
	w.context = ctx
	w.mu.Unlock()
	_, _ = w.draw(time.Now(), false)
}

func (w *WideDashboardWidget) resume() {
	w.tick(time.Now())
	if w.cache != nil {
		go w.cache.refresh(w.city, w.weatherPoll)
	}
}

func (w *WideDashboardWidget) tick(now time.Time) { _, _ = w.render(now) }

// render redraws and notifies the Manager when the icon changed.
func (w *WideDashboardWidget) render(now time.Time) (bool, error) {
	return w.draw(now, true)
}

func (w *WideDashboardWidget) draw(now time.Time, notify bool) (bool, error) {
	w.mu.Lock()
	ctx := w.context
	weather := w.weather
	frameKey := dashboardFrameKey(now, ctx, weather)
	if frameKey == w.lastFrameKey {
		w.mu.Unlock()
		return false, nil
	}
	w.mu.Unlock()

	img := image.NewRGBA(image.Rect(0, 0, icons.WideWidth, icons.IconHeight))
	if ctx.InFolder() {
		renderDashboardBack(img)
	} else {
		renderDashboardClock(img, now, weather)
	}

	write := w.writeIcon
	if notify {
		write = w.publish
	}
	changed, err := write(img)
	if err != nil {
		return false, fmt.Errorf("write dashboard widget icon: %w", err)
	}
	w.mu.Lock()
	w.lastFrameKey = frameKey
	w.mu.Unlock()
	return changed, nil
}

// dashboardFrameKey identifies what the dashboard shows; a redraw is
// skipped while it stays the same. The weather is part of it so a fetch
// result shows up at once, not on the next minute change.
func dashboardFrameKey(now time.Time, ctx Context, weather dashboardWeatherState) string {
	if ctx.InFolder() {
		return fmt.Sprintf("back:%d:%d:%d", ctx.PageIndex, ctx.TotalPages, ctx.FolderDepth)
	}
	temp := "-"
	if weather.TempC != nil {
		temp = fmt.Sprintf("%.0f", math.Round(*weather.TempC))
	}
	return fmt.Sprintf("clock:%s:%d:%s:%d", now.Format("2006-01-02 15:04"), ctx.PageIndex, temp, weather.Code)
}

func dashboardWeatherPoll(ms int) time.Duration {
	if ms <= 0 {
		return dashboardWeatherDefaultPoll
	}
	d := time.Duration(ms) * time.Millisecond
	if d < dashboardWeatherMinPoll {
		return dashboardWeatherMinPoll
	}
	return d
}

func rectWH(x, y, w, h int) image.Rectangle {
	return image.Rect(x, y, x+w, y+h)
}

// rgb parses "RRGGBB" (optional "#") into an opaque color; invalid input
// yields transparent black.
func rgb(hex string) color.RGBA {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		return color.RGBA{}
	}
	var out uint32
	if _, err := fmt.Sscanf(hex, "%06x", &out); err != nil {
		return color.RGBA{}
	}
	return color.RGBA{R: uint8(out >> 16), G: uint8(out >> 8), B: uint8(out), A: 0xff}
}

func renderDashboardClock(img *image.RGBA, now time.Time, weather dashboardWeatherState) {
	draw.Draw(img, img.Bounds(), &image.Uniform{rgb("000000")}, image.Point{}, draw.Src)
	draw.Draw(img, rectWH(icons.WideWidth-4, 0, 4, icons.IconHeight), &image.Uniform{rgb("086fff")}, image.Point{}, draw.Src)

	// big clock — left area
	drawClockText(img, now.Format("15:04"), rectWH(24, 8, 310, 118), color.White, rgb("7a7a7a"), 100, -4)

	// date — bottom left
	drawText(img, strings.ToUpper(now.Format("02 Jan 2006")), FromLeft(24), FromBottom(30), 220, 44, AlignMiddleLeft, rgb("bbbbbb"), 35, true, -1)

	// weekday — bottom right (red on weekend)
	weekdayClr := color.Color(rgb("bbbbbb"))
	if wd := now.Weekday(); wd == time.Saturday || wd == time.Sunday {
		weekdayClr = rgb("ff4444")
	}
	drawText(img, strings.ToUpper(dashboardWeekday(now.Weekday())), FromRight(28), FromBottom(30), 200, 44, AlignMiddleRight, weekdayClr, 35, true, -1)

	// weather summary — top right: icon + temp on one row
	drawDashboardClockWeatherSummary(img, weather)
}

func renderDashboardBack(img *image.RGBA) {
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)
	drawIcon(img, "lucide/arrow-left", CenterX(-80), CenterY(0), 70, 70, rgb("aaaaaa"))
	drawText(img, "BACK", CenterX(40), CenterY(0), 140, 48, AlignMiddleLeft, color.White, 48, true)
}

func drawDashboardClockWeatherSummary(img *image.RGBA, weather dashboardWeatherState) {
	// temp + icon on same row, top-right corner
	const areaX, areaY, areaW, areaH = 310, 15, 120, 68
	iconSize := 45
	tempStr := "--°"
	if weather.TempC != nil {
		tempStr = fmt.Sprintf("%.0f°", math.Round(*weather.TempC))
	}
	iconClr := color.Color(rgb("888888"))
	if weather.TempC != nil {
		iconClr = weatherDayIconColor(weather.Code)
	}
	drawText(img, tempStr,
		FromLeft(areaX), FromTop(areaY), areaW-iconSize-4, areaH,
		AlignMiddleRight, color.White, 54, true)
	drawIcon(img, weatherCodeToLucideID(weather.Code),
		FromLeft(areaX+areaW-iconSize), FromTop(25), iconSize, iconSize, iconClr)
}

func drawLeftFace(img *image.RGBA, text string, x, baselineY int, clr color.Color, face font.Face) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(clr), Face: face}
	d.Dot = fixed.P(x, baselineY)
	d.DrawString(text)
}

func drawLeftFaceTracked(img *image.RGBA, text string, x, baselineY int, clr color.Color, face font.Face, tracking float64) {
	if tracking == 0 {
		drawLeftFace(img, text, x, baselineY, clr, face)
		return
	}
	d := &font.Drawer{Dst: img, Src: image.NewUniform(clr), Face: face}
	d.Dot = fixed.P(x, baselineY)
	step := fixed.Int26_6(tracking * 64)
	for _, r := range text {
		d.DrawString(string(r))
		d.Dot.X += step
	}
}

func drawFittedLeftText(img *image.RGBA, text string, rect image.Rectangle, clr color.Color, size float64, bold bool) {
	face, display := fitFace(text, rect.Dx(), size, 10, 1, bold)
	if face == nil {
		return
	}
	defer face.Close()
	baseline := rect.Min.Y + (rect.Dy()-face.Metrics().Height.Ceil())/2 + face.Metrics().Ascent.Ceil()
	drawLeftFace(img, display, rect.Min.X, baseline, clr, face)
}

func drawFittedLeftTextTracked(img *image.RGBA, text string, rect image.Rectangle, clr color.Color, size float64, bold bool, tracking float64) {
	face, display := fitFace(text, rect.Dx(), size, 10, 1, bold)
	if face == nil {
		return
	}
	defer face.Close()
	baseline := rect.Min.Y + (rect.Dy()-face.Metrics().Height.Ceil())/2 + face.Metrics().Ascent.Ceil()
	drawLeftFaceTracked(img, display, rect.Min.X, baseline, clr, face, tracking)
}

func drawClockText(img *image.RGBA, text string, rect image.Rectangle, mainClr, colonClr color.Color, size float64, tracking float64) {
	if len(text) != 5 || text[2] != ':' {
		drawFittedLeftTextTracked(img, text, rect, mainClr, size, true, tracking)
		return
	}
	face, _ := fitFace(text, rect.Dx(), size, 10, 1, true)
	if face == nil {
		return
	}
	defer face.Close()
	baseline := rect.Min.Y + (rect.Dy()-face.Metrics().Height.Ceil())/2 + face.Metrics().Ascent.Ceil()
	hours := text[:2]
	colon := text[2:3]
	minutes := text[3:]
	drawLeftFaceTracked(img, hours, rect.Min.X, baseline, mainClr, face, tracking)
	hoursW := trackedTextWidth(face, hours, tracking)
	drawLeftFaceTracked(img, colon, rect.Min.X+hoursW, baseline, colonClr, face, tracking)
	colonW := trackedTextWidth(face, colon, tracking)
	drawLeftFaceTracked(img, minutes, rect.Min.X+hoursW+colonW, baseline, mainClr, face, tracking)
}

func trackedTextWidth(face font.Face, text string, tracking float64) int {
	if text == "" {
		return 0
	}
	width := font.MeasureString(face, text).Round()
	runes := []rune(text)
	if len(runes) > 1 && tracking != 0 {
		width += int(math.Round(tracking * float64(len(runes)-1)))
	}
	return width
}

func drawDashboardCenteredText(img *image.RGBA, text string, rect image.Rectangle, clr color.Color, startSize float64) {
	face, display := fitFace(text, rect.Dx(), startSize, 11, 1.5, false)
	if face == nil {
		return
	}
	defer face.Close()
	baseline := rect.Min.Y + (rect.Dy()-face.Metrics().Height.Ceil())/2 + face.Metrics().Ascent.Ceil()
	drawCenteredFace(img, display, rect.Min.X+rect.Dx()/2, baseline, clr, face)
}

func drawDashboardLeftText(img *image.RGBA, text string, rect image.Rectangle, clr color.Color, startSize float64) {
	face, display := fitFace(text, rect.Dx(), startSize, 11, 1.5, false)
	if face == nil {
		return
	}
	defer face.Close()
	baseline := rect.Min.Y + (rect.Dy()-face.Metrics().Height.Ceil())/2 + face.Metrics().Ascent.Ceil()
	drawLeftFace(img, display, rect.Min.X, baseline, clr, face)
}

func dashboardWeekday(day time.Weekday) string {
	switch day {
	case time.Monday:
		return "Monday"
	case time.Tuesday:
		return "Tuesday"
	case time.Wednesday:
		return "Wednesday"
	case time.Thursday:
		return "Thursday"
	case time.Friday:
		return "Friday"
	case time.Saturday:
		return "Saturday"
	default:
		return "Sunday"
	}
}

func fetchDashboardCoords(city string) (*dashboardWeatherCoords, error) {
	u, err := url.Parse(dashboardGeocodeURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("name", city)
	q.Set("count", "1")
	q.Set("language", "en")
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	var payload struct {
		Results []struct {
			Name      string  `json:"name"`
			Country   string  `json:"country"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Timezone  string  `json:"timezone"`
		} `json:"results"`
	}
	if err := getJSON(dashboardHTTPClient, u.String(), nil, &payload); err != nil {
		return nil, fmt.Errorf("geocode: %w", err)
	}
	if len(payload.Results) == 0 {
		return nil, fmt.Errorf("city not found")
	}
	res := payload.Results[0]
	name := strings.TrimSpace(res.Name)
	if res.Country != "" {
		name = name + ", " + strings.TrimSpace(res.Country)
	}
	return &dashboardWeatherCoords{
		Name:      name,
		Latitude:  res.Latitude,
		Longitude: res.Longitude,
		Timezone:  res.Timezone,
	}, nil
}
