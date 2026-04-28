package widgets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font/basicfont"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
)

const (
	defaultSpotifyPollMS    = 2000
	defaultSpotifyHTTPTO    = 8 * time.Second
	spotifyFallbackName     = "Spotify"
	spotifyNotifierDebounce = 250 * time.Millisecond
)

var (
	spotifyExecCommand = exec.Command
	spotifyHTTPClient  = &http.Client{Timeout: defaultSpotifyHTTPTO}
)

type spotifySnapshot struct {
	Running   bool
	State     string
	TrackID   string
	TrackName string
	Artist    string
	Playing   bool
	Artwork   image.Image
}

type spotifySubscriber interface {
	spotifyUpdate(spotifySnapshot)
}

type SpotifyService struct {
	mu          sync.RWMutex
	snapshot    spotifySnapshot
	subscribers []spotifySubscriber
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	requestCh   chan struct{}
}

func NewSpotifyService(pollMS int) (*SpotifyService, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &SpotifyService{
		cancel:    cancel,
		requestCh: make(chan struct{}, 1),
	}

	s.wg.Add(1)
	go s.refreshLoop(ctx, pollDurationOrDefault(pollMS, defaultSpotifyPollMS))

	s.wg.Add(1)
	go s.notifierLoop(ctx)

	s.requestRefresh()
	return s, nil
}

func (s *SpotifyService) Close() error {
	s.cancel()
	s.wg.Wait()
	return nil
}

func (s *SpotifyService) Subscribe(sub spotifySubscriber) {
	s.mu.Lock()
	s.subscribers = append(s.subscribers, sub)
	snapshot := s.snapshot
	s.mu.Unlock()
	sub.spotifyUpdate(snapshot)
}

func (s *SpotifyService) requestRefresh() {
	select {
	case s.requestCh <- struct{}{}:
	default:
	}
}

func (s *SpotifyService) PlayPause() {
	if err := spotifyOSA(`tell application "Spotify" to playpause`); err == nil {
		s.requestRefresh()
	}
}

func (s *SpotifyService) NextTrack() {
	if err := spotifyOSA(`tell application "Spotify" to next track`); err == nil {
		s.requestRefresh()
	}
}

func (s *SpotifyService) refreshLoop(ctx context.Context, poll time.Duration) {
	defer s.wg.Done()

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refresh()
		case <-s.requestCh:
			s.refresh()
		}
	}
}

func (s *SpotifyService) notifierLoop(ctx context.Context) {
	defer s.wg.Done()

	events, err := startSpotifyNotifier(ctx)
	if err != nil {
		return
	}

	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-events:
			if !ok {
				return
			}
			now := time.Now()
			if now.Sub(last) < spotifyNotifierDebounce {
				continue
			}
			last = now
			s.requestRefresh()
		}
	}
}

func (s *SpotifyService) refresh() {
	next, err := fetchSpotifySnapshot()
	if err != nil {
		return
	}

	s.mu.Lock()
	prev := s.snapshot
	trackChanged := next.TrackID != prev.TrackID
	if trackChanged && next.TrackID != "" {
		next.Artwork = fetchSpotifyArtwork(next.TrackID)
	} else if next.TrackID == "" {
		next.Artwork = nil
	} else {
		next.Artwork = prev.Artwork
	}
	changed := spotifyChanged(prev, next)
	if !changed {
		s.mu.Unlock()
		return
	}
	s.snapshot = next
	subs := append([]spotifySubscriber(nil), s.subscribers...)
	s.mu.Unlock()

	for _, sub := range subs {
		sub.spotifyUpdate(next)
	}
}

func spotifyChanged(a, b spotifySnapshot) bool {
	if a.Running != b.Running || a.State != b.State || a.TrackID != b.TrackID || a.TrackName != b.TrackName || a.Artist != b.Artist || a.Playing != b.Playing {
		return true
	}
	return (a.Artwork == nil) != (b.Artwork == nil)
}

func fetchSpotifySnapshot() (spotifySnapshot, error) {
	out, err := spotifyExecOut("/usr/bin/osascript", []string{"-e", spotifyStateScript()}, 4*time.Second)
	if err != nil {
		return spotifySnapshot{}, err
	}
	return parseSpotifyState(out), nil
}

func spotifyStateScript() string {
	return `if application "Spotify" is running then
tell application "Spotify"
return ("1||" & (player state as string) & "||" & (id of current track) & "||" & (name of current track) & "||" & (artist of current track))
end tell
else
return "0||||"
end if`
}

func parseSpotifyState(out string) spotifySnapshot {
	out = strings.TrimSpace(out)
	if out == "" {
		return spotifySnapshot{}
	}
	parts := strings.SplitN(out, "||", 5)
	if len(parts) != 5 {
		return spotifySnapshot{}
	}
	state := strings.TrimSpace(parts[1])
	return spotifySnapshot{
		Running:   strings.TrimSpace(parts[0]) == "1",
		State:     state,
		TrackID:   strings.TrimSpace(parts[2]),
		Playing:   state == "playing",
		TrackName: strings.TrimSpace(parts[3]),
		Artist:    strings.TrimSpace(parts[4]),
	}
}

func fetchSpotifyArtwork(trackID string) image.Image {
	artworkURL, err := fetchSpotifyArtworkURL(trackID)
	if err != nil || artworkURL == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodGet, artworkURL, nil)
	if err != nil {
		return nil
	}
	resp, err := spotifyHTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	return img
}

func fetchSpotifyArtworkURL(trackID string) (string, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(trackID), "spotify:track:")
	if trimmed == "" {
		return "", nil
	}
	u := "https://open.spotify.com/oembed?url=" + url.QueryEscape("spotify:track:"+trimmed)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := spotifyHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("oembed status: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return parseSpotifyArtworkURL(body)
}

func parseSpotifyArtworkURL(body []byte) (string, error) {
	var resp struct {
		ThumbnailURL string `json:"thumbnail_url"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	return resp.ThumbnailURL, nil
}

type spotifyBaseWidget struct {
	id       string
	service  *SpotifyService
	notify   func(string)
	iconPath string
	mu       sync.Mutex
	closed   bool
}

func newSpotifyBaseWidget(id string, service *SpotifyService, notify func(string), filename string) (*spotifyBaseWidget, error) {
	dir, err := os.MkdirTemp("", "ulanzi-spotify-widget-")
	if err != nil {
		return nil, fmt.Errorf("create spotify widget temp dir: %w", err)
	}
	return &spotifyBaseWidget{
		id:       id,
		service:  service,
		notify:   notify,
		iconPath: filepath.Join(dir, filename),
	}, nil
}

func (w *spotifyBaseWidget) IconPath() string { return w.iconPath }

func (w *spotifyBaseWidget) Close() error {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	return os.RemoveAll(filepath.Dir(w.iconPath))
}

func (w *spotifyBaseWidget) writeImage(img image.Image) error {
	if err := writePNGAtomic(w.iconPath, img); err != nil {
		return fmt.Errorf("write spotify widget icon: %w", err)
	}
	return nil
}

type SpotifyPlayPauseWidget struct {
	*spotifyBaseWidget
}

func NewSpotifyPlayPauseWidget(id string, service *SpotifyService, notify func(string)) (*SpotifyPlayPauseWidget, error) {
	base, err := newSpotifyBaseWidget(id, service, notify, "spotify-playpause.png")
	if err != nil {
		return nil, err
	}
	w := &SpotifyPlayPauseWidget{spotifyBaseWidget: base}
	service.Subscribe(w)
	return w, nil
}

func (w *SpotifyPlayPauseWidget) Press() { w.service.PlayPause() }

func (w *SpotifyPlayPauseWidget) spotifyUpdate(s spotifySnapshot) {
	img := image.NewRGBA(image.Rect(0, 0, icons.IconWidth, icons.IconHeight))
	drawWidgetGradient(img, color.RGBA{0x08, 0x13, 0x0d, 0xff}, color.RGBA{0x11, 0x25, 0x17, 0xff})
	accent := spotifyAccent(s)
	drawSmallBadge(img, spotifyStatusChip(s), accent)
	drawScaledCenteredText(img, spotifyPlayPauseTitle(s), image.Rect(18, 34, icons.IconWidth-18, 88), accent)
	drawPlayPauseGlyph(img, s.Playing, accent)
	drawCenteredFace(img, shortenLine(trackLine(s), 15), icons.IconWidth/2, icons.IconHeight-18, color.RGBA{0xc0, 0xc0, 0xc0, 0xff}, basicfont.Face7x13)
	if err := w.writeImage(img); err == nil {
		w.notify(w.id)
	}
}

type SpotifyNextWidget struct {
	*spotifyBaseWidget
}

func NewSpotifyNextWidget(id string, service *SpotifyService, notify func(string)) (*SpotifyNextWidget, error) {
	base, err := newSpotifyBaseWidget(id, service, notify, "spotify-next.png")
	if err != nil {
		return nil, err
	}
	w := &SpotifyNextWidget{spotifyBaseWidget: base}
	service.Subscribe(w)
	return w, nil
}

func (w *SpotifyNextWidget) Press() { w.service.NextTrack() }

func (w *SpotifyNextWidget) spotifyUpdate(s spotifySnapshot) {
	img := image.NewRGBA(image.Rect(0, 0, icons.IconWidth, icons.IconHeight))
	drawWidgetGradient(img, color.RGBA{0x10, 0x10, 0x10, 0xff}, color.RGBA{0x18, 0x18, 0x18, 0xff})
	accent := spotifyAccent(s)
	drawSmallBadge(img, spotifyStatusChip(s), accent)
	drawScaledCenteredText(img, "NEXT", image.Rect(18, 28, icons.IconWidth-18, 78), accent)
	drawNextGlyph(img, accent)
	drawCenteredFace(img, shortenLine(trackLine(s), 15), icons.IconWidth/2, icons.IconHeight-18, color.RGBA{0xc0, 0xc0, 0xc0, 0xff}, basicfont.Face7x13)
	if err := w.writeImage(img); err == nil {
		w.notify(w.id)
	}
}

type SpotifyArtworkWidget struct {
	*spotifyBaseWidget
}

func NewSpotifyArtworkWidget(id string, service *SpotifyService, notify func(string)) (*SpotifyArtworkWidget, error) {
	base, err := newSpotifyBaseWidget(id, service, notify, "spotify-artwork.png")
	if err != nil {
		return nil, err
	}
	w := &SpotifyArtworkWidget{spotifyBaseWidget: base}
	service.Subscribe(w)
	return w, nil
}

func (w *SpotifyArtworkWidget) Press() {}

func (w *SpotifyArtworkWidget) spotifyUpdate(s spotifySnapshot) {
	img := image.NewRGBA(image.Rect(0, 0, icons.IconWidth, icons.IconHeight))
	if s.Artwork != nil {
		xdraw.BiLinear.Scale(img, img.Bounds(), s.Artwork, s.Artwork.Bounds(), draw.Over, nil)
		drawTopScrim(img)
		drawBottomScrim(img)
		drawPlaybackBadge(img, s)
	} else {
		drawWidgetGradient(img, color.RGBA{0x12, 0x12, 0x12, 0xff}, color.RGBA{0x0d, 0x19, 0x10, 0xff})
		drawSmallBadge(img, spotifyStatusChip(s), spotifyAccent(s))
		drawScaledCenteredText(img, spotifyArtworkTitle(s), image.Rect(20, 38, icons.IconWidth-20, 98), spotifyAccent(s))
	}
	drawCenteredFace(img, shortenLine(displayTrackTitle(s), 18), icons.IconWidth/2, icons.IconHeight-30, color.RGBA{0xff, 0xff, 0xff, 0xff}, basicfont.Face7x13)
	drawCenteredFace(img, shortenLine(displayArtist(s), 18), icons.IconWidth/2, icons.IconHeight-14, color.RGBA{0xd0, 0xd0, 0xd0, 0xff}, basicfont.Face7x13)
	if err := w.writeImage(img); err == nil {
		w.notify(w.id)
	}
}

func spotifyPlayPauseTitle(s spotifySnapshot) string {
	if !s.Running {
		return "OPEN"
	}
	if s.Playing {
		return "PAUSE"
	}
	return "PLAY"
}

func spotifyArtworkTitle(s spotifySnapshot) string {
	if !s.Running {
		return "SPOTIFY"
	}
	switch s.State {
	case "paused":
		return "PAUSED"
	case "stopped":
		return "READY"
	default:
		return "NOW"
	}
}

func spotifyStatusChip(s spotifySnapshot) string {
	if !s.Running {
		return "OFF"
	}
	switch s.State {
	case "playing":
		return "LIVE"
	case "paused":
		return "PAUSED"
	default:
		return "READY"
	}
}

func spotifyAccent(s spotifySnapshot) color.RGBA {
	if !s.Running {
		return color.RGBA{0xb8, 0xbc, 0xc3, 0xff}
	}
	if s.Playing {
		return color.RGBA{0x1d, 0xb9, 0x54, 0xff}
	}
	return color.RGBA{0xe8, 0xe8, 0xe8, 0xff}
}

func trackLine(s spotifySnapshot) string {
	if s.TrackName != "" {
		return s.TrackName
	}
	if s.TrackID != "" {
		return "READY"
	}
	if s.Running {
		return "OPEN APP"
	}
	return spotifyFallbackName
}

func displayTrackTitle(s spotifySnapshot) string {
	if s.TrackName != "" {
		return s.TrackName
	}
	if !s.Running {
		return spotifyFallbackName
	}
	if s.State == "stopped" {
		return "Nothing Playing"
	}
	return spotifyFallbackName
}

func displayArtist(s spotifySnapshot) string {
	if s.Artist != "" {
		return s.Artist
	}
	if !s.Running {
		return "Not Running"
	}
	switch s.State {
	case "playing":
		return "Playing"
	case "paused":
		return "Paused"
	case "stopped":
		return "Ready"
	default:
		return "Open App"
	}
}

func shortenLine(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func drawPlayPauseGlyph(img *image.RGBA, playing bool, clr color.Color) {
	if playing {
		draw.Draw(img, image.Rect(62, 88, 82, 138), &image.Uniform{clr}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(114, 88, 134, 138), &image.Uniform{clr}, image.Point{}, draw.Src)
		return
	}
	for x := 0; x < 52; x++ {
		top := 88 + x/2
		bottom := 138 - x/2
		draw.Draw(img, image.Rect(74+x, top, 76+x, bottom), &image.Uniform{clr}, image.Point{}, draw.Src)
	}
}

func drawNextGlyph(img *image.RGBA, clr color.Color) {
	for x := 0; x < 38; x++ {
		top := 88 + x/2
		bottom := 138 - x/2
		draw.Draw(img, image.Rect(42+x, top, 44+x, bottom), &image.Uniform{clr}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(86+x, top, 88+x, bottom), &image.Uniform{clr}, image.Point{}, draw.Src)
	}
	draw.Draw(img, image.Rect(138, 88, 152, 138), &image.Uniform{clr}, image.Point{}, draw.Src)
}

func drawWidgetGradient(img *image.RGBA, top, bottom color.RGBA) {
	for y := 0; y < icons.IconHeight; y++ {
		ratio := float64(y) / float64(icons.IconHeight-1)
		c := color.RGBA{
			R: uint8(float64(top.R)*(1-ratio) + float64(bottom.R)*ratio),
			G: uint8(float64(top.G)*(1-ratio) + float64(bottom.G)*ratio),
			B: uint8(float64(top.B)*(1-ratio) + float64(bottom.B)*ratio),
			A: 0xff,
		}
		draw.Draw(img, image.Rect(0, y, icons.IconWidth, y+1), &image.Uniform{c}, image.Point{}, draw.Src)
	}
}

func drawSmallBadge(img *image.RGBA, text string, clr color.Color) {
	draw.Draw(img, image.Rect(14, 12, 78, 28), &image.Uniform{color.RGBA{0, 0, 0, 0xaa}}, image.Point{}, draw.Over)
	drawCenteredFace(img, text, 46, 24, clr, basicfont.Face7x13)
}

func drawTopScrim(img *image.RGBA) {
	for y := 0; y < 42; y++ {
		alpha := uint8((42 - y) * 120 / 42)
		draw.Draw(img, image.Rect(0, y, icons.IconWidth, y+1), &image.Uniform{color.RGBA{0, 0, 0, alpha}}, image.Point{}, draw.Over)
	}
}

func drawPlaybackBadge(img *image.RGBA, s spotifySnapshot) {
	accent := spotifyAccent(s)
	draw.Draw(img, image.Rect(146, 12, 182, 34), &image.Uniform{color.RGBA{0, 0, 0, 0x96}}, image.Point{}, draw.Over)
	if s.Playing {
		draw.Draw(img, image.Rect(157, 16, 162, 30), &image.Uniform{accent}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(166, 16, 171, 30), &image.Uniform{accent}, image.Point{}, draw.Src)
		return
	}
	for x := 0; x < 11; x++ {
		top := 16 + x/2
		bottom := 30 - x/2
		draw.Draw(img, image.Rect(156+x, top, 157+x, bottom), &image.Uniform{accent}, image.Point{}, draw.Src)
	}
}

func drawBottomScrim(img *image.RGBA) {
	for y := icons.IconHeight - 56; y < icons.IconHeight; y++ {
		alpha := uint8((y - (icons.IconHeight - 56)) * 160 / 56)
		draw.Draw(img, image.Rect(0, y, icons.IconWidth, y+1), &image.Uniform{color.RGBA{0, 0, 0, alpha}}, image.Point{}, draw.Over)
	}
}

func spotifyExecOut(cmd string, args []string, timeout time.Duration) (string, error) {
	c := spotifyExecCommand(cmd, args...)
	c.Env = os.Environ()
	_ = timeout
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func spotifyOSA(script string) error {
	_, err := spotifyExecCommand("/usr/bin/osascript", "-e", script).Output()
	return err
}

func pollDurationOrDefault(ms int, def int) time.Duration {
	if ms <= 0 {
		return time.Duration(def) * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}
