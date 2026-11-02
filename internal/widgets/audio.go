package widgets

import (
	"github.com/alexbabintsev/ulanzi-deck-go/internal/macos"
)

// audioSystem is the macOS audio API as the audio widgets use it. Each
// widget copies systemAudio when it is created, so tests can substitute
// it without racing running widgets.
type audioSystem struct {
	read       func(macos.Scope) (macos.AudioState, error)
	setVolume  func(macos.Scope, float64) error
	setMuted   func(macos.Scope, bool) error
	devices    func(macos.Scope) ([]macos.Device, error)
	setDefault func(macos.Scope, uint32) error
}

var systemAudio = audioSystem{
	read:       macos.DefaultAudio,
	setVolume:  macos.SetVolume,
	setMuted:   macos.SetMuted,
	devices:    macos.Devices,
	setDefault: macos.SetDefaultDevice,
}

// isMuted treats a device turned all the way down as muted: that is how
// devices without a mute switch are muted.
func isMuted(st macos.AudioState) bool { return st.Muted || st.Volume == 0 }

// toggleMute mutes or unmutes the default device of scope. Devices
// without a mute switch are muted by setting the volume to 0; *restore
// keeps the level to return to.
func toggleMute(sys audioSystem, scope macos.Scope, st macos.AudioState, restore *float64) error {
	if !isMuted(st) {
		if st.CanMute {
			return sys.setMuted(scope, true)
		}
		*restore = st.Volume
		return sys.setVolume(scope, 0)
	}
	if st.CanMute && st.Muted {
		if err := sys.setMuted(scope, false); err != nil {
			return err
		}
	}
	if st.Volume == 0 {
		level := *restore
		if level <= 0 {
			level = 0.5
		}
		return sys.setVolume(scope, level)
	}
	return nil
}
