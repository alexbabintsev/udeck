package widgets

import (
	"errors"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/macos"
)

// fakeAudio is an in-memory audio system: one default device per scope.
type fakeAudio struct {
	mu      sync.Mutex
	state   map[macos.Scope]macos.AudioState
	readErr map[macos.Scope]error
	devs    []macos.Device
	calls   []string
}

func newFakeAudio() *fakeAudio {
	return &fakeAudio{state: map[macos.Scope]macos.AudioState{}, readErr: map[macos.Scope]error{}}
}

func (f *fakeAudio) set(s macos.Scope, st macos.AudioState, err error) {
	f.mu.Lock()
	f.state[s], f.readErr[s] = st, err
	f.mu.Unlock()
}

func (f *fakeAudio) get(s macos.Scope) macos.AudioState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state[s]
}

func (f *fakeAudio) log(c string) {
	f.calls = append(f.calls, c)
}

func (f *fakeAudio) callLog() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ",")
}

func (f *fakeAudio) system() audioSystem {
	return audioSystem{
		read: func(s macos.Scope) (macos.AudioState, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.state[s], f.readErr[s]
		},
		setVolume: func(s macos.Scope, v float64) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			st := f.state[s]
			st.Volume = math.Round(min(max(v, 0), 1)*1000) / 1000
			f.state[s] = st
			f.log("vol")
			return nil
		},
		setMuted: func(s macos.Scope, m bool) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			st := f.state[s]
			if !st.CanMute {
				return errors.New("no mute switch")
			}
			st.Muted = m
			f.state[s] = st
			f.log("mute")
			return nil
		},
		devices: func(macos.Scope) ([]macos.Device, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.devs, nil
		},
		setDefault: func(s macos.Scope, id uint32) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, d := range f.devs {
				if d.ID == id {
					st := f.state[s]
					st.Device = d.Name
					f.state[s] = st
					f.readErr[s] = nil
				}
			}
			f.log("default")
			return nil
		},
	}
}

// useFakeAudio makes widgets created by the test use f.
func useFakeAudio(t *testing.T, f *fakeAudio) {
	t.Helper()
	old := systemAudio
	systemAudio = f.system()
	t.Cleanup(func() { systemAudio = old })
}

func countPixels(img image.Image, pred func(color.RGBA) bool) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if pred(rgbaAt(img, x, y)) {
				n++
			}
		}
	}
	return n
}

func near(c, want color.RGBA) bool {
	d := func(a, b uint8) int { return abs(int(a) - int(b)) }
	return d(c.R, want.R) < 24 && d(c.G, want.G) < 24 && d(c.B, want.B) < 24
}

func hasColor(img image.Image, want color.RGBA) bool {
	return countPixels(img, func(c color.RGBA) bool { return near(c, want) }) > 20
}

func TestToggleMute(t *testing.T) {
	cases := []struct {
		name    string
		st      macos.AudioState
		restore float64
		want    macos.AudioState
		calls   string
	}{
		{"switch mutes", macos.AudioState{Volume: 0.4, CanMute: true}, 0, macos.AudioState{Volume: 0.4, CanMute: true, Muted: true}, "mute"},
		{"switch unmutes", macos.AudioState{Volume: 0.4, CanMute: true, Muted: true}, 0, macos.AudioState{Volume: 0.4, CanMute: true}, "mute"},
		{"switch unmutes at zero", macos.AudioState{CanMute: true, Muted: true}, 0.3, macos.AudioState{Volume: 0.3, CanMute: true}, "mute,vol"},
		{"no switch mutes via volume", macos.AudioState{Volume: 0.7}, 0, macos.AudioState{}, "vol"},
		{"no switch restores", macos.AudioState{}, 0.7, macos.AudioState{Volume: 0.7}, "vol"},
		{"no switch restores default", macos.AudioState{}, 0, macos.AudioState{Volume: 0.5}, "vol"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeAudio()
			f.set(macos.Output, tc.st, nil)
			restore := tc.restore
			if err := toggleMute(f.system(), macos.Output, tc.st, &restore); err != nil {
				t.Fatal(err)
			}
			if got := f.get(macos.Output); got != tc.want {
				t.Fatalf("state %+v, want %+v", got, tc.want)
			}
			if got := f.callLog(); got != tc.calls {
				t.Fatalf("calls %q, want %q", got, tc.calls)
			}
		})
	}
	// Muting via volume remembers the level to come back to.
	f := newFakeAudio()
	st := macos.AudioState{Volume: 0.35}
	f.set(macos.Output, st, nil)
	var restore float64
	_ = toggleMute(f.system(), macos.Output, st, &restore)
	if restore != 0.35 {
		t.Fatalf("restore = %v", restore)
	}
}

func TestVolumeWidgetMuteTapAndRender(t *testing.T) {
	f := newFakeAudio()
	f.set(macos.Output, macos.AudioState{Device: "Speakers", Volume: 0.42, CanMute: true}, nil)
	useFakeAudio(t, f)
	rec := &recorder{}
	w, err := NewVolumeWidget("v", &config.Widget{Type: "volume"}, rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if !hasColor(readIcon(t, w.IconPath()), styleAccent) {
		t.Fatal("expected the accent level ring")
	}

	w.HandleGesture(ig.Tap)
	if !f.get(macos.Output).Muted {
		t.Fatal("tap must mute")
	}
	img := readIcon(t, w.IconPath())
	if hasColor(img, styleAccent) || !hasColor(img, styleAlert) {
		t.Fatal("muted volume must show a red icon and no level")
	}
	if rec.count() == 0 {
		t.Fatal("expected a notification after the tap")
	}
	w.HandleGesture(ig.Tap)
	if st := f.get(macos.Output); st.Muted || st.Volume != 0.42 {
		t.Fatalf("second tap must unmute, got %+v", st)
	}
	w.HandleGesture(ig.DoubleTap) // ignored
	w.HandleGesture(ig.Hold)      // mute only reacts to taps
	if f.get(macos.Output).Muted {
		t.Fatal("hold must not toggle mute")
	}
}

func TestVolumeWidgetStepsSnapAndUnmute(t *testing.T) {
	f := newFakeAudio()
	f.set(macos.Output, macos.AudioState{Volume: 0.42, CanMute: true, Muted: true}, nil)
	useFakeAudio(t, f)
	up, err := NewVolumeWidget("up", &config.Widget{Type: "volume", Action: "up", Step: 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()
	up.HandleGesture(ig.Tap)
	if st := f.get(macos.Output); st.Muted || st.Volume != 0.5 {
		t.Fatalf("up must unmute and snap to the grid, got %+v", st)
	}
	up.HandleGesture(ig.Repeat)
	if v := f.get(macos.Output).Volume; v != 0.6 {
		t.Fatalf("repeat must step again, got %v", v)
	}

	down, err := NewVolumeWidget("down", &config.Widget{Type: "volume", Action: "down"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer down.Close()
	down.HandleGesture(ig.Hold)
	if v := f.get(macos.Output).Volume; v != 0.55 {
		t.Fatalf("down by the default 5%%, got %v", v)
	}
	f.set(macos.Output, macos.AudioState{Volume: 0.02, CanMute: true}, nil)
	down.refresh()
	down.HandleGesture(ig.Tap)
	if v := f.get(macos.Output).Volume; v != 0 {
		t.Fatalf("down must stop at 0, got %v", v)
	}
}

func TestVolumeWidgetFixedAndMissingDevice(t *testing.T) {
	f := newFakeAudio()
	f.set(macos.Output, macos.AudioState{Device: "USB DAC"}, macos.ErrFixedVolume)
	useFakeAudio(t, f)
	w, err := NewVolumeWidget("v", &config.Widget{Type: "volume", Action: "up"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.HandleGesture(ig.Tap)
	if f.callLog() != "" {
		t.Fatalf("a fixed-volume device must not be touched, calls %q", f.callLog())
	}
	img := readIcon(t, w.IconPath())
	if hasColor(img, styleText) || !hasColor(img, styleDim) {
		t.Fatal("fixed volume must render dim")
	}
	// The device comes back: the next tap refreshes first, then steps.
	f.set(macos.Output, macos.AudioState{Volume: 0.5}, nil)
	w.HandleGesture(ig.Tap)
	if !hasColor(readIcon(t, w.IconPath()), styleText) {
		t.Fatal("expected a live key once the device is back")
	}
	w.HandleGesture(ig.Tap)
	if v := f.get(macos.Output).Volume; v != 0.55 {
		t.Fatalf("volume %v after the device came back", v)
	}
	if renderVolume(macos.AudioState{}, errors.New("x"), "mute") == nil {
		t.Fatal("render must handle generic errors")
	}
}

func TestRenderVolumeKeysDiffer(t *testing.T) {
	st := macos.AudioState{Volume: 0.52, CanMute: true}
	mute, up, down := renderVolume(st, nil, "mute"), renderVolume(st, nil, "up"), renderVolume(st, nil, "down")
	if !hasColor(mute, styleAccent) {
		t.Fatal("the mute key shows the level ring")
	}
	if hasColor(up, styleAccent) || hasColor(down, styleAccent) {
		t.Fatal("up/down keys must not repeat the ring")
	}
	if string(up.Pix) == string(down.Pix) {
		t.Fatal("up and down must differ")
	}
	muted := macos.AudioState{Volume: 0.52, Muted: true, CanMute: true}
	if !hasColor(renderVolume(muted, nil, "up"), styleAlert) {
		t.Fatal("up/down show the muted state in red")
	}
}

func TestVolumeGestures(t *testing.T) {
	if o := volumeGestures(&config.Widget{Action: "up"}); !o.HasTap || !o.HasHold || !o.HasRepeat || o.RepeatEvery != volumeRepeatEvery {
		t.Fatalf("up options %+v", o)
	}
	if o := volumeGestures(&config.Widget{}); !o.HasTap || o.HasHold || o.HasRepeat {
		t.Fatalf("mute options %+v", o)
	}
	if pollOr(0, defaultVolumePoll) != defaultVolumePoll || pollOr(300, 0) != 300*time.Millisecond {
		t.Fatal("pollOr")
	}
}

func TestMicrophoneWidget(t *testing.T) {
	f := newFakeAudio()
	f.set(macos.Input, macos.AudioState{Device: "Mic", Volume: 0.6, CanMute: true}, nil)
	useFakeAudio(t, f)
	w, err := NewMicrophoneWidget("m", &config.Widget{Type: "microphone"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if !hasColor(readIcon(t, w.IconPath()), styleLive) {
		t.Fatal("a live mic must be green")
	}
	w.HandleGesture(ig.Tap)
	if !f.get(macos.Input).Muted || f.get(macos.Output).Muted {
		t.Fatal("tap must mute the input only")
	}
	img := readIcon(t, w.IconPath())
	if hasColor(img, styleLive) || !hasColor(img, styleAlert) {
		t.Fatal("a muted mic must be red")
	}
	w.HandleGesture(ig.Hold) // ignored
	if !f.get(macos.Input).Muted {
		t.Fatal("hold must not toggle")
	}

	// No input device: dim, and a tap does nothing.
	f.set(macos.Input, macos.AudioState{}, errors.New("no device"))
	w.refresh()
	w.HandleGesture(ig.Tap)
	if hasColor(readIcon(t, w.IconPath()), styleAlert) {
		t.Fatal("no mic must render dim")
	}
	if got := f.callLog(); got != "mute" {
		t.Fatalf("calls %q", got)
	}
}

func TestRenderMicrophoneTitleKeepsColor(t *testing.T) {
	plain := renderMicrophone(macos.AudioState{Volume: 1}, nil, "")
	titled := renderMicrophone(macos.AudioState{Volume: 1}, nil, "Zoom")
	if !hasColor(titled, styleLive) {
		t.Fatal("the title must not change the state color")
	}
	if string(plain.Pix) == string(titled.Pix) {
		t.Fatal("the title must replace the state word")
	}
}

func TestPickDevices(t *testing.T) {
	all := []macos.Device{{ID: 1, Name: "MacBook Pro Speakers"}, {ID: 2, Name: "AirPods Pro"}, {ID: 3, Name: "SMSL USB AUDIO"}, {ID: 4, Name: "AirPods Max"}}
	names := func(ds []macos.Device) string {
		var s []string
		for _, d := range ds {
			s = append(s, d.Name)
		}
		return strings.Join(s, "|")
	}
	if got := names(pickDevices(all, nil)); got != "MacBook Pro Speakers|AirPods Pro|SMSL USB AUDIO|AirPods Max" {
		t.Fatalf("all: %s", got)
	}
	if got := names(pickDevices(all, []string{"smsl", "airpods", "pro"})); got != "SMSL USB AUDIO|AirPods Pro|AirPods Max|MacBook Pro Speakers" {
		t.Fatalf("ordered: %s", got)
	}
	if got := pickDevices(all, []string{"hdmi"}); len(got) != 0 {
		t.Fatalf("no match: %v", got)
	}
}

func TestAudioOutputWidgetCycles(t *testing.T) {
	f := newFakeAudio()
	f.devs = []macos.Device{{ID: 1, Name: "MacBook Pro Speakers"}, {ID: 2, Name: "AirPods Pro"}, {ID: 3, Name: "SMSL USB AUDIO"}}
	f.set(macos.Output, macos.AudioState{Device: "SMSL USB AUDIO"}, macos.ErrFixedVolume)
	useFakeAudio(t, f)
	w, err := NewAudioOutputWidget("o", &config.Widget{Type: "audio_output", Devices: []string{" SMSL ", "airpods", ""}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if !hasColor(readIcon(t, w.IconPath()), styleAccent) {
		t.Fatal("a fixed-volume device still shows its name")
	}
	want := []string{"AirPods Pro", "SMSL USB AUDIO", "AirPods Pro"}
	for _, name := range want {
		w.HandleGesture(ig.Tap)
		if got := f.get(macos.Output).Device; got != name {
			t.Fatalf("switched to %q, want %q", got, name)
		}
	}
	w.HandleGesture(ig.Hold) // ignored
	if f.get(macos.Output).Device != "AirPods Pro" {
		t.Fatal("hold must not switch")
	}

	// The current device is not in the list: start from the first.
	f.set(macos.Output, macos.AudioState{Device: "MacBook Pro Speakers"}, nil)
	w.refresh()
	w.HandleGesture(ig.Tap)
	if f.get(macos.Output).Device != "SMSL USB AUDIO" {
		t.Fatal("expected the first configured device")
	}

	// None of the configured devices is connected.
	w.devices = []string{"hdmi"}
	if err := w.switchNext(); err == nil {
		t.Fatal("expected an error")
	}
}

func TestRenderAudioOutput(t *testing.T) {
	if hasColor(renderAudioOutput("", nil), styleAccent) || hasColor(renderAudioOutput("X", errors.New("x")), styleAccent) {
		t.Fatal("no device must render dim")
	}
	if audioDeviceIcon("Alex's AirPods Pro") != "lucide/headphones" || audioDeviceIcon("Наушники") != "lucide/headphones" || audioDeviceIcon("DELL U2720Q") != "lucide/speaker" {
		t.Fatal("audioDeviceIcon")
	}
	// A long device name wraps instead of being cut: both lines are drawn.
	img := renderAudioOutput("MacBook Pro Speakers With Long Name", nil)
	top := anyPixel(img, image.Rect(0, styleLabelTop-6, 196, styleLabelTop+18), func(c color.RGBA) bool { return c.R > 150 })
	bottom := anyPixel(img, image.Rect(0, styleLabelTop+22, 196, 196), func(c color.RGBA) bool { return c.R > 150 })
	if !top || !bottom {
		t.Fatalf("expected a two-line label, top=%v bottom=%v", top, bottom)
	}
}

func TestSplitLabel(t *testing.T) {
	cases := map[string][2]string{
		"MACBOOK PRO SPEAKERS": {"MACBOOK PRO", "SPEAKERS"},
		"ABCDEF":               {"ABC", "DEF"},
		"A B":                  {"A", "B"},
	}
	for in, want := range cases {
		if a, b := splitLabel(in); a != want[0] || b != want[1] {
			t.Fatalf("splitLabel(%q) = %q, %q", in, a, b)
		}
	}
}
