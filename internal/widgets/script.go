package widgets

// Script widgets are widget types written in JavaScript, one file per
// type: widgets/<type>.js next to the config file. The file calls
// widget({...}) once with the definition:
//
//	widget({
//	  pollMS: 10000,             // default poll interval (poll_ms overrides)
//	  minPollMS: 3000,           // smallest poll_ms the config may set
//	  validate(params) {},       // throw to reject the widget's options
//	  gestures(params) {},       // ["tap", "hold"] or {tap: true, hold_ms: 900}
//	  create(w) {                // once per key
//	    return {
//	      draw(c) {},            // paint the key on canvas c
//	      async poll() {},       // every poll interval while on screen
//	      async gesture(kind) {},
//	      resume() {},           // shown again (default: poll)
//	      close() {},
//	    };
//	  },
//	});
//
// Each key runs in its own JavaScript runtime with its own event loop
// goroutine: handlers never run concurrently for one key, and promises
// let slow work (HTTP, commands) run without blocking taps. The key is
// redrawn after every handler settles; draw results that do not change
// the image are not uploaded again.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/alexbabintsev/udeck/internal/config"
	ig "github.com/alexbabintsev/udeck/internal/gestures"
)

// scriptCallTimeout bounds every synchronous call into a script, so an
// endless loop cannot freeze the deck; slow work belongs in promises,
// which do not count. A var so tests can shorten it.
var scriptCallTimeout = 2 * time.Second

const (
	defaultScriptPollMS    = 10000
	defaultScriptMinPollMS = 1000
	defaultScriptDoubleTap = 300 * time.Millisecond
	defaultScriptRepeat    = 200 * time.Millisecond
)

var errScriptTimeout = errors.New("script ran too long: slow work belongs in promises")

func init() { config.CheckScriptWidget = checkScriptWidget }

// scriptType is one compiled widgets/<type>.js.
type scriptType struct {
	name      string
	path      string
	configDir string
	prog      *goja.Program
	pollMS    int
	minPollMS int

	// mu guards rt and def, the runtime validate and gestures run in.
	mu  sync.Mutex
	rt  *goja.Runtime
	def *goja.Object
}

type scriptCacheEntry struct {
	mod  time.Time
	size int64
	typ  *scriptType
	err  error
}

// scriptCache keeps compiled scripts by path until the file changes, so
// a config reload only recompiles the scripts that were edited.
var scriptCache = struct {
	sync.Mutex
	m map[string]scriptCacheEntry
}{m: map[string]scriptCacheEntry{}}

func loadScriptType(path string) (*scriptType, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	scriptCache.Lock()
	defer scriptCache.Unlock()
	if e, ok := scriptCache.m[path]; ok && e.mod.Equal(info.ModTime()) && e.size == info.Size() {
		return e.typ, e.err
	}
	typ, err := compileScriptType(path)
	scriptCache.m[path] = scriptCacheEntry{mod: info.ModTime(), size: info.Size(), typ: typ, err: err}
	return typ, err
}

func compileScriptType(path string) (*scriptType, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	prog, err := goja.Compile(path, string(src), false)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", path, err)
	}
	t := &scriptType{
		name:      strings.TrimSuffix(filepath.Base(path), ".js"),
		path:      path,
		configDir: filepath.Dir(filepath.Dir(path)),
		prog:      prog,
	}
	rt, def, err := t.instantiate(&scriptHost{name: t.name})
	if err != nil {
		return nil, err
	}
	t.rt, t.def = rt, def
	if t.pollMS, err = scriptInt(def, "pollMS", defaultScriptPollMS); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if t.minPollMS, err = scriptInt(def, "minPollMS", defaultScriptMinPollMS); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

// instantiate runs the script in a new runtime and returns the
// definition it passed to widget().
func (t *scriptType) instantiate(host *scriptHost) (*goja.Runtime, *goja.Object, error) {
	rt := goja.New()
	rt.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	host.rt = rt
	host.configDir = t.configDir
	var def *goja.Object
	_ = rt.Set("widget", func(call goja.FunctionCall) goja.Value {
		obj, ok := call.Argument(0).(*goja.Object)
		if !ok || def != nil {
			panic(rt.NewTypeError("widget() takes the widget definition, once"))
		}
		def = obj
		return goja.Undefined()
	})
	installScriptAPI(host)
	if _, err := guardScript(rt, func() (goja.Value, error) { return rt.RunProgram(t.prog) }); err != nil {
		return nil, nil, fmt.Errorf("%s: %s", t.path, scriptErrorText(err))
	}
	if def == nil {
		return nil, nil, fmt.Errorf("%s does not call widget({...})", t.path)
	}
	if _, ok := goja.AssertFunction(def.Get("create")); !ok {
		return nil, nil, fmt.Errorf("%s: the widget has no create(w) function", t.path)
	}
	return rt, def, nil
}

// checkScriptWidget is config.CheckScriptWidget: it compiles the script
// and runs its validate on the widget's options.
func checkScriptWidget(path string, w *config.Widget) error {
	typ, err := loadScriptType(path)
	if err != nil {
		return err
	}
	return typ.validate(w)
}

func (t *scriptType) validate(w *config.Widget) error {
	if w.PollMS != 0 && w.PollMS < t.minPollMS {
		return fmt.Errorf("poll_ms too small: %d (at least %d)", w.PollMS, t.minPollMS)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fn, ok := goja.AssertFunction(t.def.Get("validate"))
	if !ok {
		return nil
	}
	params, err := scriptJSON(t.rt, w.Params)
	if err != nil {
		return err
	}
	if _, err := guardScript(t.rt, func() (goja.Value, error) { return fn(t.def, params) }); err != nil {
		return errors.New(scriptMessage(err))
	}
	return nil
}

// gestures resolves the definition's gestures for one widget's options.
// The default is a tap.
func (t *scriptType) gestures(w *config.Widget) (ig.Options, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.def.Get("gestures")
	if fn, ok := goja.AssertFunction(v); ok {
		params, err := scriptJSON(t.rt, w.Params)
		if err != nil {
			return ig.Options{}, err
		}
		if v, err = guardScript(t.rt, func() (goja.Value, error) { return fn(t.def, params) }); err != nil {
			return ig.Options{}, fmt.Errorf("gestures: %s", scriptMessage(err))
		}
	}
	return parseScriptGestures(v, w.HoldMS)
}

func parseScriptGestures(v goja.Value, holdMS int) (ig.Options, error) {
	opts := ig.Options{
		HoldDelay:      holdDuration(holdMS),
		DoubleTapDelay: defaultScriptDoubleTap,
		RepeatEvery:    defaultScriptRepeat,
	}
	set := func(kind string) error {
		switch ig.Kind(kind) {
		case ig.Tap:
			opts.HasTap = true
		case ig.DoubleTap:
			opts.HasDoubleTap = true
		case ig.Hold:
			opts.HasHold = true
		case ig.Repeat:
			opts.HasRepeat = true
		default:
			return fmt.Errorf("gesture %q unknown (tap|double_tap|hold|repeat)", kind)
		}
		return nil
	}
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		opts.HasTap = true
		return opts, nil
	}
	switch raw := v.Export().(type) {
	case []any:
		for _, k := range raw {
			s, _ := k.(string)
			if err := set(s); err != nil {
				return opts, err
			}
		}
	case map[string]any:
		for k, val := range raw {
			ms, isNum := scriptNumber(val)
			switch k {
			case "hold_ms", "double_tap_ms", "repeat_ms":
				if !isNum || ms < 50 {
					return opts, fmt.Errorf("gestures.%s must be a number of at least 50", k)
				}
				d := time.Duration(ms) * time.Millisecond
				switch k {
				case "hold_ms":
					opts.HoldDelay = d
				case "double_tap_ms":
					opts.DoubleTapDelay = d
				default:
					opts.RepeatEvery = d
				}
			default:
				if on, _ := val.(bool); on {
					if err := set(k); err != nil {
						return opts, err
					}
				}
			}
		}
	default:
		return opts, fmt.Errorf("gestures must be a list or an object, not %v", v)
	}
	return opts, nil
}

func scriptNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func scriptInt(obj *goja.Object, key string, def int) (int, error) {
	v := obj.Get(key)
	if v == nil || goja.IsUndefined(v) {
		return def, nil
	}
	n, ok := scriptNumber(v.Export())
	if !ok || n < 0 {
		return 0, fmt.Errorf("%s must be a positive number", key)
	}
	return int(n), nil
}

// scriptJSON converts Go data into plain JavaScript values (arrays are
// real arrays, objects plain objects) by way of JSON.
// Missing data (a nil map, say) becomes an empty object.
func scriptJSON(rt *goja.Runtime, v any) (goja.Value, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("options: %w", err)
	}
	if string(data) == "null" {
		return rt.NewObject(), nil
	}
	return scriptParseJSON(rt, string(data))
}

func scriptParseJSON(rt *goja.Runtime, text string) (goja.Value, error) {
	parse, _ := goja.AssertFunction(rt.Get("JSON").ToObject(rt).Get("parse"))
	return parse(goja.Undefined(), rt.ToValue(text))
}

// guardScript runs fn, interrupting the script if it takes longer than
// scriptCallTimeout.
func guardScript(rt *goja.Runtime, fn func() (goja.Value, error)) (goja.Value, error) {
	var (
		mu       sync.Mutex
		finished bool
	)
	timer := time.AfterFunc(scriptCallTimeout, func() {
		mu.Lock()
		if !finished {
			rt.Interrupt(errScriptTimeout)
		}
		mu.Unlock()
	})
	v, err := fn()
	mu.Lock()
	finished = true
	mu.Unlock()
	timer.Stop()
	rt.ClearInterrupt()
	return v, err
}

// scriptMessage is the message of a script error: what a thrown Error
// says, without the stack, for config errors.
func scriptMessage(err error) string {
	var ex *goja.Exception
	if errors.As(err, &ex) {
		if obj, ok := ex.Value().(*goja.Object); ok {
			if msg := obj.Get("message"); msg != nil && !goja.IsUndefined(msg) {
				return msg.String()
			}
		}
		return ex.Value().String()
	}
	var intr *goja.InterruptedError
	if errors.As(err, &intr) {
		return errScriptTimeout.Error()
	}
	return err.Error()
}

// scriptErrorText is a script error with where it happened, for logs.
func scriptErrorText(err error) string {
	var ex *goja.Exception
	if errors.As(err, &ex) {
		text := strings.TrimSpace(ex.String())
		// The first frame says where; the rest is noise in a log line.
		if lines := strings.Split(text, "\n"); len(lines) > 2 {
			text = strings.Join(lines[:2], " ")
		}
		return strings.Join(strings.Fields(text), " ")
	}
	return scriptMessage(err)
}
