package macos

import "fmt"

// MediaKey is a system media key, as on Apple keyboards. The frontmost
// "now playing" app receives it: Music, Spotify, a browser tab, ...
type MediaKey int

// NX_KEYTYPE_* values from IOKit/hidsystem/ev_keymap.h.
const (
	MediaPlayPause MediaKey = 16
	MediaNext      MediaKey = 17
	MediaPrevious  MediaKey = 18
)

// ParseMediaKey maps play_pause, next and previous to keys.
func ParseMediaKey(name string) (MediaKey, error) {
	switch name {
	case "play_pause":
		return MediaPlayPause, nil
	case "next":
		return MediaNext, nil
	case "previous":
		return MediaPrevious, nil
	}
	return 0, fmt.Errorf("unknown media key %q (play_pause|next|previous)", name)
}
