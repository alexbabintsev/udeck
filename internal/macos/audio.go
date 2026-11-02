// Package macos wraps the macOS system services the widgets use: the
// default audio devices (CoreAudio) and the media keys. Outside macOS
// every call fails with ErrUnsupported.
package macos

import "errors"

// ErrUnsupported is returned on platforms without the service.
var ErrUnsupported = errors.New("not supported on this platform")

// ErrFixedVolume reports a device whose volume software cannot change
// (common for USB DACs and HDMI/DisplayPort outputs).
var ErrFixedVolume = errors.New("device volume cannot be changed")

// Scope selects the output (speakers) or input (microphone) side.
type Scope int

const (
	Output Scope = iota
	Input
)

func (s Scope) String() string {
	if s == Input {
		return "input"
	}
	return "output"
}

// AudioState is the default device's level and mute state.
type AudioState struct {
	Device string  // device name
	Volume float64 // 0..1
	Muted  bool
	// CanMute reports a hardware/driver mute switch; without one, muting
	// is done by setting the volume to 0.
	CanMute bool
}

// Device is an audio device with streams in a scope.
type Device struct {
	ID   uint32
	Name string
}
