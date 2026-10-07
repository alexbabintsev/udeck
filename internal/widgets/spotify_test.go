package widgets

import (
	"bytes"
	"context"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"testing"

	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/icons"
)

func TestParseSpotifyState(t *testing.T) {
	got := parseSpotifyState("1||playing||spotify:track:123||Song Name||Album Name||Artist Name||84.5||200000")
	if !got.Running {
		t.Fatal("expected running=true")
	}
	if got.TrackID != "spotify:track:123" {
		t.Fatalf("unexpected track id: %q", got.TrackID)
	}
	if !got.Playing {
		t.Fatal("expected playing=true")
	}
	if got.TrackName != "Song Name" {
		t.Fatalf("unexpected track name: %q", got.TrackName)
	}
	if got.Album != "Album Name" {
		t.Fatalf("unexpected album: %q", got.Album)
	}
	if got.Artist != "Artist Name" {
		t.Fatalf("unexpected artist: %q", got.Artist)
	}
	if got.PositionSec != 84.5 {
		t.Fatalf("unexpected position: %v", got.PositionSec)
	}
	if got.DurationMS != 200000 {
		t.Fatalf("unexpected duration: %d", got.DurationMS)
	}
}

func TestParseSpotifyStateEmpty(t *testing.T) {
	got := parseSpotifyState("")
	if got.Running || got.TrackID != "" || got.TrackName != "" || got.Album != "" || got.Artist != "" || got.Playing || got.PositionSec != 0 || got.DurationMS != 0 {
		t.Fatalf("expected zero snapshot, got %+v", got)
	}
}

func TestParseSpotifyStateNotRunning(t *testing.T) {
	got := parseSpotifyState("0|||||||")
	if got.Running {
		t.Fatal("expected running=false")
	}
	if got.State != "" || got.TrackID != "" || got.TrackName != "" || got.Album != "" || got.Artist != "" || got.PositionSec != 0 || got.DurationMS != 0 {
		t.Fatalf("expected empty metadata, got %+v", got)
	}
}

func TestParseSpotifyArtworkURL(t *testing.T) {
	got, err := parseSpotifyArtworkURL([]byte(`{"thumbnail_url":"https://i.scdn.co/image/abc123"}`))
	if err != nil {
		t.Fatalf("parse artwork url: %v", err)
	}
	if got != "https://i.scdn.co/image/abc123" {
		t.Fatalf("unexpected artwork url: %q", got)
	}
}

func TestShortenLine(t *testing.T) {
	got := shortenLine("abcdefghijklmnopqrstuvwxyz", 6)
	if got != "abcde…" {
		t.Fatalf("unexpected shortened line: %q", got)
	}
}

func TestSpotifyPlayPauseWidgetTapRunsPlayPause(t *testing.T) {
	oldExec := execCommandContext
	defer func() { execCommandContext = oldExec }()

	var gotArgs []string
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotArgs = append([]string{name}, args...)
		return exec.CommandContext(ctx, "/usr/bin/true")
	}

	base, err := newSpotifyBaseWidget("test", &SpotifyService{}, func(string) {}, "spotify-playpause.png")
	if err != nil {
		t.Fatalf("new spotify base widget: %v", err)
	}
	defer base.Close()

	w := &SpotifyPlayPauseWidget{spotifyBaseWidget: base}
	w.HandleGesture(ig.Tap)

	if len(gotArgs) < 3 || gotArgs[0] != "/usr/bin/osascript" || gotArgs[1] != "-e" || gotArgs[2] != `tell application "Spotify" to playpause` {
		t.Fatalf("unexpected playpause command: %#v", gotArgs)
	}
}

func TestSpotifyPlayPauseWidgetHoldRunsNextTrack(t *testing.T) {
	oldExec := execCommandContext
	defer func() { execCommandContext = oldExec }()

	var gotArgs []string
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotArgs = append([]string{name}, args...)
		return exec.CommandContext(ctx, "/usr/bin/true")
	}

	base, err := newSpotifyBaseWidget("test", &SpotifyService{}, func(string) {}, "spotify-playpause.png")
	if err != nil {
		t.Fatalf("new spotify base widget: %v", err)
	}
	defer base.Close()

	w := &SpotifyPlayPauseWidget{spotifyBaseWidget: base}
	w.HandleGesture(ig.Hold)

	if len(gotArgs) < 3 || gotArgs[0] != "/usr/bin/osascript" || gotArgs[1] != "-e" || gotArgs[2] != `tell application "Spotify" to next track` {
		t.Fatalf("unexpected next-track command: %#v", gotArgs)
	}
}

func TestSpotifyPlayPauseWidgetRendersMinimalIcon(t *testing.T) {
	base, err := newSpotifyBaseWidget("test", &SpotifyService{}, func(string) {}, "spotify-playpause.png")
	if err != nil {
		t.Fatalf("new spotify base widget: %v", err)
	}
	defer base.Close()

	w := &SpotifyPlayPauseWidget{spotifyBaseWidget: base}
	w.spotifyUpdate(spotifySnapshot{Running: true, Playing: true})

	data, err := os.ReadFile(base.iconPath)
	if err != nil {
		t.Fatalf("read rendered icon: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode rendered icon: %v", err)
	}

	bg := color.RGBAModel.Convert(img.At(8, 8)).(color.RGBA)
	if bg.R > 4 || bg.G > 4 || bg.B > 4 {
		t.Fatalf("expected black background, got %#v", bg)
	}

	foundGreen := false
	for y := 24; y < 132 && !foundGreen; y++ {
		for x := 40; x < 156; x++ {
			px := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if px.G > 140 && px.G > px.R+40 && px.G > px.B+40 {
				foundGreen = true
				break
			}
		}
	}
	if !foundGreen {
		t.Fatal("expected green lucide icon pixels")
	}

	bottom := color.RGBAModel.Convert(img.At(icons.IconWidth/2, 176)).(color.RGBA)
	if bottom.R > 4 || bottom.G > 4 || bottom.B > 4 {
		t.Fatalf("expected no bottom label, got %#v", bottom)
	}
}

func TestNewSpotifyPlayPauseWidgetWritesInitialIcon(t *testing.T) {
	svc := &SpotifyService{}
	svc.cancel = func() {}

	w, err := NewSpotifyPlayPauseWidget("test", svc, func(string) {})
	if err != nil {
		t.Fatalf("new spotify play/pause widget: %v", err)
	}
	defer w.Close()

	data, err := os.ReadFile(w.IconPath())
	if err != nil {
		t.Fatalf("read initial icon: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode initial icon: %v", err)
	}

	foundGreen := false
	for y := 34; y < 162 && !foundGreen; y++ {
		for x := 34; x < 162; x++ {
			px := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if px.G > 140 && px.G > px.R+40 && px.G > px.B+40 {
				foundGreen = true
				break
			}
		}
	}
	if !foundGreen {
		t.Fatal("expected initial icon render to contain green play glyph")
	}
}
