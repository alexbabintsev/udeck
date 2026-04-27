package actions

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation
#include <CoreGraphics/CoreGraphics.h>

void send_key(CGKeyCode keyCode, CGEventFlags flags) {
    CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
    CGEventRef down = CGEventCreateKeyboardEvent(src, keyCode, true);
    CGEventRef up   = CGEventCreateKeyboardEvent(src, keyCode, false);
    CGEventSetFlags(down, flags);
    CGEventSetFlags(up,   flags);
    CGEventPost(kCGHIDEventTap, down);
    CGEventPost(kCGHIDEventTap, up);
    CFRelease(down);
    CFRelease(up);
    CFRelease(src);
}
*/
import "C"
import (
	"fmt"
	"strings"
)

// sendKey parses "key" string like "cmd shift s" or "f5" and fires CGEvent.
func sendKey(key string) error {
	parts := strings.Fields(strings.ToLower(key))
	if len(parts) == 0 {
		return fmt.Errorf("empty key spec")
	}

	var flags C.CGEventFlags
	var keyName string

	for _, p := range parts {
		switch p {
		case "cmd", "command":
			flags |= C.kCGEventFlagMaskCommand
		case "shift":
			flags |= C.kCGEventFlagMaskShift
		case "alt", "option":
			flags |= C.kCGEventFlagMaskAlternate
		case "ctrl", "control":
			flags |= C.kCGEventFlagMaskControl
		default:
			keyName = p
		}
	}

	if keyName == "" {
		return fmt.Errorf("no key name in spec %q", key)
	}

	code, ok := keyCodes[keyName]
	if !ok {
		return fmt.Errorf("unknown key name %q", keyName)
	}

	C.send_key(C.CGKeyCode(code), flags)
	return nil
}

// keyCodes maps key name strings to macOS virtual key codes.
var keyCodes = map[string]int{
	"a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7,
	"c": 8, "v": 9, "b": 11, "q": 12, "w": 13, "e": 14, "r": 15,
	"y": 16, "t": 17, "1": 18, "2": 19, "3": 20, "4": 21, "6": 22,
	"5": 23, "=": 24, "9": 25, "7": 26, "-": 27, "8": 28, "0": 29,
	"]": 30, "o": 31, "u": 32, "[": 33, "i": 34, "p": 35, "return": 36,
	"l": 37, "j": 38, "'": 39, "k": 40, ";": 41, "\\": 42, ",": 43,
	"/": 44, "n": 45, "m": 46, ".": 47, "tab": 48, "space": 49,
	"`": 50, "backspace": 51, "delete": 51, "escape": 53, "esc": 53,
	"capslock": 57,
	"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97,
	"f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
	"home": 115, "pageup": 116, "end": 119, "pagedown": 121,
	"left": 123, "right": 124, "down": 125, "up": 126,
}
