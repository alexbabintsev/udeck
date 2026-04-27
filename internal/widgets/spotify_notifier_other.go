//go:build !darwin

package widgets

import (
	"context"
	"errors"
)

func startSpotifyNotifier(context.Context) (<-chan struct{}, error) {
	return nil, errors.New("spotify notifications unsupported on this platform")
}
