package widgets

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/config"
)

func TestManagerResolveButtonsInjectsWidgetIcon(t *testing.T) {
	buttons := []config.Button{
		{
			At:        [2]int{0, 0},
			Label:     "Memory",
			RuntimeID: "buttons[0]",
			Widget:    &config.Widget{Type: "memory"},
		},
	}

	m, err := NewManager(&config.Config{Buttons: buttons})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer m.Close()

	got, err := m.ResolveButtons(buttons)
	if err != nil {
		t.Fatalf("resolve buttons: %v", err)
	}
	if got[0].Icon == "" {
		t.Fatal("expected resolved widget icon path")
	}
	if got[0].Label != "" {
		t.Fatalf("expected widget label to be suppressed, got %q", got[0].Label)
	}
}

func TestManagerSpotifyPlayPauseEnablesHoldGesture(t *testing.T) {
	buttons := []config.Button{
		{
			At:        [2]int{0, 0},
			RuntimeID: "buttons[0]",
			Widget:    &config.Widget{Type: "spotify_play_pause", HoldMS: 900},
		},
	}

	m, err := NewManager(&config.Config{Buttons: buttons})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer m.Close()

	opts, ok := m.gestureOptions("buttons[0]")
	if !ok {
		t.Fatal("expected spotify gesture options")
	}
	if !opts.HasTap || !opts.HasHold {
		t.Fatalf("expected tap and hold enabled, got %+v", opts)
	}
	if opts.HoldDelay != 900*time.Millisecond {
		t.Fatalf("unexpected hold delay: %v", opts.HoldDelay)
	}
}

func TestManagerPrewarmsSpotifyAndKeepsItCached(t *testing.T) {
	oldFactory := newSpotifyService
	defer func() { newSpotifyService = oldFactory }()

	var created int
	newSpotifyService = func(pollMS int) (*SpotifyService, error) {
		created++
		s := &SpotifyService{}
		s.cancel = func() {}
		return s, nil
	}

	buttons := []config.Button{
		{
			At:        [2]int{0, 0},
			RuntimeID: "buttons[0]",
			Widget:    &config.Widget{Type: "memory"},
		},
		{
			At:        [2]int{1, 0},
			RuntimeID: "buttons[1]",
			Widget:    &config.Widget{Type: "spotify_play_pause"},
		},
	}

	m, err := NewManager(&config.Config{Buttons: buttons})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer m.Close()

	if created != 1 || m.spotify == nil {
		t.Fatalf("expected spotify runtime to prewarm once, created=%d spotify=%v", created, m.spotify)
	}

	if err := m.SyncVisible(buttons[:1]); err != nil {
		t.Fatalf("sync visible memory page: %v", err)
	}
	if created != 1 || m.spotify == nil {
		t.Fatalf("expected spotify runtime to stay warm on non-spotify page, created=%d spotify=%v", created, m.spotify)
	}

	if err := m.SyncVisible(buttons[1:]); err != nil {
		t.Fatalf("sync visible spotify page: %v", err)
	}
	if created != 1 || m.spotify == nil {
		t.Fatalf("expected spotify runtime to reuse warm instance, created=%d spotify=%v", created, m.spotify)
	}

	if err := m.SyncVisible(buttons[:1]); err != nil {
		t.Fatalf("sync back to memory page: %v", err)
	}
	if m.spotify == nil {
		t.Fatal("expected spotify runtime to remain cached after leaving page")
	}
}

func TestManagerResolveButtonsInjectsWideDashboardIcon(t *testing.T) {
	oldFactory := newSpotifyService
	oldGeocodeURL := dashboardGeocodeURL
	oldForecastURL := dashboardForecastURL
	defer func() {
		newSpotifyService = oldFactory
		dashboardGeocodeURL = oldGeocodeURL
		dashboardForecastURL = oldForecastURL
	}()

	newSpotifyService = func(pollMS int) (*SpotifyService, error) {
		s := &SpotifyService{}
		s.cancel = func() {}
		return s, nil
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/geocode":
			_, _ = w.Write([]byte(`{"results":[{"name":"Kyiv","country":"Ukraine","latitude":50.45,"longitude":30.52,"timezone":"Europe/Kiev"}]}`))
		case "/forecast":
			_, _ = w.Write([]byte(`{"current":{"temperature_2m":17.4,"weather_code":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dashboardGeocodeURL = srv.URL + "/geocode"
	dashboardForecastURL = srv.URL + "/forecast"

	buttons := []config.Button{
		{
			At:        [2]int{3, 2},
			Label:     "Dashboard",
			RuntimeID: "buttons[0]",
			Widget:    &config.Widget{Type: "wide_dashboard", City: "Kyiv"},
		},
	}

	m, err := NewManager(&config.Config{Buttons: buttons})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer m.Close()
	m.SetContext(Context{PageIndex: 1, TotalPages: 3})

	got, err := m.ResolveButtons(buttons)
	if err != nil {
		t.Fatalf("resolve buttons: %v", err)
	}
	if got[0].Icon == "" {
		t.Fatal("expected resolved widget icon path")
	}
	if got[0].Label != "" {
		t.Fatalf("expected widget label to be suppressed, got %q", got[0].Label)
	}
}
