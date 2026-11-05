//go:build darwin

package widgets

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/mainloop"
)

// TestMain serves the main run loop like the daemon does, so
// distributed notifications reach the tests.
func TestMain(m *testing.M) { mainloop.Run(m.Run) }

func TestSpotifyNotifierFansOutAndCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a, err := startSpotifyNotifier(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	b, _ := startSpotifyNotifier(ctx2)

	goSpotifyNotification()
	for _, ch := range []<-chan struct{}{a, b} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("subscriber not notified")
		}
	}

	cancel()
	select {
	case _, ok := <-a:
		if ok {
			t.Fatal("expected closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after cancel")
	}
}

// TestSpotifyNotifierReceivesDistributedNotification posts the real
// notification from another process. Opt-in: it talks to the user's
// notification daemon (and wakes any running udeck).
func TestSpotifyNotifierReceivesDistributedNotification(t *testing.T) {
	if os.Getenv("UDECK_E2E") == "" {
		t.Skip("set UDECK_E2E=1 to post a real distributed notification")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := startSpotifyNotifier(ctx)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // let the observer register
	post := `ObjC.import("Foundation"); $.NSDistributedNotificationCenter.defaultCenter.postNotificationNameObjectUserInfoDeliverImmediately("com.spotify.client.PlaybackStateChanged", $(), $(), true)`
	if out, err := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", post).CombinedOutput(); err != nil {
		t.Fatalf("post: %v: %s", err, out)
	}
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("distributed notification not delivered")
	}
}
