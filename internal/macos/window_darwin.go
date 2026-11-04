//go:build darwin

package macos

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework ApplicationServices
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>

enum { WIN_OK = 0, WIN_NO_APP = 1, WIN_NO_WINDOW = 2, WIN_FAILED = 3, WIN_UNTRUSTED = 4 };

// focused_window returns the frontmost app's focused window, retained.
static int focused_window(AXUIElementRef *app_out, AXUIElementRef *win_out, int *pid_out) {
	if (!AXIsProcessTrusted()) return WIN_UNTRUSTED;
	pid_t pid;
	@autoreleasepool {
		NSRunningApplication *front = [[NSWorkspace sharedWorkspace] frontmostApplication];
		if (front == nil) return WIN_NO_APP;
		pid = front.processIdentifier;
	}
	AXUIElementRef app = AXUIElementCreateApplication(pid);
	if (app == NULL) return WIN_NO_APP;
	CFTypeRef win = NULL;
	if (AXUIElementCopyAttributeValue(app, kAXFocusedWindowAttribute, &win) != kAXErrorSuccess || win == NULL) {
		CFRelease(app);
		return WIN_NO_WINDOW;
	}
	*app_out = app;
	*win_out = (AXUIElementRef)win;
	*pid_out = pid;
	return WIN_OK;
}

static int read_frame(AXUIElementRef win, double *x, double *y, double *w, double *h) {
	CFTypeRef pos = NULL, size = NULL;
	CGPoint p; CGSize s;
	int ok = AXUIElementCopyAttributeValue(win, kAXPositionAttribute, &pos) == kAXErrorSuccess &&
		AXUIElementCopyAttributeValue(win, kAXSizeAttribute, &size) == kAXErrorSuccess &&
		AXValueGetValue(pos, kAXValueCGPointType, &p) && AXValueGetValue(size, kAXValueCGSizeType, &s);
	if (pos) CFRelease(pos);
	if (size) CFRelease(size);
	if (!ok) return WIN_FAILED;
	*x = p.x; *y = p.y; *w = s.width; *h = s.height;
	return WIN_OK;
}

static int window_frame(double *x, double *y, double *w, double *h, int *pid) {
	AXUIElementRef app, win;
	int rc = focused_window(&app, &win, pid);
	if (rc != WIN_OK) return rc;
	rc = read_frame(win, x, y, w, h);
	CFRelease(win);
	CFRelease(app);
	return rc;
}

static int set_size(AXUIElementRef win, double w, double h) {
	CGSize s = CGSizeMake(w, h);
	AXValueRef v = AXValueCreate(kAXValueCGSizeType, &s);
	AXError err = AXUIElementSetAttributeValue(win, kAXSizeAttribute, v);
	CFRelease(v);
	return err == kAXErrorSuccess;
}

static int set_position(AXUIElementRef win, double x, double y) {
	CGPoint p = CGPointMake(x, y);
	AXValueRef v = AXValueCreate(kAXValueCGPointType, &p);
	AXError err = AXUIElementSetAttributeValue(win, kAXPositionAttribute, v);
	CFRelease(v);
	return err == kAXErrorSuccess;
}

// set_window_frame moves and resizes the focused window. The size is set
// before and after the move: before, so the window fits where it goes;
// after, because a display may have capped it on the way.
// AXEnhancedUserInterface (turned on by assistive apps) makes apps
// animate and ignore sizes, so it is off while the frame is set.
static int set_window_frame(double x, double y, double w, double h) {
	AXUIElementRef app, win;
	int pid;
	int rc = focused_window(&app, &win, &pid);
	if (rc != WIN_OK) return rc;
	CFStringRef enhanced = CFSTR("AXEnhancedUserInterface");
	CFTypeRef was = NULL;
	int restoreEnhanced = AXUIElementCopyAttributeValue(app, enhanced, &was) == kAXErrorSuccess &&
		was != NULL && CFBooleanGetValue(was);
	if (was) CFRelease(was);
	if (restoreEnhanced) AXUIElementSetAttributeValue(app, enhanced, kCFBooleanFalse);
	int ok = set_size(win, w, h);
	ok = set_position(win, x, y) && ok;
	ok = set_size(win, w, h) && ok;
	if (restoreEnhanced) AXUIElementSetAttributeValue(app, enhanced, kCFBooleanTrue);
	CFRelease(win);
	CFRelease(app);
	return ok ? WIN_OK : WIN_FAILED;
}

// screens writes each display's frame and visible frame (8 doubles) in
// Accessibility coordinates: AppKit puts the origin at the bottom-left
// of the main display with y up, Accessibility at its top-left, y down.
static int screens(double *out, int max) {
	int n = 0;
	@autoreleasepool {
		NSArray<NSScreen *> *list = [NSScreen screens];
		if (list.count == 0) return 0;
		CGFloat mainH = list[0].frame.size.height;
		for (NSScreen *s in list) {
			if (n >= max) break;
			NSRect f = s.frame, v = s.visibleFrame;
			double *o = out + n * 8;
			o[0] = f.origin.x; o[1] = mainH - f.origin.y - f.size.height; o[2] = f.size.width; o[3] = f.size.height;
			o[4] = v.origin.x; o[5] = mainH - v.origin.y - v.size.height; o[6] = v.size.width; o[7] = v.size.height;
			n++;
		}
	}
	return n;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func windowErr(rc C.int) error {
	switch rc {
	case C.WIN_OK:
		return nil
	case C.WIN_UNTRUSTED:
		return ErrNotTrusted
	case C.WIN_NO_APP, C.WIN_NO_WINDOW:
		return ErrNoWindow
	default:
		return ErrWindowLocked
	}
}

// FocusedWindow returns the frame of the frontmost app's focused window
// and the app's process ID.
func FocusedWindow() (Rect, int, error) {
	var x, y, w, h C.double
	var pid C.int
	if err := windowErr(C.window_frame(&x, &y, &w, &h, &pid)); err != nil {
		return Rect{}, 0, err
	}
	return Rect{float64(x), float64(y), float64(w), float64(h)}, int(pid), nil
}

// SetFocusedWindow moves and resizes the frontmost app's focused window.
func SetFocusedWindow(r Rect) error {
	return windowErr(C.set_window_frame(C.double(r.X), C.double(r.Y), C.double(r.W), C.double(r.H)))
}

// Screens returns the displays, the main one first.
func Screens() ([]Screen, error) {
	const max = 16
	buf := make([]C.double, max*8)
	n := int(C.screens((*C.double)(unsafe.Pointer(&buf[0])), max))
	if n == 0 {
		return nil, errors.New("no displays")
	}
	out := make([]Screen, n)
	for i := range out {
		o := buf[i*8:]
		out[i] = Screen{
			Frame:   Rect{float64(o[0]), float64(o[1]), float64(o[2]), float64(o[3])},
			Visible: Rect{float64(o[4]), float64(o[5]), float64(o[6]), float64(o[7])},
		}
	}
	return out, nil
}
