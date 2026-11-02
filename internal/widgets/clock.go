package widgets

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"
	"time"

	"github.com/alexbabintsev/ulanzi-deck-go/internal/config"
	"github.com/alexbabintsev/ulanzi-deck-go/internal/debug"
	ig "github.com/alexbabintsev/ulanzi-deck-go/internal/gestures"
)

// clockTick is how often the clock checks for a new minute; it redraws
// only when the minute changes.
const clockTick = time.Second

// clockNow is the time source; each clock copies it when created, so
// tests can substitute it.
var clockNow = time.Now

// ClockWidget shows the time, by default local, or in a configured time
// zone with the city's name under it (a world clock).
type ClockWidget struct {
	*widgetBase
	loc    *time.Location
	label  string
	hour12 bool
	now    func() time.Time

	mu      sync.Mutex
	lastKey string
}

func NewClockWidget(id string, cfg *config.Widget, notify func(string)) (*ClockWidget, error) {
	loc := time.Local
	if tz := strings.TrimSpace(cfg.Timezone); tz != "" {
		var err error
		if loc, err = time.LoadLocation(tz); err != nil {
			return nil, fmt.Errorf("clock time zone %q: %w", tz, err)
		}
	}
	base, err := newWidgetBase(id, "clock", notify)
	if err != nil {
		return nil, err
	}
	w := &ClockWidget{
		widgetBase: base,
		loc:        loc,
		label:      clockLabel(cfg.Title, cfg.Timezone),
		hour12:     cfg.Hour12,
		now:        clockNow,
	}
	if _, err := w.writeIcon(w.render(w.now())); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write clock icon: %w", err)
	}
	w.every(clockTick, func(time.Time) { w.tick() })
	return w, nil
}

// clockLabel is the title, or else the city of the time zone:
// "America/New_York" reads "New York". The local clock has none.
func clockLabel(title, tz string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	tz = strings.TrimSpace(tz)
	if i := strings.LastIndex(tz, "/"); i >= 0 {
		tz = tz[i+1:]
	}
	return strings.ReplaceAll(tz, "_", " ")
}

func (w *ClockWidget) HandleGesture(ig.Kind) {}

func (w *ClockWidget) resume() { w.tick() }

// tick redraws when the shown minute changed.
func (w *ClockWidget) tick() {
	now := w.now()
	w.mu.Lock()
	same := w.lastKey == w.frameKey(now)
	w.mu.Unlock()
	if same {
		return
	}
	if _, err := w.publish(w.render(now)); err != nil {
		debug.Logf("widget %s: %v", w.id, err)
	}
}

func (w *ClockWidget) frameKey(now time.Time) string {
	return now.In(w.loc).Format("2006-01-02 15:04")
}

func (w *ClockWidget) render(now time.Time) *image.RGBA {
	w.mu.Lock()
	w.lastKey = w.frameKey(now)
	w.mu.Unlock()
	return renderClock(now.In(w.loc), w.label, w.hour12)
}

// renderClock draws the date on top, the time in the middle with a dimmed
// colon (as on the wide dashboard) and the label at the bottom, where
// icon cards keep theirs. Without a label the time moves down to the
// middle of the key.
func renderClock(t time.Time, label string, hour12 bool) *image.RGBA {
	img := newCard()
	timeText := t.Format("15:04")
	date := strings.ToUpper(t.Format("Mon 2"))
	if hour12 {
		timeText = t.Format("3:04")
		date = t.Format("PM") + "  " + date
	}
	dateClr := color.Color(styleDim)
	if wd := t.Weekday(); wd == time.Saturday || wd == time.Sunday {
		dateClr = styleAlert
	}
	drawText(img, date, FromLeft(8), FromTop(16), img.Bounds().Dx()-16, 30, AlignMiddleCenter, dateClr, 26, true)
	timeBox := image.Rect(14, 52, img.Bounds().Dx()-14, 124)
	if label == "" {
		timeBox = image.Rect(14, 64, img.Bounds().Dx()-14, 150)
	}
	drawCenteredClock(img, timeText, timeBox, styleText, styleDim, 66)
	if label != "" {
		drawCardLabel(img, label, styleAccent)
	}
	return img
}

// drawCenteredClock draws "15:04" centered in rect, the colon in
// colonClr, at the largest size up to size that fits.
func drawCenteredClock(img *image.RGBA, text string, rect image.Rectangle, clr, colonClr color.Color, size float64) {
	face, _ := fitFace(text, rect.Dx(), size, 18, 1, true)
	if face == nil {
		return
	}
	defer face.Close()
	baseline := rect.Min.Y + (rect.Dy()-face.Metrics().Height.Ceil())/2 + face.Metrics().Ascent.Ceil()
	x := rect.Min.X + (rect.Dx()-trackedTextWidth(face, text, 0))/2
	hours, minutes, ok := strings.Cut(text, ":")
	if !ok {
		drawLeftFace(img, text, x, baseline, clr, face)
		return
	}
	drawLeftFace(img, hours, x, baseline, clr, face)
	x += trackedTextWidth(face, hours, 0)
	drawLeftFace(img, ":", x, baseline, colonClr, face)
	x += trackedTextWidth(face, ":", 0)
	drawLeftFace(img, minutes, x, baseline, clr, face)
}
