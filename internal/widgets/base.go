package widgets

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// widgetBase is the lifecycle every widget shares: a private temp
// directory holding the rendered icon, change notification towards the
// Manager, and background loops that stop as soon as the widget is
// closed (not on their next tick, which for slow pollers can be minutes
// after a config reload replaced the widget).
type widgetBase struct {
	id       string
	iconPath string
	notify   func(string)

	done      chan struct{}
	closeOnce sync.Once
}

func newWidgetBase(id, kind, filename string, notify func(string)) (*widgetBase, error) {
	dir, err := os.MkdirTemp("", "ulanzi-"+kind+"-")
	if err != nil {
		return nil, fmt.Errorf("create %s widget temp dir: %w", kind, err)
	}
	if notify == nil {
		notify = func(string) {}
	}
	return &widgetBase{
		id:       id,
		iconPath: filepath.Join(dir, filename),
		notify:   notify,
		done:     make(chan struct{}),
	}, nil
}

// IconPath returns the file the widget renders into.
func (b *widgetBase) IconPath() string { return b.iconPath }

// Close stops the widget's loops and removes its icon directory. It is
// safe to call more than once.
func (b *widgetBase) Close() error {
	b.closeOnce.Do(func() { close(b.done) })
	return os.RemoveAll(filepath.Dir(b.iconPath))
}

// isClosed reports whether Close has been called.
func (b *widgetBase) isClosed() bool {
	select {
	case <-b.done:
		return true
	default:
		return false
	}
}

// writeIcon atomically stores img and reports whether the file changed.
// It does not notify; use it for the initial render in constructors.
func (b *widgetBase) writeIcon(img image.Image) (bool, error) {
	if b.isClosed() {
		return false, nil
	}
	return writePNGAtomicIfChanged(b.iconPath, img)
}

// publish stores img and tells the Manager when the icon changed.
func (b *widgetBase) publish(img image.Image) (bool, error) {
	changed, err := b.writeIcon(img)
	if err != nil {
		return false, err
	}
	if changed {
		b.notify(b.id)
	}
	return changed, nil
}

// every runs fn on its own goroutine every interval until Close.
func (b *widgetBase) every(interval time.Duration, fn func(now time.Time)) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-b.done:
				return
			case now := <-ticker.C:
				fn(now)
			}
		}
	}()
}
