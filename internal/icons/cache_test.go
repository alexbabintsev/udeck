package icons

import (
	"bytes"
	"image/color"
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
		c.put(key(i), nil, []byte{byte(i)})
	}
	c.get(key(0), nil) // key 0 is now the most recent
	c.put(key(iconCacheMax), nil, nil)
	if len(c.entries) != iconCacheMax {
		t.Fatalf("size %d", len(c.entries))
	}
	if _, ok := c.get(key(0), nil); !ok {
		t.Fatal("recently used entry evicted")
	}
	if _, ok := c.get(key(1), nil); ok {
		t.Fatal("least recently used entry kept")
	}
	if sameFile(nil, nil) != true {
		t.Fatal("pack icons have no file info")
	}
}
