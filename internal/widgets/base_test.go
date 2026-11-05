package widgets

import (
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexbabintsev/udeck/internal/icons"
)

func TestWidgetBasePublishWriteAndClose(t *testing.T) {
	rec := &recorder{}
	b, err := newWidgetBase("id", "t", rec.notify)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if changed, err := b.writeIcon(img); err != nil || !changed || rec.count() != 0 {
		t.Fatalf("writeIcon changed=%v err=%v notifies=%d", changed, err, rec.count())
	}
	// Published images are not modified afterwards; a change is a new one.
	img = image.NewRGBA(image.Rect(0, 0, 4, 4))
	copy(img.Pix[:4], []byte{255, 255, 255, 255})
	if changed, _ := b.publish(img); !changed || rec.count() != 1 {
		t.Fatal("publish must notify on change")
	}
	if changed, _ := b.publish(img); changed || rec.count() != 1 {
		t.Fatal("publish must not notify when unchanged")
	}

	var ticks atomic.Int32
	b.every(time.Millisecond, func(time.Time) { ticks.Add(1) })
	waitFor(t, "ticks", func() bool { return ticks.Load() > 2 })

	if err := b.Close(); err != nil || !b.isClosed() {
		t.Fatal("close")
	}
	after := ticks.Load()
	time.Sleep(20 * time.Millisecond)
	if ticks.Load() > after+1 {
		t.Fatal("loop must stop right after Close")
	}
	if changed, err := b.publish(img); changed || err != nil {
		t.Fatal("closed widget must not write")
	}
	if _, ok := icons.Image(b.IconPath()); ok {
		t.Fatal("Close must drop the icon")
	}
	_ = b.Close()

	nb, _ := newWidgetBase("n", "t", nil)
	defer nb.Close()
	nb.notify("ignored") // nil notify replaced with a no-op
}

func TestHTTPGetAndJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			if r.Header.Get("X-Test") != "1" {
				http.Error(w, "missing header", http.StatusBadRequest)
				return
			}
			fmt.Fprint(w, `{"a": 1}`)
		case "/big":
			w.Write([]byte(strings.Repeat("x", maxHTTPBodyBytes+1)))
		case "/bad":
			fmt.Fprint(w, `{`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var out struct{ A int }
	if err := getJSON(srv.Client(), srv.URL+"/ok", map[string]string{"X-Test": "1"}, &out); err != nil || out.A != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	for _, path := range []string{"/big", "/bad", "/missing"} {
		if err := getJSON(srv.Client(), srv.URL+path, nil, &out); err == nil {
			t.Fatalf("%s: expected error", path)
		}
	}
	if _, err := httpGet(srv.Client(), "://bad", nil); err == nil {
		t.Fatal("expected URL error")
	}
}

func TestFontsDefaultAndCustom(t *testing.T) {
	defer SetFont("")
	SetFont("")
	if uiFont(false) == nil || uiFont(true) == nil || uiFont(false) == uiFont(true) {
		t.Fatal("built-in regular and bold fonts must load")
	}
	SetFont(filepath.Join(t.TempDir(), "missing.ttf"))
	if uiFont(false) == nil {
		t.Fatal("missing custom font must fall back")
	}
	bad := filepath.Join(t.TempDir(), "bad.ttf")
	os.WriteFile(bad, []byte("nope"), 0o600)
	SetFont(bad)
	if uiFont(true) == nil {
		t.Fatal("broken custom font must fall back")
	}
	SetFont("~/definitely-missing-font.ttf")
	SetFont("~/definitely-missing-font.ttf") // unchanged path: no reload
}

// Text must render without any system fonts installed.
func TestDrawTextRendersWithBuiltinFont(t *testing.T) {
	SetFont("")
	img := image.NewRGBA(image.Rect(0, 0, 196, 196))
	drawText(img, "Hello", FromLeft(0), FromTop(0), 196, 60, AlignMiddleCenter, rgb("ffffff"), 40, true)
	lit := false
	for _, v := range img.Pix {
		if v > 0 {
			lit = true
			break
		}
	}
	if !lit {
		t.Fatal("expected text pixels with the built-in font")
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
