package actions

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

int ax_is_trusted() {
	return AXIsProcessTrusted() ? 1 : 0;
}

int send_shortcut(CGKeyCode *modifierCodes, CGEventFlags *modifierFlags, int modifierCount, CGKeyCode keyCode) {
	CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStateCombinedSessionState);
	if (src == NULL) {
		return 1;
	}

	CGEventSourceSetLocalEventsSuppressionInterval(src, 0.0);

	CGEventFlags activeFlags = 0;
	for (int i = 0; i < modifierCount; i++) {
		activeFlags |= modifierFlags[i];
		CGEventRef down = CGEventCreateKeyboardEvent(src, modifierCodes[i], true);
		if (down == NULL) {
			CFRelease(src);
			return 2;
		}
		CGEventSetFlags(down, activeFlags);
		CGEventPost(kCGAnnotatedSessionEventTap, down);
		CFRelease(down);
	}

	CGEventRef keyDown = CGEventCreateKeyboardEvent(src, keyCode, true);
	if (keyDown == NULL) {
		CFRelease(src);
		return 3;
	}
	CGEventSetFlags(keyDown, activeFlags);
	CGEventPost(kCGAnnotatedSessionEventTap, keyDown);
	CFRelease(keyDown);

	CGEventRef keyUp = CGEventCreateKeyboardEvent(src, keyCode, false);
	if (keyUp == NULL) {
		CFRelease(src);
		return 4;
	}
	CGEventSetFlags(keyUp, activeFlags);
	CGEventPost(kCGAnnotatedSessionEventTap, keyUp);
	CFRelease(keyUp);

	for (int i = modifierCount - 1; i >= 0; i--) {
		CGEventRef up = CGEventCreateKeyboardEvent(src, modifierCodes[i], false);
		if (up == NULL) {
			CFRelease(src);
			return 5;
		}
		CGEventSetFlags(up, activeFlags);
		CGEventPost(kCGAnnotatedSessionEventTap, up);
		CFRelease(up);
		activeFlags &= ~modifierFlags[i];
	}

	CFRelease(src);
	return 0;
}
*/
import "C"

import (
	"fmt"
	"strings"
)

type keyModifier struct {
	code int
	flag C.CGEventFlags
}

type parsedShortcut struct {
	keyCode   int
	modifiers []keyModifier
}

var isAXTrusted = func() bool {
	return C.ax_is_trusted() != 0
}

var sendShortcutFunc = func(spec parsedShortcut) error {
	var rc C.int
	if len(spec.modifiers) == 0 {
		rc = C.send_shortcut(nil, nil, 0, C.CGKeyCode(spec.keyCode))
	} else {
		modCodes := make([]C.CGKeyCode, len(spec.modifiers))
		modFlags := make([]C.CGEventFlags, len(spec.modifiers))
		for i := range spec.modifiers {
			modCodes[i] = C.CGKeyCode(spec.modifiers[i].code)
			modFlags[i] = spec.modifiers[i].flag
		}
		rc = C.send_shortcut(
			(*C.CGKeyCode)(&modCodes[0]),
			(*C.CGEventFlags)(&modFlags[0]),
			C.int(len(spec.modifiers)),
			C.CGKeyCode(spec.keyCode),
		)
	}
	if rc != 0 {
		return fmt.Errorf("post macOS keyboard shortcut failed: code %d", int(rc))
	}
	return nil
}

// sendKey parses "key" string like "cmd shift s" or "f5" and fires a macOS shortcut.
func sendKey(key string) error {
	if !isAXTrusted() {
		return fmt.Errorf("key actions require macOS Accessibility permission for the app running ulanzi-deck")
	}

	spec, err := parseShortcut(key)
	if err != nil {
		return err
	}
	return sendShortcutFunc(spec)
}

func parseShortcut(key string) (parsedShortcut, error) {
	parts := strings.Fields(strings.ToLower(key))
	if len(parts) == 0 {
		return parsedShortcut{}, fmt.Errorf("empty key spec")
	}

	var (
		keyName string
		seenMod = map[string]bool{}
		spec    parsedShortcut
	)

	for _, raw := range parts {
		p := normalizeKeyToken(raw)
		if mod, ok := modifierKeys[p]; ok {
			if !seenMod[p] {
				spec.modifiers = append(spec.modifiers, mod)
				seenMod[p] = true
			}
			continue
		}
		if keyName != "" {
			return parsedShortcut{}, fmt.Errorf("multiple key names in spec %q", key)
		}
		keyName = p
	}

	if keyName == "" {
		return parsedShortcut{}, fmt.Errorf("no key name in spec %q", key)
	}

	code, ok := keyCodes[keyName]
	if !ok {
		return parsedShortcut{}, fmt.Errorf("unknown key name %q", keyName)
	}
	spec.keyCode = code
	return spec, nil
}

func normalizeKeyToken(token string) string {
	switch token {
	case "command":
		return "cmd"
	case "control":
		return "ctrl"
	case "option":
		return "alt"
	case "enter":
		return "return"
	default:
		return token
	}
}

var modifierKeys = map[string]keyModifier{
	"cmd":   {code: 55, flag: C.kCGEventFlagMaskCommand},
	"shift": {code: 56, flag: C.kCGEventFlagMaskShift},
	"alt":   {code: 58, flag: C.kCGEventFlagMaskAlternate},
	"ctrl":  {code: 59, flag: C.kCGEventFlagMaskControl},
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
	"f1":       122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97,
	"f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
	"home": 115, "pageup": 116, "end": 119, "pagedown": 121,
	"left": 123, "right": 124, "down": 125, "up": 126,
}
