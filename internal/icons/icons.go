package icons

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	_ "image/png"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/chai2010/webp"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/draw"

	"github.com/alexbabintsev/ulanzi-deck-go/assets"
)

const (
	IconWidth    = 196
	IconHeight   = 196
	WideWidth    = 458
	PackIconSize = 128
)

// Format is the encoding of key images uploaded to the device.
type Format int32

const (
	// JPEG works with every firmware.
	JPEG Format = iota
	// WebP is about 45% smaller than JPEG at the same quality for widget
	// images. Firmware 5.3.6 renders it; older firmware may not.
	WebP
)

// Extension is the file extension used inside page ZIPs.
func (f Format) Extension() string {
	if f == WebP {
		return "webp"
	}
	return "jpg"
}

func (f Format) String() string { return f.Extension() }

// DefaultQuality balances size against looks on the 196px keys: the D200
// has limited throughput, and at 75 a typical widget image is ~30%
// smaller than at 90 with no visible difference on the device.
const DefaultQuality = 75

var (
	quality atomic.Int32
	format  atomic.Int32
)

// SetQuality sets the quality (1-100) of key images; out-of-range values
// (including 0) restore the default.
func SetQuality(q int) {
	if q < 1 || q > 100 {
		q = DefaultQuality
	}
	quality.Store(int32(q))
}

// SetFormat selects the image encoding for key images.
func SetFormat(f Format) { format.Store(int32(f)) }

// CurrentFormat reports the encoding key images are produced in.
func CurrentFormat() Format { return Format(format.Load()) }

// Quality reports the effective image quality (1-100).
func Quality() int { return imageQuality() }

func imageQuality() int {
	if q := quality.Load(); q > 0 {
		return int(q)
	}
	return DefaultQuality
}

// encode renders img in the current format and quality.
func encode(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	if CurrentFormat() == WebP {
		err = webp.Encode(&buf, img, &webp.Options{Quality: float32(imageQuality())})
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: imageQuality()})
	}
	if err != nil {
		return nil, fmt.Errorf("encode icon as %s: %w", CurrentFormat(), err)
	}
	return buf.Bytes(), nil
}

type Insets struct {
	Top    int
	Right  int
	Bottom int
	Left   int
}

// SlotDimensions returns the physical pixel size for a button slot.
// The D200 uses 196×196 buttons everywhere except slot [3,2], which is
// a double-wide 458×196 display.
func SlotDimensions(col, row int) (width, height int) {
	if col == 3 && row == 2 {
		return WideWidth, IconHeight
	}
	return IconWidth, IconHeight
}

// LoadForButton reads an image file and resizes it to the physical pixel
// size of the target button slot before encoding it in the current
// format. Results are cached (see iconCache).
func LoadForButton(path string, margins Insets, at [2]int, iconColor string) ([]byte, error) {
	width, height := SlotDimensions(at[0], at[1])
	key := cacheKey{
		path: path, margins: margins, width: width, height: height,
		color: iconColor, format: CurrentFormat(), quality: imageQuality(),
	}
	// Pack icons are embedded and never change; files are re-encoded
	// when they do (widgets rewrite theirs on every update).
	var info os.FileInfo
	if !IsPackIcon(path) {
		st, err := os.Stat(expandTilde(path))
		if err != nil {
			return nil, fmt.Errorf("open icon %q: %w", expandTilde(path), err)
		}
		info = st
	}
	if data, ok := iconCache.get(key, info); ok {
		return data, nil
	}
	data, err := loadSized(path, margins, width, height, iconColor)
	if err != nil {
		return nil, err
	}
	iconCache.put(key, info, data)
	return data, nil
}

// iconCacheMax bounds the cache: static icons of all pages plus one
// entry per widget, whose entry is replaced on every update.
const iconCacheMax = 512

type cacheKey struct {
	path          string
	margins       Insets
	width, height int
	color         string
	format        Format
	quality       int
}

type cacheEntry struct {
	info os.FileInfo // nil for pack icons
	data []byte
	used uint64
}

// iconCache keeps encoded key images so a page switch does not decode,
// scale and re-encode every icon again.
var iconCache = &encodedCache{entries: map[cacheKey]*cacheEntry{}}

type encodedCache struct {
	mu      sync.Mutex
	entries map[cacheKey]*cacheEntry
	clock   uint64
	misses  int // for tests
}

func (c *encodedCache) get(key cacheKey, info os.FileInfo) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || !sameFile(e.info, info) {
		c.misses++
		return nil, false
	}
	c.clock++
	e.used = c.clock
	return e.data, true
}

// put stores data encoded from the file as it was at info (stat taken
// before reading, so a write in between only causes another miss).
func (c *encodedCache) put(key cacheKey, info os.FileInfo, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	c.entries[key] = &cacheEntry{info: info, data: data, used: c.clock}
	if len(c.entries) <= iconCacheMax {
		return
	}
	var oldest cacheKey
	var oldestUsed uint64 = math.MaxUint64
	for k, e := range c.entries {
		if e.used < oldestUsed {
			oldest, oldestUsed = k, e.used
		}
	}
	delete(c.entries, oldest)
}

func sameFile(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.ModTime().Equal(b.ModTime()) && a.Size() == b.Size()
}

func loadSized(path string, margins Insets, width, height int, iconColor string) ([]byte, error) {
	if IsPackIcon(path) {
		return loadPackSized(path, margins, width, height, iconColor)
	}

	path = expandTilde(path)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open icon %q: %w", path, err)
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode icon %q: %w", path, err)
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	stddraw.Draw(dst, dst.Bounds(), image.Black, image.Point{}, stddraw.Src)
	target := image.Rect(
		margins.Left,
		margins.Top,
		width-margins.Right,
		height-margins.Bottom,
	)
	if width != height {
		target = aspectFitRect(target, src.Bounds())
	}
	draw.BiLinear.Scale(dst, target, src, src.Bounds(), draw.Over, nil)

	return encode(dst)
}

func aspectFitRect(target image.Rectangle, src image.Rectangle) image.Rectangle {
	if target.Empty() || src.Empty() {
		return target
	}

	targetW := float64(target.Dx())
	targetH := float64(target.Dy())
	srcW := float64(src.Dx())
	srcH := float64(src.Dy())
	scale := math.Min(targetW/srcW, targetH/srcH)
	if scale <= 0 {
		return target
	}

	fittedW := int(math.Round(srcW * scale))
	fittedH := int(math.Round(srcH * scale))
	if fittedW < 1 {
		fittedW = 1
	}
	if fittedH < 1 {
		fittedH = 1
	}

	left := target.Min.X + (target.Dx()-fittedW)/2
	top := target.Min.Y + (target.Dy()-fittedH)/2
	return image.Rect(left, top, left+fittedW, top+fittedH)
}

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

type iconRef struct {
	Pack string
	Name string
}

func IsPackIcon(id string) bool {
	_, ok := parseIconRef(id)
	return ok
}

func parseIconRef(id string) (iconRef, bool) {
	pack, name, ok := strings.Cut(strings.TrimSpace(id), "/")
	if !ok || pack == "" || name == "" {
		return iconRef{}, false
	}
	switch pack {
	case "lucide":
		return iconRef{Pack: pack, Name: name}, true
	default:
		return iconRef{}, false
	}
}

func loadPackSized(id string, margins Insets, width, height int, iconColor string) ([]byte, error) {
	ref, ok := parseIconRef(id)
	if !ok {
		return nil, fmt.Errorf("unknown icon pack id %q", id)
	}

	switch ref.Pack {
	case "lucide":
		return loadLucideSized(ref.Name, margins, width, height, iconColor)
	default:
		return nil, fmt.Errorf("unsupported icon pack %q", ref.Pack)
	}
}

func DrawPackIcon(dst stddraw.Image, id string, target image.Rectangle, clr color.Color) error {
	ref, ok := parseIconRef(id)
	if !ok {
		return fmt.Errorf("unknown icon pack id %q", id)
	}
	if target.Empty() {
		return nil
	}

	switch ref.Pack {
	case "lucide":
		return drawLucideIcon(dst, ref.Name, target, clr)
	default:
		return fmt.Errorf("unsupported icon pack %q", ref.Pack)
	}
}

func loadLucideSized(name string, _ Insets, width, height int, iconColor string) ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	stddraw.Draw(dst, dst.Bounds(), image.Black, image.Point{}, stddraw.Src)

	size := PackIconSize
	if width < size {
		size = width
	}
	if height < size {
		size = height
	}
	left := (width - size) / 2
	top := (height - size) / 2
	target := image.Rect(left, top, left+size, top+size)

	if err := drawLucideIcon(dst, name, target, parseIconColor(iconColor)); err != nil {
		return nil, err
	}

	return encode(dst)
}

func drawLucideIcon(dst stddraw.Image, name string, target image.Rectangle, clr color.Color) error {
	scale := lucideStrokeScale(target)
	icon, err := readLucideIcon(name, clr, scale)
	if err != nil {
		return err
	}

	// Lucide icons use a 24x24 viewBox. By rendering into the requested
	// target rectangle, the SVG transform naturally scales both geometry
	// and stroke width relative to the final icon size.
	canvas := image.NewRGBA(image.Rect(0, 0, target.Dx(), target.Dy()))
	icon.SetTarget(0, 0, float64(target.Dx()), float64(target.Dy()))
	scanner := rasterx.NewScannerGV(target.Dx(), target.Dy(), canvas, canvas.Bounds())
	raster := rasterx.NewDasher(target.Dx(), target.Dy(), scanner)
	icon.Draw(raster, 1)
	stddraw.Draw(dst, target, canvas, image.Point{}, stddraw.Over)
	return nil
}

func parseIconColor(s string) color.Color {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return color.White
	}
	var rgb uint32
	if _, err := fmt.Sscanf(s, "%06x", &rgb); err != nil {
		return color.White
	}
	return color.RGBA{
		R: uint8(rgb >> 16),
		G: uint8((rgb >> 8) & 0xff),
		B: uint8(rgb & 0xff),
		A: 0xff,
	}
}

func readLucideIcon(name string, clr color.Color, strokeScale float64) (*oksvg.SvgIcon, error) {
	// fs.ValidPath rejects "..", absolute and empty segments, so a config
	// value cannot reach outside the embedded icon set.
	iconPath := "lucide/icons/" + name + ".svg"
	if !fs.ValidPath(iconPath) || strings.Contains(name, "/") {
		return nil, fmt.Errorf("invalid lucide icon name %q", name)
	}
	data, err := assets.Lucide.ReadFile(iconPath)
	if err != nil {
		return nil, fmt.Errorf("read lucide icon %q: %w", name, err)
	}

	svg := normalizeLucideSVG(data, clr, strokeScale)
	icon, err := oksvg.ReadIconStream(strings.NewReader(svg))
	if err != nil {
		return nil, fmt.Errorf("parse lucide icon %q: %w", name, err)
	}
	return icon, nil
}

func normalizeLucideSVG(data []byte, clr color.Color, strokeScale float64) string {
	svg := string(data)
	if idx := strings.Index(svg, "<svg"); idx >= 0 {
		svg = svg[idx:]
	}
	svg = strings.ReplaceAll(svg, "currentColor", hexColor(clr))
	if strokeScale > 1 {
		svg = strings.ReplaceAll(svg, `stroke-width="2"`, fmt.Sprintf(`stroke-width="%.3f"`, 2*strokeScale))
	}
	return svg
}

func lucideStrokeScale(target image.Rectangle) float64 {
	size := target.Dx()
	if target.Dy() < size {
		size = target.Dy()
	}
	if size <= 24 {
		return 1
	}
	return float64(size) / 24.0
}

func hexColor(clr color.Color) string {
	r, g, b, _ := clr.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}
