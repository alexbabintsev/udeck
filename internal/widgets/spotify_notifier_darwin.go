//go:build darwin

package widgets

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>

extern void goSpotifyNotification(void);

static void spotify_cb(CFNotificationCenterRef center, void *observer,
		CFNotificationName name, const void *object, CFDictionaryRef info) {
	goSpotifyNotification();
}

static int spotify_observer_token;

static void spotify_observe(void) {
	CFNotificationCenterAddObserver(CFNotificationCenterGetDistributedCenter(),
		&spotify_observer_token, spotify_cb,
		CFSTR("com.spotify.client.PlaybackStateChanged"), NULL,
		CFNotificationSuspensionBehaviorDeliverImmediately);
}
*/
import "C"

import (
	"context"
	"sync"
)

// spotifyNotifications fans Spotify's distributed notifications out to
// the running SpotifyServices. The observer is registered once for the
// life of the process, since services come and go with config reloads.
//
// Distributed notifications arrive through the main run loop, which
// the daemon serves via internal/mainloop. Without it (tests) nothing
// is delivered and the service relies on polling alone.
var spotifyNotifications struct {
	once sync.Once
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

//export goSpotifyNotification
func goSpotifyNotification() {
	n := &spotifyNotifications
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch := range n.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// startSpotifyNotifier delivers a value whenever Spotify's playback state
// changes, until ctx is done; then the channel is closed.
func startSpotifyNotifier(ctx context.Context) (<-chan struct{}, error) {
	n := &spotifyNotifications
	n.once.Do(func() {
		n.subs = map[chan struct{}]struct{}{}
		C.spotify_observe()
	})
	ch := make(chan struct{}, 16)
	n.mu.Lock()
	n.subs[ch] = struct{}{}
	n.mu.Unlock()
	go func() {
		<-ctx.Done()
		n.mu.Lock()
		delete(n.subs, ch)
		n.mu.Unlock()
		close(ch)
	}()
	return ch, nil
}
