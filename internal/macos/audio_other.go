//go:build !darwin

package macos

func DefaultAudio(Scope) (AudioState, error) { return AudioState{}, ErrUnsupported }
func SetVolume(Scope, float64) error         { return ErrUnsupported }
func SetMuted(Scope, bool) error             { return ErrUnsupported }
func Devices(Scope) ([]Device, error)        { return nil, ErrUnsupported }
func SetDefaultDevice(Scope, uint32) error   { return ErrUnsupported }
