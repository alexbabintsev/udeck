//go:build darwin

package macos

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework ApplicationServices
#import <AppKit/AppKit.h>

// post_media_key sends a press and release of an NX_KEYTYPE_* key as a
// system-defined event, the way the keyboard's media keys do.
static int post_media_key(int key) {
	@autoreleasepool {
		for (int down = 1; down >= 0; down--) {
			NSEvent *ev = [NSEvent otherEventWithType:NSEventTypeSystemDefined
				location:NSZeroPoint
				modifierFlags:(down ? 0xa00 : 0xb00)
				timestamp:0
				windowNumber:0
				context:nil
				subtype:8
				data1:((key << 16) | ((down ? 0xa : 0xb) << 8))
				data2:-1];
			CGEventRef cg = [ev CGEvent];
			if (cg == NULL) {
				return 1;
			}
			CGEventPost(kCGHIDEventTap, cg);
		}
	}
	return 0;
}

static int ax_trusted(void) { return AXIsProcessTrusted() ? 1 : 0; }
*/
import "C"

import "errors"

// PressMediaKey sends k. Like key actions it needs Accessibility access
// for the app running udeck.
func PressMediaKey(k MediaKey) error {
	if C.post_media_key(C.int(k)) != 0 {
		return errors.New("post media key failed")
	}
	return nil
}

// AccessibilityTrusted reports whether this process may post input
// events (System Settings → Privacy & Security → Accessibility).
func AccessibilityTrusted() bool { return C.ax_trusted() == 1 }
