//go:build darwin

package macos

/*
#cgo LDFLAGS: -framework CoreAudio -framework AudioToolbox -framework CoreFoundation
#include <CoreAudio/CoreAudio.h>
#include <AudioToolbox/AudioServices.h>
#include <stdlib.h>

static AudioObjectPropertyScope scope_of(int input) {
	return input ? kAudioDevicePropertyScopeInput : kAudioDevicePropertyScopeOutput;
}

static OSStatus default_device(int input, AudioObjectID *out) {
	AudioObjectPropertyAddress a = {
		input ? kAudioHardwarePropertyDefaultInputDevice : kAudioHardwarePropertyDefaultOutputDevice,
		kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain };
	UInt32 size = sizeof(*out);
	return AudioObjectGetPropertyData(kAudioObjectSystemObject, &a, 0, NULL, &size, out);
}

static OSStatus set_default_device(int input, AudioObjectID dev) {
	AudioObjectPropertyAddress a = {
		input ? kAudioHardwarePropertyDefaultInputDevice : kAudioHardwarePropertyDefaultOutputDevice,
		kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain };
	return AudioObjectSetPropertyData(kAudioObjectSystemObject, &a, 0, NULL, sizeof(dev), &dev);
}

// device_name copies the UTF-8 name into buf.
static OSStatus device_name(AudioObjectID dev, char *buf, int len) {
	AudioObjectPropertyAddress a = { kAudioObjectPropertyName,
		kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain };
	CFStringRef name = NULL;
	UInt32 size = sizeof(name);
	OSStatus st = AudioObjectGetPropertyData(dev, &a, 0, NULL, &size, &name);
	if (st != noErr) return st;
	if (name == NULL) return -1;
	Boolean ok = CFStringGetCString(name, buf, len, kCFStringEncodingUTF8);
	CFRelease(name);
	return ok ? noErr : -1;
}

static OSStatus get_volume(AudioObjectID dev, int input, Float32 *out) {
	AudioObjectPropertyAddress a = { kAudioHardwareServiceDeviceProperty_VirtualMainVolume,
		scope_of(input), kAudioObjectPropertyElementMain };
	if (!AudioObjectHasProperty(dev, &a)) return kAudioHardwareUnknownPropertyError;
	UInt32 size = sizeof(*out);
	return AudioObjectGetPropertyData(dev, &a, 0, NULL, &size, out);
}

static OSStatus set_volume(AudioObjectID dev, int input, Float32 v) {
	AudioObjectPropertyAddress a = { kAudioHardwareServiceDeviceProperty_VirtualMainVolume,
		scope_of(input), kAudioObjectPropertyElementMain };
	if (!AudioObjectHasProperty(dev, &a)) return kAudioHardwareUnknownPropertyError;
	return AudioObjectSetPropertyData(dev, &a, 0, NULL, sizeof(v), &v);
}

// mute_settable reports a settable mute switch on the main element.
static int mute_settable(AudioObjectID dev, int input) {
	AudioObjectPropertyAddress a = { kAudioDevicePropertyMute, scope_of(input), kAudioObjectPropertyElementMain };
	Boolean settable = false;
	if (!AudioObjectHasProperty(dev, &a)) return 0;
	if (AudioObjectIsPropertySettable(dev, &a, &settable) != noErr) return 0;
	return settable ? 1 : 0;
}

static OSStatus get_mute(AudioObjectID dev, int input, UInt32 *out) {
	AudioObjectPropertyAddress a = { kAudioDevicePropertyMute, scope_of(input), kAudioObjectPropertyElementMain };
	UInt32 size = sizeof(*out);
	return AudioObjectGetPropertyData(dev, &a, 0, NULL, &size, out);
}

static OSStatus set_mute(AudioObjectID dev, int input, UInt32 m) {
	AudioObjectPropertyAddress a = { kAudioDevicePropertyMute, scope_of(input), kAudioObjectPropertyElementMain };
	return AudioObjectSetPropertyData(dev, &a, 0, NULL, sizeof(m), &m);
}

// has_streams reports whether dev has streams in the scope (is a
// speaker or a microphone).
static int has_streams(AudioObjectID dev, int input) {
	AudioObjectPropertyAddress a = { kAudioDevicePropertyStreams, scope_of(input), kAudioObjectPropertyElementMain };
	UInt32 size = 0;
	if (AudioObjectGetPropertyDataSize(dev, &a, 0, NULL, &size) != noErr) return 0;
	return size > 0;
}

// list_devices fills ids (capacity cap) and returns the device count,
// or a negative OSStatus.
static int list_devices(AudioObjectID *ids, int cap) {
	AudioObjectPropertyAddress a = { kAudioHardwarePropertyDevices,
		kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain };
	UInt32 size = 0;
	OSStatus st = AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &a, 0, NULL, &size);
	if (st != noErr) return -1;
	int n = size / sizeof(AudioObjectID);
	if (n > cap) n = cap;
	size = n * sizeof(AudioObjectID);
	st = AudioObjectGetPropertyData(kAudioObjectSystemObject, &a, 0, NULL, &size, ids);
	if (st != noErr) return -1;
	return size / sizeof(AudioObjectID);
}
*/
import "C"

import (
	"fmt"
	"strings"
)

func inputFlag(s Scope) C.int {
	if s == Input {
		return 1
	}
	return 0
}

func osErr(what string, st C.OSStatus) error {
	if st == 0 {
		return nil
	}
	return fmt.Errorf("%s: OSStatus %d", what, int32(st))
}

func defaultDevice(s Scope) (C.AudioObjectID, error) {
	var dev C.AudioObjectID
	if err := osErr("default "+s.String()+" device", C.default_device(inputFlag(s), &dev)); err != nil {
		return 0, err
	}
	if dev == 0 {
		return 0, fmt.Errorf("no default %s device", s)
	}
	return dev, nil
}

func deviceName(dev C.AudioObjectID) string {
	var buf [256]C.char
	if C.device_name(dev, &buf[0], C.int(len(buf))) != 0 {
		return ""
	}
	return strings.TrimSpace(C.GoString(&buf[0]))
}

// DefaultAudio reads the default device's name, volume and mute state.
func DefaultAudio(s Scope) (AudioState, error) {
	dev, err := defaultDevice(s)
	if err != nil {
		return AudioState{}, err
	}
	state := AudioState{Device: deviceName(dev), CanMute: C.mute_settable(dev, inputFlag(s)) == 1}
	var v C.Float32
	if st := C.get_volume(dev, inputFlag(s), &v); st == C.kAudioHardwareUnknownPropertyError {
		return state, ErrFixedVolume
	} else if err := osErr("volume", st); err != nil {
		return state, err
	}
	state.Volume = float64(v)
	if state.CanMute {
		var m C.UInt32
		if C.get_mute(dev, inputFlag(s), &m) == 0 {
			state.Muted = m != 0
		}
	}
	return state, nil
}

// SetVolume sets the default device's volume (clamped to 0..1).
func SetVolume(s Scope, v float64) error {
	dev, err := defaultDevice(s)
	if err != nil {
		return err
	}
	v = min(max(v, 0), 1)
	st := C.set_volume(dev, inputFlag(s), C.Float32(v))
	if st == C.kAudioHardwareUnknownPropertyError {
		return ErrFixedVolume
	}
	return osErr("set volume", st)
}

// SetMuted switches the default device's mute; it fails when the device
// has no mute switch (see AudioState.CanMute).
func SetMuted(s Scope, muted bool) error {
	dev, err := defaultDevice(s)
	if err != nil {
		return err
	}
	if C.mute_settable(dev, inputFlag(s)) != 1 {
		return fmt.Errorf("%s device has no mute switch", s)
	}
	var m C.UInt32
	if muted {
		m = 1
	}
	return osErr("set mute", C.set_mute(dev, inputFlag(s), m))
}

// Devices lists the devices with streams in scope s.
func Devices(s Scope) ([]Device, error) {
	var ids [128]C.AudioObjectID
	n := C.list_devices(&ids[0], C.int(len(ids)))
	if n < 0 {
		return nil, fmt.Errorf("list audio devices failed")
	}
	var out []Device
	for _, id := range ids[:n] {
		if C.has_streams(id, inputFlag(s)) == 1 {
			out = append(out, Device{ID: uint32(id), Name: deviceName(id)})
		}
	}
	return out, nil
}

// SetDefaultDevice makes the device with id the default for scope s.
func SetDefaultDevice(s Scope, id uint32) error {
	return osErr("set default "+s.String()+" device", C.set_default_device(inputFlag(s), C.AudioObjectID(id)))
}
