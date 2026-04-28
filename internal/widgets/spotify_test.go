package widgets

import "testing"

func TestParseSpotifyState(t *testing.T) {
	got := parseSpotifyState("1||playing||spotify:track:123||Song Name||Artist Name")
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
	if got.Artist != "Artist Name" {
		t.Fatalf("unexpected artist: %q", got.Artist)
	}
}

func TestParseSpotifyStateEmpty(t *testing.T) {
	got := parseSpotifyState("")
	if got.Running || got.TrackID != "" || got.TrackName != "" || got.Artist != "" || got.Playing {
		t.Fatalf("expected zero snapshot, got %+v", got)
	}
}

func TestParseSpotifyStateNotRunning(t *testing.T) {
	got := parseSpotifyState("0||||")
	if got.Running {
		t.Fatal("expected running=false")
	}
	if got.State != "" || got.TrackID != "" || got.TrackName != "" || got.Artist != "" {
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
