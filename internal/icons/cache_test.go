package icons

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"testing"
)

func cacheMisses() int {
	iconCache.mu.Lock()
	defer iconCache.mu.Unlock()
	return iconCache.misses
}

func TestLoadForButtonCachesUntilTheFileChanges(t *testing.T) {
	SetFormat(JPEG)
	path := filepath.Join(t.TempDir(), "icon.png")
	writeSolidPNG(t, path, color.RGBA{255, 0, 0, 255})

	before := cacheMisses()
	first, err := LoadForButton(path, Insets{}, [2]int{0, 0}, "")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := LoadForButton(path, Insets{}, [2]int{0, 0}, "")
	if cacheMisses() != before+1 || !bytes.Equal(first, again) {
		t.Fatalf("second load must be a cache hit (misses %d -> %d)", before, cacheMisses())
	}

	// Widgets replace their icon file atomically: a new file is a miss.
	tmp := path + ".new"
	writeSolidPNG(t, tmp, color.RGBA{0, 0, 255, 255})
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	changed, _ := LoadForButton(path, Insets{}, [2]int{0, 0}, "")
	if cacheMisses() != before+2 || bytes.Equal(first, changed) {
		t.Fatal("a rewritten file must be re-encoded")
	}

	// Format, slot size and margins are part of the key.
	SetFormat(WebP)
	defer SetFormat(JPEG)
	if _, err := LoadForButton(path, Insets{}, [2]int{0, 0}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadForButton(path, Insets{Top: 4}, [2]int{3, 2}, ""); err != nil {
		t.Fatal(err)
	}
	if cacheMisses() != before+4 {
		t.Fatalf("format/size/margins must miss, misses %d", cacheMisses()-before)
	}

	if _, err := LoadForButton(filepath.Join(t.TempDir(), "missing.png"), Insets{}, [2]int{0, 0}, ""); err == nil {
		t.Fatal("missing file must fail")
	}
}

func TestLoadForButtonCachesPackIcons(t *testing.T) {
	before := cacheMisses()
	for i := 0; i < 3; i++ {
		if _, err := LoadForButton("lucide/anchor", Insets{}, [2]int{1, 1}, "ff8000"); err != nil {
			t.Fatal(err)
		}
	}
	if cacheMisses() != before+1 {
		t.Fatalf("pack icon must be encoded once, misses %d", cacheMisses()-before)
	}
}

func TestEncodedCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := &encodedCache{entries: map[cacheKey]*cacheEntry{}}
	key := func(i int) cacheKey { return cacheKey{width: i} }
	for i := 0; i < iconCacheMax; i++ {
		c.put(key(i), cacheStamp{}, []byte{byte(i)})
	}
	c.get(key(0), cacheStamp{}) // key 0 is now the most recent
	c.put(key(iconCacheMax), cacheStamp{}, nil)
	if len(c.entries) != iconCacheMax {
		t.Fatalf("size %d", len(c.entries))
	}
	if _, ok := c.get(key(0), cacheStamp{}); !ok {
		t.Fatal("recently used entry evicted")
	}
	if _, ok := c.get(key(1), cacheStamp{}); ok {
		t.Fatal("least recently used entry kept")
	}
	if sameFile(nil, nil) != true {
		t.Fatal("pack icons have no file info")
	}
}

func TestMemIconsAreStoredEncodedAndCached(t *testing.T) {
	SetFormat(JPEG)
	ref := NewMemRef("test")
	if !IsMemIcon(ref) || IsPackIcon(ref) {
		t.Fatalf("ref %q", ref)
	}
	if _, err := LoadForButton(ref, Insets{}, [2]int{0, 0}, ""); err == nil {
		t.Fatal("unknown image must fail")
	}
	solid := func(c color.RGBA) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
		return img
	}
	if !PutImage(ref, solid(color.RGBA{255, 0, 0, 255})) {
		t.Fatal("first put must report a change")
	}
	if PutImage(ref, solid(color.RGBA{255, 0, 0, 255})) {
		t.Fatal("identical pixels must not count as a change")
	}
	v1, _ := MemVersion(ref)
	before := cacheMisses()
	red, err := LoadForButton(ref, Insets{}, [2]int{0, 0}, "")
	if err != nil {
		t.Fatal(err)
	}
	LoadForButton(ref, Insets{}, [2]int{0, 0}, "")
	if cacheMisses() != before+1 {
		t.Fatal("unchanged image must be encoded once")
	}
	PutImage(ref, solid(color.RGBA{0, 0, 255, 255}))
	if v2, _ := MemVersion(ref); v2 == v1 {
		t.Fatal("version must change with the content")
	}
	blue, _ := LoadForButton(ref, Insets{}, [2]int{0, 0}, "")
	if bytes.Equal(red, blue) || cacheMisses() != before+2 {
		t.Fatal("changed image must be re-encoded")
	}
	DeleteImage(ref)
	if _, ok := Image(ref); ok {
		t.Fatal("deleted image still present")
	}
	if PutImage(ref, image.NewGray(image.Rect(0, 0, 1, 1))) != true || sameImage(image.NewGray(image.Rect(0, 0, 1, 1)), image.NewGray(image.Rect(0, 0, 1, 1))) {
		t.Fatal("non-RGBA images always count as changed")
	}
	DeleteImage(ref)
}
