package widgets

import (
	"sync"
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/icons"

	"github.com/alexbabintsev/udeck/internal/config"
	ig "github.com/alexbabintsev/udeck/internal/gestures"
	"github.com/alexbabintsev/udeck/internal/macos"
)

func stubSpotifyFactory(t *testing.T) {
	t.Helper()
	old := newSpotifyService
	newSpotifyService = func(int) (*SpotifyService, error) { return &SpotifyService{cancel: func() {}}, nil }
	t.Cleanup(func() { newSpotifyService = old })
}

func TestManagerGestureOptionsPerWidgetType(t *testing.T) {
	stubSpotifyFactory(t)
	btn := func(id, typ string, on *config.WidgetOn) config.Button {
		return config.Button{RuntimeID: id, Widget: &config.Widget{Type: typ, On: on, HoldMS: 900}}
	}
	m, err := NewManager(&config.Config{Buttons: []config.Button{
		btn("spp", "spotify_play_pause", nil),
		btn("mem", "memory", nil),
		{RuntimeID: "vol-up", Widget: &config.Widget{Type: "volume", Action: "up"}},
		btn("mic", "microphone", nil),
		btn("pomo", "pomodoro", nil),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if o, _ := m.gestureOptions("spp"); !o.HasHold || o.HoldDelay != 900*time.Millisecond {
		t.Fatalf("spotify options %+v", o)
	}
	if o, ok := m.gestureOptions("mem"); !ok || !o.HasTap || o.HasHold {
		t.Fatalf("memory options %+v", o)
	}
	if o, _ := m.gestureOptions("vol-up"); !o.HasRepeat || o.RepeatEvery != volumeRepeatEvery {
		t.Fatalf("volume up options %+v", o)
	}
	if o, ok := m.gestureOptions("mic"); !ok || !o.HasTap || o.HasHold {
		t.Fatalf("microphone options %+v", o)
	}
	if o, _ := m.gestureOptions("pomo"); !o.HasTap || !o.HasHold || o.HoldDelay != timerHoldDelay {
		t.Fatalf("pomodoro options %+v", o)
	}
	if _, ok := m.gestureOptions("missing"); ok {
		t.Fatal("unknown id must have no options")
	}
}

// tapWidget records gestures.
type tapWidget struct {
	mu    sync.Mutex
	kinds []ig.Kind
}

func (w *tapWidget) IconPath() string { return "" }
func (w *tapWidget) Close() error     { return nil }
func (w *tapWidget) HandleGesture(k ig.Kind) {
	w.mu.Lock()
	w.kinds = append(w.kinds, k)
	w.mu.Unlock()
}

func TestManagerHandleEventAndTickDispatchGestures(t *testing.T) {
	stubSpotifyFactory(t)
	buttons := []config.Button{{RuntimeID: "spp", Widget: &config.Widget{Type: "spotify_play_pause", HoldMS: 50}}}
	m, err := NewManager(&config.Config{Buttons: buttons})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	w := &tapWidget{}
	m.items["spp"] = w

	now := time.Now()
	if !m.HandleEvent("spp", true, now) || !m.HandleEvent("spp", false, now.Add(10*time.Millisecond)) {
		t.Fatal("widget events must be consumed")
	}
	// Hold fires from Tick once the delay elapsed.
	m.HandleEvent("spp", true, now.Add(time.Second))
	if !m.Tick(buttons, now.Add(2*time.Second)) {
		t.Fatal("tick must fire the hold gesture")
	}
	if m.Tick(buttons, now.Add(3*time.Second)) {
		t.Fatal("hold must fire once")
	}
	m.HandleEvent("spp", false, now.Add(4*time.Second))
	w.mu.Lock()
	got := append([]ig.Kind(nil), w.kinds...)
	w.mu.Unlock()
	if len(got) != 2 || got[0] != ig.Tap || got[1] != ig.Hold {
		t.Fatalf("gestures = %v", got)
	}
	if m.HandleEvent("unknown", true, now) {
		t.Fatal("unknown id must not be consumed")
	}
	if m.Tick([]config.Button{{RuntimeID: "x"}}, now) {
		t.Fatal("no widgets: nothing to tick")
	}
}

func TestManagerResolveButtonsOverlaysPageNumber(t *testing.T) {
	buttons := []config.Button{
		{At: [2]int{0, 0}, Label: "Next", On: &config.Handlers{Tap: &config.GestureBinding{Type: "page_next"}}},
		{At: [2]int{1, 0}, Label: "Prev", IconColor: "ff0000", On: &config.Handlers{Hold: &config.GestureBinding{Type: "page_prev"}}},
		{At: [2]int{2, 0}, Label: "Plain"},
	}
	m, err := NewManager(&config.Config{Buttons: buttons})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.SetContext(Context{PageIndex: 2, TotalPages: 3})
	got, err := m.ResolveButtons(buttons)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Icon == "" || got[0].Label != "" || got[1].Icon == "" || got[1].Icon == got[0].Icon {
		t.Fatalf("expected distinct overlay icons: %+v", got[:2])
	}
	if got[2].Icon != "" || got[2].Label != "Plain" {
		t.Fatal("plain button must be untouched")
	}
	again, _ := m.ResolveButtons(buttons)
	if again[0].Icon != got[0].Icon {
		t.Fatal("overlay must be cached")
	}
	if pageNavIconID(buttons[0]) != "lucide/arrow-right" || pageNavIconID(buttons[1]) != "lucide/arrow-left" || pageNavIconID(buttons[2]) != "lucide/arrow-right" {
		t.Fatal("pageNavIconID")
	}
	if isPageNavButton(config.Button{Widget: &config.Widget{}}) || isPageNavButton(buttons[2]) {
		t.Fatal("isPageNavButton")
	}
}

func TestManagerRejectsUnknownWidget(t *testing.T) {
	m, err := NewManager(&config.Config{Buttons: []config.Button{{RuntimeID: "x", Widget: &config.Widget{Type: "nope"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := m.ResolveButtons([]config.Button{{RuntimeID: "x", Widget: &config.Widget{Type: "nope"}}}); err == nil {
		t.Fatal("expected unsupported widget error")
	}
	if _, err := m.ResolveButtons([]config.Button{{RuntimeID: "missing", Widget: &config.Widget{Type: "memory"}}}); err == nil {
		t.Fatal("expected unknown widget id error")
	}
}

func TestManagerCreatesEveryWidgetType(t *testing.T) {
	stubSpotifyFactory(t)
	newOpenMeteo(t)
	audio := newFakeAudio()
	audio.set(macos.Output, macos.AudioState{Device: "Speakers", Volume: 0.5}, nil)
	useFakeAudio(t, audio)
	useFakeMedia(t, true, nil)

	buttons := []config.Button{
		{RuntimeID: "a", Widget: &config.Widget{Type: "spotify_play_pause"}},
		{RuntimeID: "c", Widget: &config.Widget{Type: "spotify_artwork"}},
		{RuntimeID: "d", At: [2]int{3, 2}, Widget: &config.Widget{Type: "wide_dashboard"}},
		{RuntimeID: "e", Widget: &config.Widget{Type: "weather_day", City: uniqueCity(t)}},
		{RuntimeID: "i", Widget: &config.Widget{Type: "volume"}},
		{RuntimeID: "j", Widget: &config.Widget{Type: "microphone"}},
		{RuntimeID: "k", Widget: &config.Widget{Type: "audio_output"}},
		{RuntimeID: "l", Widget: &config.Widget{Type: "media", Action: "next"}},
		{RuntimeID: "n", Widget: &config.Widget{Type: "memory"}},
		{RuntimeID: "o", Widget: &config.Widget{Type: "clock", Timezone: "Europe/Kyiv"}},
		{RuntimeID: "p", Widget: &config.Widget{Type: "timer"}},
		{RuntimeID: "q", Widget: &config.Widget{Type: "stopwatch"}},
		{RuntimeID: "r", Widget: &config.Widget{Type: "pomodoro"}},
		{RuntimeID: "s", Widget: &config.Widget{Type: "cpu"}},
		{RuntimeID: "u", Widget: &config.Widget{Type: "disk"}},
		{RuntimeID: "v", Widget: &config.Widget{Type: "network"}},
		{RuntimeID: "w", Widget: &config.Widget{Type: "battery"}},
	}
	m, err := NewManager(&config.Config{Buttons: buttons})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := m.ResolveButtons(buttons)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range resolved {
		if _, ok := icons.Image(b.Icon); !ok {
			t.Fatalf("%s: icon %q not rendered", b.RuntimeID, b.Icon)
		}
	}
	m.SetContext(Context{FolderDepth: 1})
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}
