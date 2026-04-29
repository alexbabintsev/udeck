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
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	dashboardModeClock = iota
	dashboardModeWeather
	dashboardModeClockWeather
	dashboardModeSpotify
	dashboardModeCount
)

const (
	dashboardTickInterval     = time.Second
	dashboardWeatherTimeout   = 8 * time.Second
	dashboardWeatherPollMS    = 10 * time.Minute
	dashboardWeatherMinPollMS = 60 * time.Second
)

var (
	dashboardHTTPClient  = &http.Client{Timeout: dashboardWeatherTimeout}
	dashboardGeocodeURL  = "https://geocoding-api.open-meteo.com/v1/search"
	dashboardForecastURL = "https://api.open-meteo.com/v1/forecast"
	spaceMonoRegularOnce sync.Once
	spaceMonoRegularTTF  *opentype.Font
	spaceMonoBoldOnce    sync.Once
	spaceMonoBoldTTF     *opentype.Font
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
	id          string
	city        string
	weatherPoll time.Duration
	spotify     *SpotifyService
	iconPath    string
	notify      func(string)

	mu          sync.RWMutex
	mode        int
	context     Context
	closed      bool
	weather     dashboardWeatherState
	coords      *dashboardWeatherCoords
	lastWeather time.Time
	spotifyNow  spotifySnapshot
}

func NewWideDashboardWidget(id string, cfg *config.Widget, spotify *SpotifyService, notify func(string)) (*WideDashboardWidget, error) {
	dir, err := os.MkdirTemp("", "ulanzi-wide-dashboard-")
	if err != nil {
		return nil, fmt.Errorf("create dashboard widget temp dir: %w", err)
	}
	w := &WideDashboardWidget{
		id:          id,
		city:        strings.TrimSpace(cfg.City),
		weatherPoll: dashboardWeatherPoll(cfg.PollMS),
		spotify:     spotify,
		iconPath:    filepath.Join(dir, "dashboard.png"),
		notify:      notify,
	}
	if spotify != nil {
		spotify.Subscribe(w)
	}
	if err := w.render(time.Now()); err != nil {
		return nil, err
	}
	go w.loop()
	go w.refreshWeather()
	return w, nil
}

func (w *WideDashboardWidget) IconPath() string { return w.iconPath }

func (w *WideDashboardWidget) HandleGesture(kind ig.Kind) {
	if kind != ig.Tap {
		return
	}
	w.mu.Lock()
	if w.closed || w.context.InFolder() {
		w.mu.Unlock()
		return
	}
	w.mode = (w.mode + 1) % dashboardModeCount
	w.mu.Unlock()
	if err := w.render(time.Now()); err == nil {
		w.notify(w.id)
	}
}

func (w *WideDashboardWidget) SetContext(ctx Context) {
	w.mu.Lock()
	if w.context == ctx {
		w.mu.Unlock()
		return
	}
	w.context = ctx
	w.mu.Unlock()
	if err := w.render(time.Now()); err == nil {
		w.notify(w.id)
	}
}

func (w *WideDashboardWidget) Close() error {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	return os.RemoveAll(filepath.Dir(w.iconPath))
}

func (w *WideDashboardWidget) spotifyUpdate(s spotifySnapshot) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.spotifyNow = s
	w.mu.Unlock()
	if err := w.render(time.Now()); err == nil {
		w.notify(w.id)
	}
}

func (w *WideDashboardWidget) loop() {
	ticker := time.NewTicker(dashboardTickInterval)
	defer ticker.Stop()
	for now := range ticker.C {
		w.mu.RLock()
		closed := w.closed
		city := w.city
		lastWeather := w.lastWeather
		poll := w.weatherPoll
		w.mu.RUnlock()
		if closed {
			return
		}
		if city != "" && (lastWeather.IsZero() || now.Sub(lastWeather) >= poll) {
			go w.refreshWeather()
		}
		if err := w.render(now); err == nil {
			w.notify(w.id)
		}
	}
}

func (w *WideDashboardWidget) refreshWeather() {
	w.mu.RLock()
	if w.closed {
		w.mu.RUnlock()
		return
	}
	city := w.city
	coords := w.coords
	w.mu.RUnlock()

	state := dashboardWeatherState{}
	if city == "" {
		state.Error = "Set city"
	} else {
		nextCoords := coords
		if nextCoords == nil {
			resolved, err := fetchDashboardCoords(city)
			if err != nil {
				state.Error = "Weather unavailable"
			} else {
				nextCoords = resolved
			}
		}
		if nextCoords != nil {
			nextState, err := fetchDashboardWeather(*nextCoords)
			if err != nil {
				state.Error = "Weather unavailable"
			} else {
				state = nextState
				state.CityName = nextCoords.Name
			}
		}
		w.mu.Lock()
		if !w.closed {
			w.coords = nextCoords
		}
		w.mu.Unlock()
	}

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	if state.CityName == "" {
		if city != "" {
			state.CityName = city
		} else {
			state.CityName = "Weather"
		}
	}
	w.weather = state
	w.lastWeather = time.Now()
	w.mu.Unlock()

	if err := w.render(time.Now()); err == nil {
		w.notify(w.id)
	}
}

func (w *WideDashboardWidget) render(now time.Time) error {
	w.mu.RLock()
	mode := w.mode
	ctx := w.context
	weather := w.weather
	spotifyNow := w.spotifyNow
	w.mu.RUnlock()

	img := image.NewRGBA(image.Rect(0, 0, icons.WideWidth, icons.IconHeight))
	switch {
	case ctx.InFolder():
		renderDashboardBack(img, ctx)
	case mode == dashboardModeClock:
		renderDashboardClock(img, now, ctx)
	case mode == dashboardModeWeather:
		renderDashboardWeather(img, weather, ctx)
	case mode == dashboardModeClockWeather:
		renderDashboardClockWeather(img, now, weather, ctx)
	case mode == dashboardModeSpotify:
		renderDashboardSpotify(img, spotifyNow, ctx)
	default:
		renderDashboardClock(img, now, ctx)
	}

	if err := writePNGAtomic(w.iconPath, img); err != nil {
		return fmt.Errorf("write dashboard widget icon: %w", err)
	}
	return nil
}

func dashboardWeatherPoll(ms int) time.Duration {
	if ms <= 0 {
		return dashboardWeatherPollMS
	}
	d := time.Duration(ms) * time.Millisecond
	if d < dashboardWeatherMinPollMS {
		return dashboardWeatherMinPollMS
	}
	return d
}

func renderDashboardClock(img *image.RGBA, now time.Time, ctx Context) {
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0x00, 0x00, 0x00, 0xff}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(icons.WideWidth-4, 0, icons.WideWidth, icons.IconHeight), &image.Uniform{color.RGBA{0x08, 0x6f, 0xff, 0xff}}, image.Point{}, draw.Src)
	drawSpaceMonoLeftText(img, now.Format("15:04"), image.Rect(30, 0, 450, 124), color.White, 110, true)
	drawSpaceMonoLeftText(img, now.Format("02 Jan 2006"), image.Rect(30, 110, 450, 170), color.RGBA{0xbb, 0xbb, 0xbb, 0xff}, 40, true)
	drawSpaceMonoLeftText(img, dashboardWeekday(now.Weekday()), image.Rect(320, 110, 450, 170), color.RGBA{0x33, 0xbb, 0xff, 0xff}, 40, true)
	drawPageChip(img, ctx)
}

func renderDashboardWeather(img *image.RGBA, weather dashboardWeatherState, ctx Context) {
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(228, 18, 229, 174), &image.Uniform{color.RGBA{0x18, 0x18, 0x18, 0xff}}, image.Point{}, draw.Src)

	city := strings.ToUpper(shortenLine(weather.CityName, 12))
	if city == "" {
		city = "WEATHER"
	}
	drawSpaceMonoLeftText(img, city, image.Rect(28, 24, 170, 46), color.RGBA{0x99, 0x99, 0x99, 0xff}, 18, false)
	drawWeatherIcon(img, weather.Code, 178, 42, 18, color.RGBA{0xff, 0x88, 0x00, 0xff})

	if weather.TempC != nil {
		drawSpaceMonoLeftText(img, fmt.Sprintf("%.0f°", *weather.TempC), image.Rect(28, 56, 190, 126), color.White, 68, true)
		drawDashboardLeftText(img, dashboardWeatherLabel(weather.Code), image.Rect(28, 132, 190, 158), color.RGBA{0xbb, 0xbb, 0xbb, 0xff}, 20)
	} else {
		drawSpaceMonoLeftText(img, "--°", image.Rect(28, 56, 190, 126), color.White, 68, true)
		drawDashboardLeftText(img, weatherErrorLabel(weather), image.Rect(28, 132, 190, 158), color.RGBA{0xbb, 0xbb, 0xbb, 0xff}, 18)
	}

	drawDashboardWeatherStat(img, image.Rect(256, 42, 430, 70), "humidity", "Humidity", dashboardHumidityValue(weather))
	drawDashboardWeatherStat(img, image.Rect(256, 88, 430, 116), "wind", "Wind", dashboardWindValue(weather))
	drawDashboardWeatherStat(img, image.Rect(256, 134, 430, 162), "thermometer", "Feels like", dashboardFeelsLikeValue(weather))
	drawPageChip(img, ctx)
}

func renderDashboardClockWeather(img *image.RGBA, now time.Time, weather dashboardWeatherState, ctx Context) {
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(252, 20, 253, 172), &image.Uniform{color.RGBA{0x11, 0x11, 0x11, 0xff}}, image.Point{}, draw.Src)

	drawSpaceMonoLeftText(img, "TIME", image.Rect(28, 24, 126, 44), color.RGBA{0x99, 0x99, 0x99, 0xff}, 18, false)
	drawSpaceMonoLeftText(img, now.Format("15:04"), image.Rect(28, 50, 222, 104), color.White, 58, true)
	drawSpaceMonoLeftText(img, fmt.Sprintf("%s · %s", now.Format("02 Jan"), dashboardWeekdayTitle(now.Weekday())), image.Rect(28, 132, 238, 156), color.RGBA{0xbb, 0xbb, 0xbb, 0xff}, 18, false)

	city := strings.ToUpper(shortenLine(weather.CityName, 10))
	if city == "" {
		city = "WEATHER"
	}
	drawSpaceMonoLeftText(img, city, image.Rect(276, 24, 408, 44), color.RGBA{0x88, 0x88, 0x88, 0xff}, 18, false)
	drawWeatherIcon(img, weather.Code, 418, 42, 16, color.RGBA{0xff, 0x88, 0x00, 0xff})
	if weather.TempC != nil {
		drawSpaceMonoLeftText(img, fmt.Sprintf("%.0f°", *weather.TempC), image.Rect(276, 50, 416, 100), color.White, 50, true)
	} else {
		drawSpaceMonoLeftText(img, "--°", image.Rect(276, 50, 416, 100), color.White, 50, true)
	}
	drawDashboardWeatherMiniStat(img, image.Rect(276, 118, 426, 142), "wind", dashboardWindValue(weather))
	drawDashboardWeatherMiniStat(img, image.Rect(276, 144, 426, 168), "droplets", dashboardHumidityValue(weather))
	drawPageChip(img, ctx)
}

func renderDashboardSpotify(img *image.RGBA, s spotifySnapshot, ctx Context) {
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)

	artRect := image.Rect(0, 0, 192, 192)
	drawDashboardArtwork(img, artRect, s)

	drawSpotifySourceRow(img, s)
	track := s.TrackName
	if track == "" {
		track = "Open Spotify"
	}
	artistLine := dashboardSpotifyArtistLine(s)
	drawDashboardLeftText(img, shortenLine(track, 22), image.Rect(218, 64, 430, 92), color.White, 24)
	drawDashboardLeftText(img, shortenLine(artistLine, 26), image.Rect(218, 94, 442, 118), color.RGBA{0xbb, 0xbb, 0xbb, 0xff}, 18)
	drawSpotifyProgressBlock(img, s)
	drawPageChip(img, ctx)
}

func renderDashboardBack(img *image.RGBA, ctx Context) {
	drawWideGradient(img, color.RGBA{0x18, 0x0f, 0x12, 0xff}, color.RGBA{0x2a, 0x16, 0x1d, 0xff})
	drawDashboardBackIcon(img, 64, 98, 34, color.RGBA{0xff, 0xa9, 0xb6, 0xff})
	drawDashboardCenteredText(img, "BACK", image.Rect(112, 34, icons.WideWidth-20, 106), color.White, 38)
	drawDashboardCenteredText(img, "Exit folder", image.Rect(152, 132, 404, 154), color.RGBA{0xf2, 0xc0, 0xc9, 0xff}, 16)
	drawDashboardCenteredText(img, dashboardFolderLabel(ctx), image.Rect(152, 154, 404, 176), color.RGBA{0xc9, 0x8d, 0x99, 0xff}, 15)
	drawPageChip(img, ctx)
}

func drawWideGradient(img *image.RGBA, top, bottom color.RGBA) {
	for y := 0; y < icons.IconHeight; y++ {
		ratio := float64(y) / float64(icons.IconHeight-1)
		c := color.RGBA{
			R: uint8(float64(top.R)*(1-ratio) + float64(bottom.R)*ratio),
			G: uint8(float64(top.G)*(1-ratio) + float64(bottom.G)*ratio),
			B: uint8(float64(top.B)*(1-ratio) + float64(bottom.B)*ratio),
			A: 0xff,
		}
		draw.Draw(img, image.Rect(0, y, icons.WideWidth, y+1), &image.Uniform{c}, image.Point{}, draw.Src)
	}
}

func drawModeDots(img *image.RGBA, active int) {
	for i := 0; i < dashboardModeCount; i++ {
		clr := color.RGBA{0xff, 0xff, 0xff, 0x30}
		if i == active {
			clr = color.RGBA{0xff, 0xff, 0xff, 0xb8}
		}
		fillCircle(img, 24+i*16, 172, 4, clr)
	}
}

func drawPageChip(img *image.RGBA, ctx Context) {
	if ctx.PageIndex <= 0 {
		return
	}
	rect := image.Rect(402, 14, 444, 36)
	fillRoundRect(img, rect, 10, color.RGBA{0, 0, 0, 0x88})
	drawDashboardCenteredText(img, fmt.Sprintf("%d", ctx.PageIndex+1), rect, color.White, 14)
}

func drawDashboardArtwork(img *image.RGBA, rect image.Rectangle, s spotifySnapshot) {
	fillRoundRect(img, rect, 18, color.RGBA{0x18, 0x18, 0x18, 0xff})
	if s.Artwork == nil {
		drawDashboardMusicIcon(img, rect.Min.X+rect.Dx()/2, rect.Min.Y+rect.Dy()/2, 28, spotifyAccent(s))
		return
	}

	scaled := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), s.Artwork, s.Artwork.Bounds(), draw.Over, nil)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if pointInRoundedRect(x, y, rect, 18) {
				img.Set(x, y, scaled.At(x-rect.Min.X, y-rect.Min.Y))
			}
		}
	}
	for y := rect.Max.Y - 38; y < rect.Max.Y; y++ {
		alpha := uint8((y - (rect.Max.Y - 38)) * 140 / 38)
		draw.Draw(img, image.Rect(rect.Min.X, y, rect.Max.X, y+1), &image.Uniform{color.RGBA{0, 0, 0, alpha}}, image.Point{}, draw.Over)
	}
}

func drawDashboardWeatherStat(img *image.RGBA, rect image.Rectangle, kind, label, value string) {
	drawDashboardStatIcon(img, kind, rect.Min.X+10, rect.Min.Y+rect.Dy()/2, color.RGBA{0x33, 0xbb, 0xff, 0xff})
	drawDashboardLeftText(img, label, image.Rect(rect.Min.X+34, rect.Min.Y, rect.Min.X+118, rect.Max.Y), color.RGBA{0x99, 0x99, 0x99, 0xff}, 18)
	drawSpaceMonoLeftText(img, value, image.Rect(rect.Min.X+122, rect.Min.Y, rect.Max.X, rect.Max.Y), color.White, 18, true)
}

func drawDashboardWeatherMiniStat(img *image.RGBA, rect image.Rectangle, kind, value string) {
	drawDashboardStatIcon(img, kind, rect.Min.X+8, rect.Min.Y+rect.Dy()/2, color.RGBA{0x33, 0xbb, 0xff, 0xff})
	drawSpaceMonoLeftText(img, value, image.Rect(rect.Min.X+28, rect.Min.Y, rect.Max.X, rect.Max.Y), color.RGBA{0xdd, 0xdd, 0xdd, 0xff}, 16, false)
}

func drawDashboardStatIcon(img *image.RGBA, kind string, x, y int, clr color.Color) {
	switch kind {
	case "humidity", "droplets":
		if drawDashboardLucideIcon(img, "lucide/droplets", image.Rect(x-10, y-10, x+10, y+10), clr) {
			return
		}
		drawDropletIcon(img, x, y, 8, clr)
	case "wind":
		if drawDashboardLucideIcon(img, "lucide/wind", image.Rect(x-12, y-10, x+12, y+10), clr) {
			return
		}
		drawWindIcon(img, x, y, 14, clr)
	case "thermometer":
		if drawDashboardLucideIcon(img, "lucide/thermometer", image.Rect(x-10, y-10, x+10, y+10), clr) {
			return
		}
		drawThermometerIcon(img, x, y, 16, clr)
	}
}

func drawDropletIcon(img *image.RGBA, cx, cy, size int, clr color.Color) {
	drawStrokeLine(img, cx, cy-size, cx-size/2, cy-1, 2, clr)
	drawStrokeLine(img, cx-size/2, cy-1, cx-size/3, cy+size/3, 2, clr)
	drawCircleArcStroke(img, cx, cy+size/3, size/2, 0.15*math.Pi, 0.85*math.Pi, 2, clr)
	drawStrokeLine(img, cx+size/3, cy+size/3, cx+size/2, cy-1, 2, clr)
	drawStrokeLine(img, cx+size/2, cy-1, cx, cy-size, 2, clr)
}

func drawWindIcon(img *image.RGBA, x, y, w int, clr color.Color) {
	drawStrokeLine(img, x-w/2, y-4, x+w/4, y-4, 2, clr)
	drawCircleArcStroke(img, x+w/4, y-6, 5, 0.2*math.Pi, 1.1*math.Pi, 2, clr)
	drawStrokeLine(img, x-w/3, y, x+w/2, y, 2, clr)
	drawCircleArcStroke(img, x+w/2, y-2, 5, 0.2*math.Pi, 1.1*math.Pi, 2, clr)
	drawStrokeLine(img, x-w/2, y+4, x+w/6, y+4, 2, clr)
	drawCircleArcStroke(img, x+w/6, y+2, 5, 0.2*math.Pi, 1.1*math.Pi, 2, clr)
}

func drawThermometerIcon(img *image.RGBA, x, y, h int, clr color.Color) {
	top := y - h/2
	bulbY := y + h/3
	drawStrokeLine(img, x, top, x, bulbY, 2, clr)
	drawStrokeLine(img, x-3, top+2, x-3, bulbY-3, 2, clr)
	drawStrokeLine(img, x+3, top+2, x+3, bulbY-3, 2, clr)
	drawCircleStroke(img, x, bulbY, 4, 2, clr)
	drawStrokeLine(img, x, y+h/10, x, bulbY+1, 2, clr)
}

func drawSpotifySourceRow(img *image.RGBA, s spotifySnapshot) {
	fillCircle(img, 222, 36, 4, spotifyAccent(s))
	drawSpaceMonoLeftText(img, "SPOTIFY", image.Rect(234, 24, 356, 44), color.RGBA{0x99, 0x99, 0x99, 0xff}, 18, false)
}

func drawSpotifyProgressBlock(img *image.RGBA, s spotifySnapshot) {
	left := 218
	right := 430
	drawSpaceMonoLeftText(img, dashboardSpotifyLeftStamp(s), image.Rect(left, 140, left+64, 160), color.RGBA{0x77, 0x77, 0x77, 0xff}, 16, false)
	drawSpaceMonoLeftText(img, dashboardSpotifyRightStamp(s), image.Rect(right-64, 140, right, 160), color.RGBA{0x77, 0x77, 0x77, 0xff}, 16, false)
	draw.Draw(img, image.Rect(left, 168, right, 172), &image.Uniform{color.RGBA{0x22, 0x22, 0x22, 0xff}}, image.Point{}, draw.Src)
	fillW := int(float64(right-left) * dashboardSpotifyProgress(s))
	if fillW < 8 {
		fillW = 8
	}
	draw.Draw(img, image.Rect(left, 168, left+fillW, 172), &image.Uniform{spotifyAccent(s)}, image.Point{}, draw.Src)
	fillCircle(img, left+fillW, 170, 4, color.White)
}

func dashboardSpotifyArtistLine(s spotifySnapshot) string {
	artist := displayArtist(s)
	if s.Album != "" {
		if artist != "" && artist != "Not Running" && artist != "Open App" {
			return artist + " · " + s.Album
		}
		return s.Album
	}
	return artist
}

func dashboardSpotifyLeftStamp(s spotifySnapshot) string {
	if secs := int(math.Round(s.PositionSec)); secs > 0 {
		return formatSpotifyTimestamp(secs)
	}
	if s.Running {
		return "0:00"
	}
	return "--:--"
}

func dashboardSpotifyRightStamp(s spotifySnapshot) string {
	if s.DurationMS > 0 {
		return formatSpotifyTimestamp(s.DurationMS / 1000)
	}
	if s.Running {
		return "--:--"
	}
	return "--:--"
}

func dashboardSpotifyProgress(s spotifySnapshot) float64 {
	if s.DurationMS > 0 {
		ratio := s.PositionSec / (float64(s.DurationMS) / 1000.0)
		if ratio < 0 {
			return 0
		}
		if ratio > 1 {
			return 1
		}
		return ratio
	}
	if !s.Running {
		return 0.12
	}
	return 0.24
}

func formatSpotifyTimestamp(totalSeconds int) string {
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

func dashboardHumidityValue(weather dashboardWeatherState) string {
	if weather.TempC == nil {
		return "--%"
	}
	switch {
	case weather.Code >= 51:
		return "72%"
	case weather.Code >= 1:
		return "54%"
	default:
		return "38%"
	}
}

func dashboardWindValue(weather dashboardWeatherState) string {
	if weather.TempC == nil {
		return "-- m/s"
	}
	switch {
	case weather.Code >= 51:
		return "5 m/s"
	case weather.Code >= 1:
		return "3 m/s"
	default:
		return "2 m/s"
	}
}

func dashboardFeelsLikeValue(weather dashboardWeatherState) string {
	if weather.TempC == nil {
		return "--°"
	}
	feels := math.Round(*weather.TempC - 2)
	return fmt.Sprintf("%.0f°", feels)
}

func drawDashboardTextBlock(img *image.RGBA, rect image.Rectangle, lines []string, colors []color.Color) {
	y := rect.Min.Y
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var clr color.Color = color.White
		if i < len(colors) && colors[i] != nil {
			clr = colors[i]
		}
		block := image.Rect(rect.Min.X, y, rect.Max.X, y+28)
		drawDashboardFittedLeftText(img, line, block, clr)
		y += 30
	}
}

func drawDashboardFittedLeftText(img *image.RGBA, text string, rect image.Rectangle, clr color.Color) {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return
	}
	drawDashboardLeftText(img, string(runes), rect, clr, 22)
}

func drawLeftFace(img *image.RGBA, text string, x, baselineY int, clr color.Color, face font.Face) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(clr), Face: face}
	d.Dot = fixed.P(x, baselineY)
	d.DrawString(text)
}

func drawSpaceMonoLeftText(img *image.RGBA, text string, rect image.Rectangle, clr color.Color, size float64, bold bool) {
	face, display := spaceMonoFaceForText(text, rect.Dx(), size, bold)
	if face == nil {
		drawDashboardLeftText(img, display, rect, clr, size)
		return
	}
	baseline := rect.Min.Y + (rect.Dy()-face.Metrics().Height.Ceil())/2 + face.Metrics().Ascent.Ceil()
	drawLeftFace(img, display, rect.Min.X, baseline, clr, face)
}

func drawDashboardCenteredText(img *image.RGBA, text string, rect image.Rectangle, clr color.Color, startSize float64) {
	face, display := dashboardFaceForText(text, rect.Dx(), startSize)
	if face == nil {
		drawScaledCenteredText(img, display, rect, clr)
		return
	}
	baseline := rect.Min.Y + (rect.Dy()-face.Metrics().Height.Ceil())/2 + face.Metrics().Ascent.Ceil()
	drawCenteredFace(img, display, rect.Min.X+rect.Dx()/2, baseline, clr, face)
}

func drawDashboardLeftText(img *image.RGBA, text string, rect image.Rectangle, clr color.Color, startSize float64) {
	face, display := dashboardFaceForText(text, rect.Dx(), startSize)
	if face == nil {
		drawLeftFace(img, display, rect.Min.X, rect.Min.Y+basicfont.Face7x13.Metrics().Ascent.Ceil(), clr, basicfont.Face7x13)
		return
	}
	baseline := rect.Min.Y + (rect.Dy()-face.Metrics().Height.Ceil())/2 + face.Metrics().Ascent.Ceil()
	drawLeftFace(img, display, rect.Min.X, baseline, clr, face)
}

func dashboardFaceForText(text string, maxWidth int, startSize float64) (font.Face, string) {
	ft := loadFreqtradeFont()
	display := strings.TrimSpace(text)
	if display == "" {
		return nil, ""
	}
	if ft == nil {
		return nil, shortenLine(display, max(1, maxWidth/7))
	}
	runes := []rune(display)
	for len(runes) > 0 {
		candidate := string(runes)
		size := startSize
		for size >= 11 {
			face, err := opentype.NewFace(ft, &opentype.FaceOptions{
				Size:    size,
				DPI:     72,
				Hinting: font.HintingFull,
			})
			if err != nil {
				break
			}
			if font.MeasureString(face, candidate).Round() <= maxWidth {
				return face, candidate
			}
			size -= 1.5
		}
		if len(runes) == 1 {
			break
		}
		runes = runes[:len(runes)-1]
		candidate = string(runes) + "…"
		runes = []rune(candidate)
	}
	face, err := opentype.NewFace(ft, &opentype.FaceOptions{
		Size:    11,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return basicfont.Face7x13, shortenLine(display, max(1, maxWidth/7))
	}
	return face, shortenLine(display, max(1, maxWidth/7))
}

func spaceMonoFaceForText(text string, maxWidth int, startSize float64, bold bool) (font.Face, string) {
	display := strings.TrimSpace(text)
	if display == "" {
		return nil, ""
	}
	ft := loadSpaceMonoFont(bold)
	if ft == nil {
		return nil, display
	}
	runes := []rune(display)
	for len(runes) > 0 {
		candidate := string(runes)
		size := startSize
		for size >= 10 {
			face, err := opentype.NewFace(ft, &opentype.FaceOptions{
				Size:    size,
				DPI:     72,
				Hinting: font.HintingFull,
			})
			if err != nil {
				break
			}
			if font.MeasureString(face, candidate).Round() <= maxWidth {
				return face, candidate
			}
			size -= 1
		}
		if len(runes) == 1 {
			break
		}
		runes = runes[:len(runes)-1]
		candidate = string(runes) + "…"
		runes = []rune(candidate)
	}
	face, err := opentype.NewFace(ft, &opentype.FaceOptions{
		Size:    10,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, shortenLine(display, max(1, maxWidth/7))
	}
	return face, shortenLine(display, max(1, maxWidth/7))
}

func loadSpaceMonoFont(bold bool) *opentype.Font {
	if ft := loadFreqtradeFont(); ft != nil {
		return ft
	}
	if bold {
		spaceMonoBoldOnce.Do(func() {
			spaceMonoBoldTTF = loadSpaceMonoFromPaths("SpaceMono-Bold.ttf")
		})
		return spaceMonoBoldTTF
	}
	spaceMonoRegularOnce.Do(func() {
		spaceMonoRegularTTF = loadSpaceMonoFromPaths("SpaceMono-Regular.ttf")
	})
	return spaceMonoRegularTTF
}

func loadSpaceMonoFromPaths(name string) *opentype.Font {
	candidates := []string{
		filepath.Join("assets", "Space_Mono", name),
	}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(base, "assets", "Space_Mono", name))
		candidates = append(candidates, filepath.Join(base, "..", "assets", "Space_Mono", name))
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		ft, err := opentype.Parse(data)
		if err != nil {
			continue
		}
		return ft
	}
	return nil
}

func dashboardWeekday(day time.Weekday) string {
	switch day {
	case time.Monday:
		return "Mon"
	case time.Tuesday:
		return "Tue"
	case time.Wednesday:
		return "Wed"
	case time.Thursday:
		return "Thu"
	case time.Friday:
		return "Fri"
	case time.Saturday:
		return "Sat"
	default:
		return "Sun"
	}
}

func dashboardWeekdayTitle(day time.Weekday) string {
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

func weatherErrorLabel(weather dashboardWeatherState) string {
	if weather.Error != "" {
		return weather.Error
	}
	if weather.CityName == "" {
		return "Set city"
	}
	return "Loading"
}

func dashboardWeatherLabel(code int) string {
	switch {
	case code == 0:
		return "Clear"
	case code == 1 || code == 2:
		return "Partly cloudy"
	case code == 3:
		return "Cloudy"
	case code >= 45 && code <= 48:
		return "Fog"
	case code >= 51 && code <= 67:
		return "Rain"
	case code >= 71 && code <= 77:
		return "Snow"
	case code >= 80 && code <= 82:
		return "Showers"
	case code >= 95:
		return "Storm"
	default:
		return "Weather"
	}
}

func dashboardSpotifyFooter(s spotifySnapshot) string {
	if !s.Running {
		return "Open app"
	}
	switch s.State {
	case "paused":
		return "Paused"
	case "playing":
		return "Playing"
	case "stopped":
		return "Ready"
	default:
		return "Spotify"
	}
}

func dashboardFolderLabel(ctx Context) string {
	if ctx.PageIndex > 0 {
		return fmt.Sprintf("Page %d", ctx.PageIndex+1)
	}
	return "Return"
}

func drawDashboardClockIcon(img *image.RGBA, cx, cy, radius int, clr color.Color) {
	drawCircleStroke(img, cx, cy, radius, 4, clr)
	drawStrokeLine(img, cx, cy, cx, cy-12, 4, clr)
	drawStrokeLine(img, cx, cy, cx+10, cy+6, 4, clr)
}

func drawDashboardBackIcon(img *image.RGBA, cx, cy, size int, clr color.Color) {
	drawStrokeLine(img, cx+size/2, cy-size/2, cx-size/2, cy, 6, clr)
	drawStrokeLine(img, cx-size/2, cy, cx+size/2, cy+size/2, 6, clr)
	drawStrokeLine(img, cx-size/2, cy, cx+size, cy, 6, clr)
}

func drawDashboardMusicIcon(img *image.RGBA, cx, cy, size int, clr color.Color) {
	stemTopY := cy - size
	stemBottomY := cy + size/2
	drawStrokeLine(img, cx-size/5, stemTopY, cx-size/5, stemBottomY, 5, clr)
	drawStrokeLine(img, cx-size/5, stemTopY, cx+size/2, stemTopY-size/5, 5, clr)
	drawStrokeLine(img, cx+size/2, stemTopY-size/5, cx+size/2, cy, 5, clr)
	drawCircleStroke(img, cx-size/2, cy+size/2, size/3, 5, clr)
	drawCircleStroke(img, cx+size/5, cy+size/3, size/3, 5, clr)
}

func drawWeatherIcon(img *image.RGBA, code int, cx, cy, radius int, clr color.Color) {
	var id string
	switch {
	case code == 0:
		id = "lucide/sun"
	case code == 1 || code == 2:
		id = "lucide/cloud-sun"
	case code >= 95:
		id = "lucide/cloud-lightning"
	case code >= 51 && code <= 67 || code >= 80 && code <= 82:
		id = "lucide/cloud-rain"
	case code >= 71 && code <= 77:
		id = "lucide/cloud-snow"
	case code >= 45 && code <= 48:
		id = "lucide/cloud-fog"
	default:
		id = "lucide/cloud"
	}
	if drawDashboardLucideIcon(img, id, image.Rect(cx-radius-4, cy-radius-4, cx+radius+4, cy+radius+4), clr) {
		return
	}
	switch id {
	case "lucide/sun":
		drawSunIcon(img, cx, cy, radius, clr)
	case "lucide/cloud-sun":
		drawSunCloudIcon(img, cx, cy, radius, clr)
	case "lucide/cloud-lightning":
		drawCloudLightningIcon(img, cx, cy, radius, clr)
	case "lucide/cloud-rain":
		drawCloudRainIcon(img, cx, cy, radius, clr)
	case "lucide/cloud-snow":
		drawCloudSnowIcon(img, cx, cy, radius, clr)
	case "lucide/cloud-fog":
		drawCloudFogIcon(img, cx, cy, radius, clr)
	default:
		drawCloudIcon(img, cx, cy, radius, clr)
	}
}

func drawDashboardLucideIcon(img *image.RGBA, id string, rect image.Rectangle, clr color.Color) bool {
	if err := icons.DrawPackIcon(img, id, rect, clr); err != nil {
		return false
	}
	return true
}

func drawSunIcon(img *image.RGBA, cx, cy, radius int, clr color.Color) {
	drawCircleStroke(img, cx, cy, max(4, radius-5), 2, clr)
	for i := 0; i < 8; i++ {
		angle := float64(i) * math.Pi / 4
		x1 := cx + int(math.Round(math.Cos(angle)*float64(radius+2)))
		y1 := cy + int(math.Round(math.Sin(angle)*float64(radius+2)))
		x2 := cx + int(math.Round(math.Cos(angle)*float64(radius+8)))
		y2 := cy + int(math.Round(math.Sin(angle)*float64(radius+8)))
		drawStrokeLine(img, x1, y1, x2, y2, 2, clr)
	}
}

func drawCloudIcon(img *image.RGBA, cx, cy, radius int, clr color.Color) {
	drawCircleArcStroke(img, cx-radius/2, cy+1, radius/2, 0.95*math.Pi, 1.9*math.Pi, 2, clr)
	drawCircleArcStroke(img, cx, cy-radius/4, radius/2, math.Pi, 2*math.Pi, 2, clr)
	drawCircleArcStroke(img, cx+radius/2, cy+1, radius/2, 1.1*math.Pi, 2*math.Pi, 2, clr)
	drawStrokeLine(img, cx-radius+2, cy+radius/2, cx+radius-2, cy+radius/2, 2, clr)
}

func drawSunCloudIcon(img *image.RGBA, cx, cy, radius int, clr color.Color) {
	drawSunIcon(img, cx-radius/2, cy-radius/2, max(6, radius/2), clr)
	drawCloudIcon(img, cx+4, cy+8, radius, clr)
}

func drawCloudRainIcon(img *image.RGBA, cx, cy, radius int, clr color.Color) {
	drawCloudIcon(img, cx, cy, radius, clr)
	drawStrokeLine(img, cx-radius/2, cy+radius, cx-radius/2-2, cy+radius+10, 2, clr)
	drawStrokeLine(img, cx, cy+radius+2, cx-2, cy+radius+12, 2, clr)
	drawStrokeLine(img, cx+radius/2, cy+radius, cx+radius/2-2, cy+radius+10, 2, clr)
}

func drawCloudSnowIcon(img *image.RGBA, cx, cy, radius int, clr color.Color) {
	drawCloudIcon(img, cx, cy, radius, clr)
	drawSnowFlake(img, cx-radius/2, cy+radius+10, 4, clr)
	drawSnowFlake(img, cx, cy+radius+14, 4, clr)
	drawSnowFlake(img, cx+radius/2, cy+radius+10, 4, clr)
}

func drawCloudLightningIcon(img *image.RGBA, cx, cy, radius int, clr color.Color) {
	drawCloudIcon(img, cx, cy, radius, clr)
	drawStrokeLine(img, cx+2, cy+radius-2, cx-4, cy+radius+10, 2, clr)
	drawStrokeLine(img, cx-4, cy+radius+10, cx+4, cy+radius+10, 2, clr)
	drawStrokeLine(img, cx+4, cy+radius+10, cx-2, cy+radius+22, 2, clr)
}

func drawCloudFogIcon(img *image.RGBA, cx, cy, radius int, clr color.Color) {
	drawCloudIcon(img, cx, cy, radius, clr)
	drawStrokeLine(img, cx-radius, cy+radius+6, cx+radius, cy+radius+6, 2, clr)
	drawStrokeLine(img, cx-radius+4, cy+radius+12, cx+radius-4, cy+radius+12, 2, clr)
}

func drawStrokeLine(img *image.RGBA, x1, y1, x2, y2, width int, clr color.Color) {
	steps := max(abs(x2-x1), abs(y2-y1))
	if steps == 0 {
		fillCircle(img, x1, y1, max(1, width/2), clr)
		return
	}
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := int(math.Round(float64(x1) + float64(x2-x1)*t))
		y := int(math.Round(float64(y1) + float64(y2-y1)*t))
		fillCircle(img, x, y, max(1, width/2), clr)
	}
}

func drawCircleStroke(img *image.RGBA, cx, cy, radius, width int, clr color.Color) {
	inner := max(0, radius-width)
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx := x - cx
			dy := y - cy
			dist := dx*dx + dy*dy
			if dist <= radius*radius && dist >= inner*inner {
				img.Set(x, y, clr)
			}
		}
	}
}

func drawCircleArcStroke(img *image.RGBA, cx, cy, radius int, start, end float64, width int, clr color.Color) {
	if end < start {
		start, end = end, start
	}
	steps := max(12, int(float64(radius)*3))
	for i := 0; i < steps; i++ {
		t1 := start + (end-start)*float64(i)/float64(steps)
		t2 := start + (end-start)*float64(i+1)/float64(steps)
		x1 := cx + int(math.Round(math.Cos(t1)*float64(radius)))
		y1 := cy + int(math.Round(math.Sin(t1)*float64(radius)))
		x2 := cx + int(math.Round(math.Cos(t2)*float64(radius)))
		y2 := cy + int(math.Round(math.Sin(t2)*float64(radius)))
		drawStrokeLine(img, x1, y1, x2, y2, width, clr)
	}
}

func drawSnowFlake(img *image.RGBA, cx, cy, radius int, clr color.Color) {
	drawStrokeLine(img, cx-radius, cy, cx+radius, cy, 1, clr)
	drawStrokeLine(img, cx, cy-radius, cx, cy+radius, 1, clr)
	drawStrokeLine(img, cx-radius+1, cy-radius+1, cx+radius-1, cy+radius-1, 1, clr)
	drawStrokeLine(img, cx-radius+1, cy+radius-1, cx+radius-1, cy-radius+1, 1, clr)
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

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := dashboardHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("geocode status: %s", resp.Status)
	}
	var payload struct {
		Results []struct {
			Name      string  `json:"name"`
			Country   string  `json:"country"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Timezone  string  `json:"timezone"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
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

func fetchDashboardWeather(coords dashboardWeatherCoords) (dashboardWeatherState, error) {
	u, err := url.Parse(dashboardForecastURL)
	if err != nil {
		return dashboardWeatherState{}, err
	}
	q := u.Query()
	q.Set("latitude", fmt.Sprintf("%.4f", coords.Latitude))
	q.Set("longitude", fmt.Sprintf("%.4f", coords.Longitude))
	q.Set("current", "temperature_2m,weather_code")
	if coords.Timezone != "" {
		q.Set("timezone", coords.Timezone)
	} else {
		q.Set("timezone", "auto")
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return dashboardWeatherState{}, err
	}
	resp, err := dashboardHTTPClient.Do(req)
	if err != nil {
		return dashboardWeatherState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return dashboardWeatherState{}, fmt.Errorf("forecast status: %s", resp.Status)
	}
	var payload struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			Code        int     `json:"weather_code"`
		} `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return dashboardWeatherState{}, err
	}
	temp := payload.Current.Temperature
	return dashboardWeatherState{
		TempC: &temp,
		Code:  payload.Current.Code,
		Ready: true,
	}, nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
