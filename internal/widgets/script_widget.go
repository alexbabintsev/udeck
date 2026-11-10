package widgets

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/alexbabintsev/udeck/internal/config"
	"github.com/alexbabintsev/udeck/internal/debug"
	ig "github.com/alexbabintsev/udeck/internal/gestures"
	"github.com/alexbabintsev/udeck/internal/icons"
)

// scriptWidget is one key run by a script (see script.go). Everything
// that touches the runtime runs on loop, apart from the constructor,
// which finishes before the loop starts.
type scriptWidget struct {
	*widgetBase
	typ    *scriptType
	host   *scriptHost
	rt     *goja.Runtime
	inst   *goja.Object
	width  int
	height int
	images map[string]image.Image
	jobs   chan func()

	// Loop only.
	depth    int  // nesting of guarded calls: only the outermost is timed
	polling  bool // a poll has not settled yet
	failed   bool // create failed: the key shows the error card
	lastErr  map[string]string
	hasPoll  bool
	pollTime time.Duration
}

func newScriptWidget(id string, typ *scriptType, btn config.Button, settings any, notify func(string)) (*scriptWidget, error) {
	base, err := newWidgetBase(id, "script-"+typ.name, notify)
	if err != nil {
		return nil, err
	}
	w := &scriptWidget{
		widgetBase: base,
		typ:        typ,
		width:      icons.IconWidth,
		height:     icons.IconHeight,
		images:     map[string]image.Image{},
		jobs:       make(chan func(), 64),
		lastErr:    map[string]string{},
	}
	if btn.At == [2]int{3, 2} {
		w.width = icons.WideWidth
	}
	w.host = &scriptHost{id: id, name: typ.name, post: w.post, guard: w.guard}

	if err := w.start(btn, settings); err != nil {
		// A broken script should not take the deck down with it: the key
		// shows the error and the log says why.
		w.failed = true
		w.report("create", err)
	}
	if _, err := w.writeIcon(w.renderImage()); err != nil {
		_ = w.Close()
		return nil, err
	}
	go w.loop()
	if w.hasPoll {
		w.every(w.pollTime, func(time.Time) { w.post(w.runPoll) })
		w.post(w.runPoll)
	}
	return w, nil
}

// start instantiates the script and calls create(w).
func (w *scriptWidget) start(btn config.Button, settings any) error {
	rt, def, err := w.typ.instantiate(w.host)
	if err != nil {
		return err
	}
	w.rt = rt
	handle, err := w.handle(btn, settings)
	if err != nil {
		return err
	}
	create, _ := goja.AssertFunction(def.Get("create"))
	v, err := w.guard(func() (goja.Value, error) { return create(def, handle) })
	if err != nil {
		return err
	}
	inst, ok := v.(*goja.Object)
	if !ok {
		return errors.New("create(w) must return an object with draw(c)")
	}
	if _, ok := goja.AssertFunction(inst.Get("draw")); !ok {
		return errors.New("the object create(w) returns has no draw(c)")
	}
	w.inst = inst
	if _, ok := goja.AssertFunction(inst.Get("poll")); ok {
		w.hasPoll = true
		ms := w.typ.pollMS
		if btn.Widget != nil && btn.Widget.PollMS > 0 {
			ms = btn.Widget.PollMS
		}
		w.pollTime = time.Duration(max(ms, w.typ.minPollMS, 1)) * time.Millisecond
	}
	return nil
}

// handle is the w a script's create receives.
func (w *scriptWidget) handle(btn config.Button, settings any) (*goja.Object, error) {
	rt := w.rt
	var params map[string]any
	if btn.Widget != nil {
		params = btn.Widget.Params
	}
	paramsV, err := scriptJSON(rt, params)
	if err != nil {
		return nil, err
	}
	settingsV, err := scriptJSON(rt, settings)
	if err != nil {
		return nil, fmt.Errorf("widgets.%s: %w", w.typ.name, err)
	}
	button := rt.NewObject()
	_ = button.Set("label", btn.Label)
	_ = button.Set("icon", btn.Icon)

	h := rt.NewObject()
	_ = h.Set("id", w.id)
	_ = h.Set("type", w.typ.name)
	_ = h.Set("params", paramsV)
	_ = h.Set("settings", settingsV)
	_ = h.Set("button", button)
	_ = h.Set("width", w.width)
	_ = h.Set("height", w.height)
	// redraw() repaints now, e.g. to show "busy" before slow work.
	_ = h.Set("redraw", func() {
		if w.inst != nil {
			w.redraw()
		}
	})
	// visible() reports whether the key is on screen.
	_ = h.Set("visible", w.isActive)
	return h, nil
}

func (w *scriptWidget) post(job func()) bool {
	select {
	case <-w.done:
		return false
	default:
	}
	select {
	case w.jobs <- job:
		return true
	case <-w.done:
		return false
	}
}

func (w *scriptWidget) loop() {
	for {
		select {
		case <-w.done:
			w.stop()
			return
		case job := <-w.jobs:
			job()
		}
	}
}

// stop runs the script's close and cancels its timers.
func (w *scriptWidget) stop() {
	for id, t := range w.host.timers {
		t.Stop()
		delete(w.host.timers, id)
	}
	if w.inst == nil || w.failed {
		return
	}
	if fn, ok := goja.AssertFunction(w.inst.Get("close")); ok {
		if _, err := w.guard(func() (goja.Value, error) { return fn(w.inst) }); err != nil {
			w.report("close", err)
		}
	}
}

// guard times the outermost call into the runtime; calls made while one
// is running (draw from a promise reaction, say) share its budget.
func (w *scriptWidget) guard(fn func() (goja.Value, error)) (goja.Value, error) {
	if w.depth > 0 {
		return fn()
	}
	w.depth++
	defer func() { w.depth-- }()
	return guardScript(w.rt, fn)
}

func (w *scriptWidget) HandleGesture(kind ig.Kind) {
	w.post(func() {
		if w.failed {
			return
		}
		w.call("gesture", w.redraw, w.rt.ToValue(string(kind)))
	})
}

func (w *scriptWidget) resume() {
	w.post(func() {
		if w.method("resume") != nil {
			w.call("resume", w.redraw)
			return
		}
		w.runPoll()
	})
}

func (w *scriptWidget) runPoll() {
	if !w.hasPoll || w.polling {
		return
	}
	w.polling = true
	w.call("poll", func() {
		w.polling = false
		w.redraw()
	})
}

func (w *scriptWidget) method(name string) goja.Callable {
	if w.inst == nil || w.failed {
		return nil
	}
	fn, _ := goja.AssertFunction(w.inst.Get(name))
	return fn
}

// call runs the handler name and, once it and any promise it returns
// have settled, after. Errors are logged.
func (w *scriptWidget) call(name string, after func(), args ...goja.Value) {
	fn := w.method(name)
	if fn == nil {
		after()
		return
	}
	settled := func(err error) {
		if err != nil {
			w.report(name, err)
		} else {
			delete(w.lastErr, name)
		}
		after()
	}
	v, err := w.guard(func() (goja.Value, error) { return fn(w.inst, args...) })
	if err != nil {
		settled(err)
		return
	}
	p, ok := v.Export().(*goja.Promise)
	if !ok {
		settled(nil)
		return
	}
	switch p.State() {
	case goja.PromiseStateFulfilled:
		settled(nil)
	case goja.PromiseStateRejected:
		settled(scriptRejection(p.Result()))
	default:
		obj := v.ToObject(w.rt)
		then, _ := goja.AssertFunction(obj.Get("then"))
		onOK := w.rt.ToValue(func(goja.FunctionCall) goja.Value {
			settled(nil)
			return goja.Undefined()
		})
		onErr := w.rt.ToValue(func(c goja.FunctionCall) goja.Value {
			settled(scriptRejection(c.Argument(0)))
			return goja.Undefined()
		})
		if _, err := w.guard(func() (goja.Value, error) { return then(obj, onOK, onErr) }); err != nil {
			settled(err)
		}
	}
}

// scriptRejection turns a rejection reason into an error.
func scriptRejection(reason goja.Value) error {
	if obj, ok := reason.(*goja.Object); ok {
		if msg := obj.Get("message"); msg != nil && !goja.IsUndefined(msg) {
			return errors.New(msg.String())
		}
	}
	return errors.New(reason.String())
}

// report logs a handler error once until it changes or the handler
// succeeds, so a bot that stays offline does not flood the log.
func (w *scriptWidget) report(stage string, err error) {
	text := scriptErrorText(err)
	if w.lastErr[stage] == text {
		debug.Logf("%s: %s: %s", w.host.logPrefix(), stage, text)
		return
	}
	w.lastErr[stage] = text
	log.Printf("%s: %s: %s", w.host.logPrefix(), stage, text)
}

func (w *scriptWidget) redraw() {
	_, _ = w.publish(w.renderImage())
}

func (w *scriptWidget) renderImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w.width, w.height))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)
	if w.failed || w.inst == nil {
		w.drawError(img)
		return img
	}
	draw, _ := goja.AssertFunction(w.inst.Get("draw"))
	canvas := newScriptCanvas(w.host, img, w.images)
	if _, err := w.guard(func() (goja.Value, error) { return draw(w.inst, canvas) }); err != nil {
		w.report("draw", err)
		draw := image.NewRGBA(img.Bounds())
		w.drawError(draw)
		return draw
	}
	delete(w.lastErr, "draw")
	return img
}

// drawError is the card of a key whose script failed.
func (w *scriptWidget) drawError(img *image.RGBA) {
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)
	drawIcon(img, "lucide/triangle-alert", CenterX(0), FromTop(styleIconTop), styleIconSize, styleIconSize, styleAlert)
	drawCardLabel(img, strings.ReplaceAll(w.typ.name, "_", " "), styleText)
}
