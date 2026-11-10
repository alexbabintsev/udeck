package widgets

import (
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg" // image() decodes JPEG and PNG files
	_ "image/png"
	"os"
	"strings"

	"github.com/dop251/goja"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"

	"github.com/alexbabintsev/udeck/internal/icons"
)

// scriptTextOptions style canvas.text.
type scriptTextOptions struct {
	Color    string  `json:"color"`
	Size     float64 `json:"size"`
	MinSize  float64 `json:"minSize"`
	Bold     *bool   `json:"bold"`
	Align    string  `json:"align"`  // left | center | right
	Valign   string  `json:"valign"` // top | middle | bottom
	Tracking float64 `json:"tracking"`
}

// newScriptCanvas wraps img for a script's draw(c). Pixel loops stay in
// Go: scripts only describe shapes and text.
func newScriptCanvas(h *scriptHost, img *image.RGBA, images map[string]image.Image) *goja.Object {
	rt := h.rt
	c := rt.NewObject()
	b := img.Bounds()
	_ = c.Set("width", b.Dx())
	_ = c.Set("height", b.Dy())

	colorOf := func(s string, def color.Color) color.Color {
		if strings.TrimSpace(s) == "" {
			return def
		}
		clr, err := parseScriptColor(s)
		if err != nil {
			h.throw("%v", err)
		}
		return clr
	}

	// clear(color = "#000") fills the whole key.
	_ = c.Set("clear", func(clr string) {
		draw.Draw(img, b, &image.Uniform{colorOf(clr, color.Black)}, image.Point{}, draw.Src)
	})
	// rect(x, y, w, h, color, radius = 0)
	_ = c.Set("rect", func(x, y, w, hgt int, clr string, radius int) {
		fillRoundRect(img, image.Rect(x, y, x+w, y+hgt), radius, colorOf(clr, color.White))
	})
	// circle(cx, cy, r, color)
	_ = c.Set("circle", func(cx, cy, r int, clr string) {
		fillCircle(img, cx, cy, r, colorOf(clr, color.White))
	})
	// text(text, x, y, w, h, {color, size, minSize, bold, align, valign,
	// tracking}) writes text in the block, shrinking it to fit its width
	// and shortening it with an ellipsis below minSize.
	_ = c.Set("text", func(call goja.FunctionCall) goja.Value {
		text := call.Argument(0).String()
		if goja.IsUndefined(call.Argument(0)) || goja.IsNull(call.Argument(0)) {
			return goja.Undefined()
		}
		x, y := int(call.Argument(1).ToInteger()), int(call.Argument(2).ToInteger())
		w, hgt := int(call.Argument(3).ToInteger()), int(call.Argument(4).ToInteger())
		var opts scriptTextOptions
		if o := call.Argument(5); !goja.IsUndefined(o) && !goja.IsNull(o) {
			if err := rt.ExportTo(o, &opts); err != nil {
				h.throw("text options: %v", err)
			}
		}
		align, err := scriptTextAlign(opts.Align, opts.Valign)
		if err != nil {
			h.throw("%v", err)
		}
		size := opts.Size
		if size <= 0 {
			size = 28
		}
		minSize := opts.MinSize
		if minSize <= 0 {
			minSize = min(textMinSize, size)
		}
		bold := opts.Bold == nil || *opts.Bold
		drawTextBlock(img, text, x, y, w, hgt, align, colorOf(opts.Color, color.White), size, minSize, bold, opts.Tracking)
		return goja.Undefined()
	})
	// measure(text, size = 28, bold = true) is the width of text in pixels.
	_ = c.Set("measure", func(call goja.FunctionCall) goja.Value {
		size := call.Argument(1).ToFloat()
		if size <= 0 {
			size = 28
		}
		bold := goja.IsUndefined(call.Argument(2)) || call.Argument(2).ToBoolean()
		ft := uiFont(bold)
		if ft == nil {
			return rt.ToValue(0)
		}
		face, err := opentype.NewFace(ft, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return rt.ToValue(0)
		}
		defer face.Close()
		return rt.ToValue(font.MeasureString(face, call.Argument(0).String()).Round())
	})
	// icon(id, x, y, w, h, color) draws a pack icon such as lucide/play.
	_ = c.Set("icon", func(id string, x, y, w, hgt int, clr string) {
		if !icons.IsPackIcon(id) {
			h.throw("icon %q is not a pack icon (e.g. lucide/play)", id)
		}
		drawIcon(img, id, FromLeft(x), FromTop(y), w, hgt, colorOf(clr, color.White))
	})
	// image(path, x, y, w, h) draws a PNG or JPEG file scaled into the
	// block and reports whether it could; files are read once per key.
	_ = c.Set("image", func(path string, x, y, w, hgt int) bool {
		src, ok := images[path]
		if !ok {
			src = loadScriptImage(path)
			images[path] = src
		}
		if src == nil {
			return false
		}
		xdraw.CatmullRom.Scale(img, image.Rect(x, y, x+w, y+hgt), src, src.Bounds(), draw.Over, nil)
		return true
	})
	return c
}

func loadScriptImage(path string) image.Image {
	f, err := os.Open(expandWidgetPath(path))
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

// parseScriptColor reads "#rrggbb" or "rrggbb".
func parseScriptColor(s string) (color.Color, error) {
	raw := strings.TrimPrefix(strings.TrimSpace(s), "#")
	b, err := hex.DecodeString(raw)
	if err != nil || len(b) != 3 {
		return nil, fmt.Errorf("color %q: want #rrggbb", s)
	}
	return color.RGBA{b[0], b[1], b[2], 0xff}, nil
}

func scriptTextAlign(h, v string) (TextAlign, error) {
	col := map[string]int{"": 1, "left": 0, "center": 1, "right": 2}
	row := map[string]int{"": 1, "top": 0, "middle": 1, "bottom": 2}
	c, ok := col[h]
	if !ok {
		return 0, fmt.Errorf("align %q unknown (left|center|right)", h)
	}
	r, ok := row[v]
	if !ok {
		return 0, fmt.Errorf("valign %q unknown (top|middle|bottom)", v)
	}
	return TextAlign(r*3 + c), nil
}

func fillCircle(img *image.RGBA, cx, cy, radius int, clr color.Color) {
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= radius*radius {
				img.Set(x, y, clr)
			}
		}
	}
}
