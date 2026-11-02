package icons

import (
	"bytes"
	"fmt"
	"image"
	"strings"
	"sync"
	"sync/atomic"
)

// memPrefix marks icon references to images held in memory. Widgets
// render into them, so a key image never has to round-trip through a
// PNG file.
const memPrefix = "mem:"

type memImage struct {
	img     image.Image
	version uint64
}

var (
	memMu     sync.RWMutex
	memImages = map[string]memImage{}
	memIDs    atomic.Uint64
	memClock  atomic.Uint64
)

// NewMemRef returns a fresh, process-unique reference for in-memory
// images; name only makes logs readable.
func NewMemRef(name string) string {
	return fmt.Sprintf("%s%s-%d", memPrefix, name, memIDs.Add(1))
}

// IsMemIcon reports whether ref names an in-memory image.
func IsMemIcon(ref string) bool { return strings.HasPrefix(ref, memPrefix) }

// PutImage stores img under ref and reports whether it differs from the
// image stored before. img must not be modified afterwards.
func PutImage(ref string, img image.Image) (changed bool) {
	memMu.Lock()
	defer memMu.Unlock()
	if prev, ok := memImages[ref]; ok && sameImage(prev.img, img) {
		return false
	}
	memImages[ref] = memImage{img: img, version: memClock.Add(1)}
	return true
}

// DeleteImage forgets ref.
func DeleteImage(ref string) {
	memMu.Lock()
	defer memMu.Unlock()
	delete(memImages, ref)
}

// Image returns the image stored under ref.
func Image(ref string) (image.Image, bool) {
	memMu.RLock()
	defer memMu.RUnlock()
	m, ok := memImages[ref]
	return m.img, ok
}

// MemVersion identifies the current content of ref; it changes whenever
// PutImage stores a different image.
func MemVersion(ref string) (uint64, bool) {
	memMu.RLock()
	defer memMu.RUnlock()
	m, ok := memImages[ref]
	return m.version, ok
}

func sameImage(a, b image.Image) bool {
	ra, ok1 := a.(*image.RGBA)
	rb, ok2 := b.(*image.RGBA)
	if !ok1 || !ok2 {
		return false
	}
	return ra.Rect == rb.Rect && ra.Stride == rb.Stride && bytes.Equal(ra.Pix, rb.Pix)
}
