package widgets

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	ig "github.com/alexbabintsev/udeck/internal/gestures"
)

type subscriberFunc func(spotifySnapshot)

func (f subscriberFunc) spotifyUpdate(s spotifySnapshot) { f(s) }
func (f subscriberFunc) isActive() bool                  { return true }

// fakeOSA makes osascript print state for every call.
func fakeOSA(t *testing.T, state *atomic.Value) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	old := execCommandContext
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls.Add(1)
		return exec.CommandContext(ctx, "printf", "%s", state.Load().(string))
	}
	t.Cleanup(func() { execCommandContext = old })
	return &calls
}

func spotifyServer(t *testing.T) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/oembed":
			fmt.Fprintf(w, `{"thumbnail_url": "%s/art.png"}`, srv.URL)
		case "/art.png":
			img := image.NewRGBA(image.Rect(0, 0, 8, 8))
			img.Set(1, 1, color.White)
			_ = png.Encode(w, img)
		case "/huge.png":
			// Valid header claiming an enormous image.
			_ = png.Encode(w, image.NewGray(image.Rect(0, 0, 5000, 5000)))
		}
	}))
	t.Cleanup(srv.Close)
	old := spotifyOEmbedURL
	spotifyOEmbedURL = srv.URL + "/oembed"
	t.Cleanup(func() { spotifyOEmbedURL = old })
	return &hits
}

func TestSpotifyServiceRefreshFetchesArtworkOncePerTrack(t *testing.T) {
	hits := spotifyServer(t)
	var state atomic.Value
	state.Store("1||playing||spotify:track:abc||Song||Album||Artist||12.5||200000")
	fakeOSA(t, &state)

	s := &SpotifyService{}
	var mu sync.Mutex
	var got []spotifySnapshot
	s.Subscribe(subscriberFunc(func(snap spotifySnapshot) {
		mu.Lock()
		got = append(got, snap)
		mu.Unlock()
	}))

	s.refresh()
	if s.snapshot.Artwork == nil || hits.Load() != 2 {
		t.Fatalf("artwork=%v hits=%d", s.snapshot.Artwork != nil, hits.Load())
	}
	s.refresh() // unchanged: no update, no fetch
	state.Store("1||paused||spotify:track:abc||Song||Album||Artist||13||200000")
	s.refresh()
	if hits.Load() != 2 || s.snapshot.Artwork == nil {
		t.Fatal("same track must reuse the artwork")
	}
	state.Store("0|||||||")
	s.refresh()
	mu.Lock()
	n := len(got)
	mu.Unlock()
	if n != 4 || s.snapshot.Artwork != nil {
		t.Fatalf("updates=%d artwork=%v", n, s.snapshot.Artwork != nil)
	}
}

func TestSpotifyServiceLoopsAndControls(t *testing.T) {
	var state atomic.Value
	state.Store("0|||||||")
	calls := fakeOSA(t, &state)
	s, err := NewSpotifyService(1)
	if err != nil {
		t.Fatal(err)
	}
	s.Subscribe(&activeSub{active: true})
	waitFor(t, "polls", func() bool { return calls.Load() > 2 })
	s.PlayPause()
	s.NextTrack()
	if err := s.Close(); err != nil { // waits for both commands
		t.Fatal(err)
	}
}

func TestFetchSpotifyArtworkRejectsHugeImages(t *testing.T) {
	spotifyServer(t)
	srvURL := strings.TrimSuffix(spotifyOEmbedURL, "/oembed")
	if img := fetchSpotifyArtwork(""); img != nil {
		t.Fatal("empty track must not fetch")
	}
	old := spotifyOEmbedURL
	defer func() { spotifyOEmbedURL = old }()
	// oEmbed pointing at a decompression bomb.
	bomb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"thumbnail_url": "%s/huge.png"}`, srvURL)
	}))
	defer bomb.Close()
	spotifyOEmbedURL = bomb.URL
	if img := fetchSpotifyArtwork("spotify:track:x"); img != nil {
		t.Fatal("oversized artwork must be rejected before decoding")
	}
}

func TestSpotifyWidgetsRender(t *testing.T) {
	svc := &SpotifyService{}
	rec := &recorder{}
	next, err := NewSpotifyNextWidget("n", svc, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	art, err := NewSpotifyArtworkWidget("a", svc, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	defer art.Close()
	for _, s := range []spotifySnapshot{
		{},
		{Running: true, State: "paused", TrackName: "T", Artist: "A"},
		{Running: true, State: "stopped"},
		{Running: true, State: "playing", Playing: true, TrackName: "T", Artist: "A", Artwork: image.NewRGBA(image.Rect(0, 0, 32, 32))},
	} {
		next.spotifyUpdate(s)
		art.spotifyUpdate(s)
	}
	if rec.count() < 4 {
		t.Fatalf("expected re-renders, got %d", rec.count())
	}
	art.HandleGesture(ig.Tap)

	var state atomic.Value
	state.Store("")
	calls := fakeOSA(t, &state)
	next.HandleGesture(ig.Tap)
	next.HandleGesture(ig.Hold)
	svc.bg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("next tap must run one osascript, got %d", calls.Load())
	}
}

func TestSpotifyTextHelpers(t *testing.T) {
	if spotifyChanged(spotifySnapshot{}, spotifySnapshot{}) || !spotifyChanged(spotifySnapshot{}, spotifySnapshot{Artwork: image.NewRGBA(image.Rect(0, 0, 1, 1))}) {
		t.Fatal("spotifyChanged")
	}
	if parseSpotifyFloat("1,5") != 1.5 || parseSpotifyFloat(" 2.25 ") != 2.25 || parseSpotifyFloat("x") != 0 || parseSpotifyInt("x") != 0 {
		t.Fatal("parse helpers")
	}
	if pollDurationOrDefault(0, 7).Milliseconds() != 7 || pollDurationOrDefault(3, 7).Milliseconds() != 3 {
		t.Fatal("pollDurationOrDefault")
	}
	for _, s := range []spotifySnapshot{{}, {Running: true}, {Running: true, State: "paused"}, {Running: true, State: "playing", Playing: true, TrackName: "T", Artist: "A"}} {
		_ = spotifyArtworkTitle(s)
		_ = spotifyStatusChip(s)
		_ = spotifyAccent(s)
		_ = trackLine(s)
		_ = displayTrackTitle(s)
		_ = displayArtist(s)
	}
	if shortenLine("ab", 5) != "ab" || shortenLine("abc", 1) != "…" && shortenLine("abc", 1) != "a" {
		t.Fatal("shortenLine")
	}
}
