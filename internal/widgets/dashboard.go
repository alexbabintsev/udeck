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

	mu           sync.RWMutex
	mode         int
	context      Context
	closed       bool
	weather      dashboardWeatherState
	coords       *dashboardWeatherCoords
	lastWeather  time.Time
	spotifyNow   spotifySnapshot
	lastFrameKey string
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
	if _, err := w.render(time.Now()); err != nil {
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
	if changed, err := w.render(time.Now()); err == nil && changed {
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
	_, _ = w.render(time.Now())
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
	mode := w.mode
	ctx := w.context
	w.mu.Unlock()
	if mode != dashboardModeSpotify || ctx.InFolder() {
		return
	}
	if changed, err := w.render(time.Now()); err == nil && changed {
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
		if changed, err := w.render(now); err == nil && changed {
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

	if changed, err := w.render(time.Now()); err == nil && changed {
		w.notify(w.id)
	}
}

func (w *WideDashboardWidget) render(now time.Time) (bool, error) {
	w.mu.Lock()
	mode := w.mode
	ctx := w.context
	weather := w.weather
	spotifyNow := w.spotifyNow
	frameKey := dashboardFrameKey(now, mode, ctx, weather, spotifyNow)
	if frameKey == w.lastFrameKey {
		w.mu.Unlock()
		return false, nil
	}
	w.mu.Unlock()

	img := image.NewRGBA(image.Rect(0, 0, icons.WideWidth, icons.IconHeight))
	switch {
	case ctx.InFolder():
		renderDashboardBack(img, ctx)
	case mode == dashboardModeSpotify:
		renderDashboardSpotify(img, spotifyNow, ctx)
	default:
		renderDashboardClock(img, now, weather, ctx)
	}

	changed, err := writePNGAtomicIfChanged(w.iconPath, img)
	if err != nil {
		return false, fmt.Errorf("write dashboard widget icon: %w", err)
	}
	w.mu.Lock()
	w.lastFrameKey = frameKey
	w.mu.Unlock()
	return changed, nil
}

func dashboardFrameKey(now time.Time, mode int, ctx Context, weather dashboardWeatherState, spotifyNow spotifySnapshot) string {
	if ctx.InFolder() {
		return fmt.Sprintf("back:%d:%d:%d", ctx.PageIndex, ctx.TotalPages, ctx.FolderDepth)
	}
	switch mode {
	case dashboardModeSpotify:
		sec := int(math.Floor(spotifyNow.PositionSec))
		if !spotifyNow.Playing {
			sec = int(math.Round(spotifyNow.PositionSec))
		}
		return fmt.Sprintf("spotify:%d:%t:%s:%s:%s:%s:%d:%d:%t", ctx.PageIndex, spotifyNow.Running, spotifyNow.State, spotifyNow.TrackID, spotifyNow.TrackName, spotifyNow.Album, sec, spotifyNow.DurationMS, spotifyNow.Artwork != nil)
	default:
		return fmt.Sprintf("clock:%s:%d", now.Format("2006-01-02 15:04"), ctx.PageIndex)
	}
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

func rectWH(x, y, w, h int) image.Rectangle {
	return image.Rect(x, y, x+w, y+h)
}

func rgb(hex string, alpha ...int) color.RGBA {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		return color.RGBA{}
	}
	var out uint32
	if _, err := fmt.Sscanf(hex, "%06x", &out); err != nil {
		return color.RGBA{}
	}
	a := 255
	if len(alpha) > 0 {
		a = alpha[0]
		if a < 0 {
			a = 0
		} else if a > 255 {
			a = 255
		}
	}
	return color.RGBA{
		R: uint8(out >> 16),
		G: uint8((out >> 8) & 0xff),
		B: uint8(out & 0xff),
		A: uint8(a),
	}
}

func renderDashboardClock(img *image.RGBA, now time.Time, weather dashboardWeatherState, ctx Context) {
	draw.Draw(img, img.Bounds(), &image.Uniform{rgb("000000")}, image.Point{}, draw.Src)
	draw.Draw(img, rectWH(icons.WideWidth-4, 0, 4, icons.IconHeight), &image.Uniform{rgb("086fff")}, image.Point{}, draw.Src)

	// big clock — left area
	drawSpaceMonoClockText(img, now.Format("15:04"), rectWH(24, 8, 310, 118), color.White, rgb("7a7a7a"), 100, -4)

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
	drawPageChip(img, ctx)
}

func renderDashboardSpotify(img *image.RGBA, s spotifySnapshot, ctx Context) {
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)

	artRect := rectWH(0, 0, 192, 192)
	drawDashboardArtwork(img, artRect, s)

	drawSpotifySourceRow(img, s)
	track := s.TrackName
	if track == "" {
		track = "Open Spotify"
	}
	artistLine := dashboardSpotifyArtistLine(s)
	drawDashboardLeftText(img, shortenLine(track, 22), rectWH(218, 64, 212, 28), color.White, 24)
	drawDashboardLeftText(img, shortenLine(artistLine, 26), rectWH(218, 94, 224, 24), color.RGBA{0xbb, 0xbb, 0xbb, 0xff}, 18)
	drawSpotifyProgressBlock(img, s)
	drawPageChip(img, ctx)
}

func renderDashboardBack(img *image.RGBA, ctx Context) {
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)
	drawIcon(img, "lucide/arrow-left", CenterX(-80), CenterY(0), 70, 70, rgb("aaaaaa"))
	drawText(img, "BACK", CenterX(40), CenterY(0), 140, 48, AlignMiddleLeft, color.White, 48, true)
	drawPageChip(img, ctx)
}

func drawPageChip(img *image.RGBA, ctx Context) {
	if ctx.PageIndex <= 0 {
		return
	}
	rect := rectWH(402, 14, 42, 22)
	fillRoundRect(img, rect, 10, rgb("000000", 136))
	drawDashboardCenteredText(img, fmt.Sprintf("%d", ctx.PageIndex+1), rect, color.White, 14)
}

func drawDashboardArtwork(img *image.RGBA, rect image.Rectangle, s spotifySnapshot) {
	fillRoundRect(img, rect, 18, rgb("181818"))
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

func drawSpaceMonoLeftText(img *image.RGBA, text string, rect image.Rectangle, clr color.Color, size float64, bold bool) {
	face, display := spaceMonoFaceForText(text, rect.Dx(), size, bold)
	if face == nil {
		drawDashboardLeftText(img, display, rect, clr, size)
		return
	}
	baseline := rect.Min.Y + (rect.Dy()-face.Metrics().Height.Ceil())/2 + face.Metrics().Ascent.Ceil()
	drawLeftFace(img, display, rect.Min.X, baseline, clr, face)
}

func drawSpaceMonoLeftTextTracked(img *image.RGBA, text string, rect image.Rectangle, clr color.Color, size float64, bold bool, tracking float64) {
	face, display := spaceMonoFaceForText(text, rect.Dx(), size, bold)
	if face == nil {
		drawSpaceMonoLeftText(img, display, rect, clr, size, bold)
		return
	}
	baseline := rect.Min.Y + (rect.Dy()-face.Metrics().Height.Ceil())/2 + face.Metrics().Ascent.Ceil()
	drawLeftFaceTracked(img, display, rect.Min.X, baseline, clr, face, tracking)
}

func drawSpaceMonoRightTextTracked(img *image.RGBA, text string, rect image.Rectangle, clr color.Color, size float64, bold bool, tracking float64) {
	face, display := spaceMonoFaceForText(text, rect.Dx(), size, bold)
	if face == nil {
		drawSpaceMonoLeftTextTracked(img, display, rect, clr, size, bold, tracking)
		return
	}
	baseline := rect.Min.Y + (rect.Dy()-face.Metrics().Height.Ceil())/2 + face.Metrics().Ascent.Ceil()
	x := rect.Max.X - trackedTextWidth(face, display, tracking)
	drawLeftFaceTracked(img, display, x, baseline, clr, face, tracking)
}

func drawSpaceMonoClockText(img *image.RGBA, text string, rect image.Rectangle, mainClr, colonClr color.Color, size float64, tracking float64) {
	if len(text) != 5 || text[2] != ':' {
		drawSpaceMonoLeftTextTracked(img, text, rect, mainClr, size, true, tracking)
		return
	}
	face, _ := spaceMonoFaceForText(text, rect.Dx(), size, true)
	if face == nil {
		drawSpaceMonoLeftTextTracked(img, text, rect, mainClr, size, true, tracking)
		return
	}
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

func drawDashboardClockIcon(img *image.RGBA, cx, cy, radius int, clr color.Color) {
	drawCircleStroke(img, cx, cy, radius, 4, clr)
	drawStrokeLine(img, cx, cy, cx, cy-12, 4, clr)
	drawStrokeLine(img, cx, cy, cx+10, cy+6, 4, clr)
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
