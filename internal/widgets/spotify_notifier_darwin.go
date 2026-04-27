//go:build darwin

package widgets

import (
	"bufio"
	"context"
	"os"
	"os/exec"
)

const spotifyWatcherSwift = `import Foundation

final class Observer: NSObject {
    @objc func handle(_ note: Notification) {
        print("spotify")
        fflush(stdout)
    }
}

let observer = Observer()
DistributedNotificationCenter.default().addObserver(
    observer,
    selector: #selector(Observer.handle(_:)),
    name: Notification.Name("com.spotify.client.PlaybackStateChanged"),
    object: nil
)
RunLoop.main.run()
`

func startSpotifyNotifier(ctx context.Context) (<-chan struct{}, error) {
	if _, err := os.Stat("/usr/bin/swift"); err != nil {
		return nil, err
	}
	script, err := os.CreateTemp("", "ulanzi-spotify-watcher-*.swift")
	if err != nil {
		return nil, err
	}
	if _, err := script.WriteString(spotifyWatcherSwift); err != nil {
		script.Close()
		_ = os.Remove(script.Name())
		return nil, err
	}
	if err := script.Close(); err != nil {
		_ = os.Remove(script.Name())
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "/usr/bin/swift", script.Name())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.Remove(script.Name())
		return nil, err
	}
	cmd.Stderr = ioDiscard{}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(script.Name())
		return nil, err
	}

	ch := make(chan struct{}, 16)
	go func() {
		defer close(ch)
		defer cmd.Wait()
		defer os.Remove(script.Name())

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}()
	return ch, nil
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
