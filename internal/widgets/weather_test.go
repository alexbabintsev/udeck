package widgets

import (
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
)

// openMeteo fakes the geocoding and forecast endpoints.
type openMeteo struct {
	geocodes  atomic.Int32
	forecasts atomic.Int32
	fail      atomic.Bool
	srv       *httptest.Server
}

func newOpenMeteo(t *testing.T) *openMeteo {
	t.Helper()
	om := &openMeteo{}
	om.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if om.fail.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		q := r.URL.Query()
		switch r.URL.Path {
		case "/geo":
			om.geocodes.Add(1)
			if q.Get("name") == "Nowhere" {
				fmt.Fprint(w, `{"results": []}`)
				return
			}
			fmt.Fprint(w, `{"results":[{"name":"Kyiv","country":"Ukraine","latitude":50.45,"longitude":30.52,"timezone":"Europe/Kyiv"}]}`)
		case "/forecast":
			om.forecasts.Add(1)
			if q.Get("current") != "" {
				fmt.Fprint(w, `{"current":{"temperature_2m":21.5,"weather_code":2}}`)
				return
			}
			if q.Get("wind_speed_unit") != "ms" {
				http.Error(w, "wind unit must be m/s", http.StatusBadRequest)
				return
			}
			fmt.Fprint(w, `{"daily":{"time":["2026-10-07","bad-date"],"weather_code":[61,0],
				"temperature_2m_max":[12.3,15],"temperature_2m_min":[4.1,6],
				"apparent_temperature_mean":[10],"relative_humidity_2m_mean":[80.4],"wind_speed_10m_max":[5.5]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(om.srv.Close)
	oldGeo, oldFc := dashboardGeocodeURL, dashboardForecastURL
	dashboardGeocodeURL, dashboardForecastURL = om.srv.URL+"/geo", om.srv.URL+"/forecast"
	t.Cleanup(func() { dashboardGeocodeURL, dashboardForecastURL = oldGeo, oldFc })
	return om
}

var cityCounter atomic.Int64

// uniqueCity avoids the process-wide per-city cache across tests and
// repeated runs (-count).
func uniqueCity(t *testing.T) string {
	return fmt.Sprintf("City-%s-%d", strings.ReplaceAll(t.Name(), "/", "-"), cityCounter.Add(1))
}

func TestFetchWeatherDayAll(t *testing.T) {
	newOpenMeteo(t)
	days, err := fetchWeatherDayAll(dashboardWeatherCoords{Latitude: 1, Longitude: 2})
	if err != nil {
		t.Fatal(err)
	}
	d0, d1 := days[0], days[1]
	if d0 == nil || d0.TempMaxC != 12.3 || d0.HumidityPct != 80 || d0.WindSpeedMS != 5.5 || d0.FeelsLikeC != 10 {
		t.Fatalf("day0 = %+v", d0)
	}
	if d1 == nil || d1.FeelsLikeC != 15 || d1.HumidityPct != 0 || d1.WindSpeedMS != 0 {
		t.Fatalf("day1 fallbacks = %+v", d1)
	}
	if days[2] != nil {
		t.Fatal("missing days must stay nil")
	}
}

func TestFetchDashboardCoordsAndWeather(t *testing.T) {
	om := newOpenMeteo(t)
	coords, err := fetchDashboardCoords("Kyiv")
	if err != nil || coords.Name != "Kyiv, Ukraine" || coords.Timezone != "Europe/Kyiv" {
		t.Fatalf("coords=%+v err=%v", coords, err)
	}
	if _, err := fetchDashboardCoords("Nowhere"); err == nil {
		t.Fatal("expected city not found")
	}
	state, err := fetchDashboardWeather(dashboardWeatherCoords{})
	if err != nil || !state.Ready || *state.TempC != 21.5 || state.Code != 2 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	om.fail.Store(true)
	if _, err := fetchDashboardWeather(*coords); err == nil {
		t.Fatal("expected forecast error")
	}
	if _, err := fetchWeatherDayAll(*coords); err == nil {
		t.Fatal("expected daily forecast error")
	}
}

func TestWeatherDayWidgetsShareOneFetchPerCity(t *testing.T) {
	om := newOpenMeteo(t)
	city := uniqueCity(t)
	rec := &recorder{}
	today, err := NewWeatherDayWidget("today", &config.Widget{City: city}, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	defer today.Close()
	waitFor(t, "forecast", func() bool {
		today.mu.Lock()
		defer today.mu.Unlock()
		return today.forecast != nil
	})
	// A second widget for the same city reuses the cached data.
	tomorrow, err := NewWeatherDayWidget("tomorrow", &config.Widget{City: city, DayIndex: 1}, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	if tomorrow.forecast == nil || tomorrow.forecast.TempMaxC != 15 {
		t.Fatal("expected cached forecast for day 1")
	}
	tomorrow.cache.refresh(city, time.Hour)
	if om.forecasts.Load() != 1 || om.geocodes.Load() != 1 {
		t.Fatalf("expected one fetch, got geocode=%d forecast=%d", om.geocodes.Load(), om.forecasts.Load())
	}

	// Closing one widget must remove only its own subscription.
	if err := tomorrow.Close(); err != nil {
		t.Fatal(err)
	}
	cache := weatherCacheForCity(city)
	cache.mu.Lock()
	_, todayStillSubscribed := cache.subs[today.subToken]
	n := len(cache.subs)
	cache.mu.Unlock()
	if !todayStillSubscribed || n != 1 {
		t.Fatalf("subscriptions after close: %d (today subscribed=%v)", n, todayStillSubscribed)
	}
	today.HandleGesture("tap")
}

func TestWeatherDayWidgetClampsDayIndexAndSurvivesErrors(t *testing.T) {
	om := newOpenMeteo(t)
	om.fail.Store(true)
	w, err := NewWeatherDayWidget("w", &config.Widget{City: uniqueCity(t), DayIndex: 99}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if w.dayIndex != weatherDayMaxForecast-1 {
		t.Fatalf("dayIndex = %d", w.dayIndex)
	}
	waitFor(t, "failed geocode attempt", func() bool { return om.geocodes.Load() == 0 && w.cache.lastFetchSet() })
	w2, _ := NewWeatherDayWidget("w2", &config.Widget{City: uniqueCity(t), DayIndex: -3}, nil)
	defer w2.Close()
	if w2.dayIndex != 0 {
		t.Fatal("negative day index must clamp to 0")
	}
	// Let the background fetch finish before the test restores the URLs.
	waitFor(t, "second fetch attempt", w2.cache.lastFetchSet)
}

func (c *weatherCityCache) lastFetchSet() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.lastFetch.IsZero() && !c.fetching
}

func TestWeatherRenderingHelpers(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 196, 196))
	renderWeatherDayCard(img, 0, nil, "Kyiv")
	for _, code := range []int{0, 1, 2, 3, 45, 51, 61, 71, 80, 95, 999} {
		renderWeatherDayCard(img, 1, &weatherDayData{Date: time.Now(), Code: code, TempMaxC: -3, TempMinC: -10}, "Kyiv")
		_ = weatherDayIconColor(code)
		if weatherCodeToLucideID(code) == "" {
			t.Fatalf("no icon for code %d", code)
		}
	}
	if weatherDayFrameKey("a", 0, nil) == weatherDayFrameKey("a", 0, &weatherDayData{}) {
		t.Fatal("frame key must change when data arrives")
	}
	if weatherDayPoll(0) != weatherDayDefaultPoll || weatherDayPoll(1) != weatherDayMinPoll || weatherDayPoll(120000) != 2*time.Minute {
		t.Fatal("weatherDayPoll")
	}
}
