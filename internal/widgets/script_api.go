package widgets

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/alexbabintsev/udeck/internal/debug"
)

const (
	defaultScriptHTTPTimeout = 10 * time.Second
	defaultScriptExecTimeout = 20 * time.Second
	maxScriptTimeout         = 10 * time.Minute
)

// scriptHost is what the udeck API needs from the runtime it serves. A
// widget's runtime has a loop (post, guard); the runtime validate and
// gestures run in has none, so the asynchronous calls refuse to work.
type scriptHost struct {
	rt        *goja.Runtime
	name      string // widget type
	id        string // button, for logs; empty outside a widget
	configDir string
	// post queues fn on the widget's loop; false once it is closed.
	post func(fn func()) bool
	// guard runs fn on the loop with the script time limit.
	guard func(fn func() (goja.Value, error)) (goja.Value, error)

	timers  map[int64]*time.Timer // loop only
	timerID int64
}

func (h *scriptHost) logPrefix() string {
	if h.id == "" {
		return "widget " + h.name
	}
	return fmt.Sprintf("widget %s (%s)", h.id, h.name)
}

func (h *scriptHost) throw(format string, args ...any) {
	panic(h.rt.NewTypeError(fmt.Sprintf(format, args...)))
}

// async runs work on its own goroutine and settles the returned promise
// on the widget's loop. settle turns work's result into a JS value
// there; nil means undefined.
func (h *scriptHost) async(what string, work func() (any, error), settle func(any) (goja.Value, error)) goja.Value {
	if h.post == nil {
		h.throw("%s is only available inside create() and the widget's handlers", what)
	}
	p, resolve, reject := h.rt.NewPromise()
	go func() {
		result, err := work()
		h.post(func() {
			_, _ = h.guard(func() (goja.Value, error) {
				if err != nil {
					return nil, reject(h.rt.NewGoError(err))
				}
				value := goja.Undefined()
				if settle != nil {
					v, err := settle(result)
					if err != nil {
						return nil, reject(h.rt.NewGoError(err))
					}
					value = v
				} else if result != nil {
					value = h.rt.ToValue(result)
				}
				return nil, resolve(value)
			})
		})
	}()
	return h.rt.ToValue(p)
}

func installScriptAPI(h *scriptHost) {
	rt := h.rt
	udeck := rt.NewObject()
	_ = rt.Set("udeck", udeck)
	_ = udeck.Set("configDir", h.configDir)

	_ = udeck.Set("log", func(call goja.FunctionCall) goja.Value {
		log.Printf("%s: %s", h.logPrefix(), joinScriptArgs(call.Arguments))
		return goja.Undefined()
	})
	_ = udeck.Set("debug", func(call goja.FunctionCall) goja.Value {
		debug.Logf("%s: %s", h.logPrefix(), joinScriptArgs(call.Arguments))
		return goja.Undefined()
	})
	console := rt.NewObject()
	_ = console.Set("log", udeck.Get("log"))
	_ = console.Set("error", udeck.Get("log"))
	_ = console.Set("debug", udeck.Get("debug"))
	_ = rt.Set("console", console)

	_ = udeck.Set("base64", func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) })

	_ = udeck.Set("sleep", func(ms float64) goja.Value {
		d := scriptDuration(ms, 0)
		return h.async("udeck.sleep", func() (any, error) {
			time.Sleep(d)
			return nil, nil
		}, nil)
	})
	_ = rt.Set("setTimeout", func(call goja.FunctionCall) goja.Value {
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			h.throw("setTimeout needs a function")
		}
		if h.post == nil {
			h.throw("setTimeout is only available inside create() and the widget's handlers")
		}
		d := scriptDuration(call.Argument(1).ToFloat(), 0)
		h.timerID++
		id := h.timerID
		if h.timers == nil {
			h.timers = map[int64]*time.Timer{}
		}
		h.timers[id] = time.AfterFunc(d, func() {
			h.post(func() {
				if _, ok := h.timers[id]; !ok {
					return // cleared
				}
				delete(h.timers, id)
				if _, err := h.guard(func() (goja.Value, error) { return fn(goja.Undefined()) }); err != nil {
					log.Printf("%s: setTimeout: %s", h.logPrefix(), scriptErrorText(err))
				}
			})
		})
		return rt.ToValue(id)
	})
	_ = rt.Set("clearTimeout", func(id int64) {
		if t, ok := h.timers[id]; ok {
			t.Stop()
			delete(h.timers, id)
		}
	})

	path := rt.NewObject()
	_ = udeck.Set("path", path)
	_ = path.Set("expand", expandWidgetPath)
	_ = path.Set("join", func(parts ...string) string { return filepath.Join(parts...) })
	_ = path.Set("base", filepath.Base)
	_ = path.Set("dir", filepath.Dir)
	_ = path.Set("isAbs", filepath.IsAbs)

	fs := rt.NewObject()
	_ = udeck.Set("fs", fs)
	_ = fs.Set("exists", func(p string) bool {
		if p == "" {
			return false
		}
		_, err := os.Stat(expandWidgetPath(p))
		return err == nil
	})

	installScriptHTTP(h, udeck)
	installScriptExec(h, udeck)
}

func joinScriptArgs(args []goja.Value) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.String()
	}
	return strings.Join(parts, " ")
}

// scriptDuration converts milliseconds from a script, falling back to
// def for zero and capping at maxScriptTimeout.
func scriptDuration(ms float64, def time.Duration) time.Duration {
	if ms <= 0 {
		return def
	}
	d := time.Duration(ms * float64(time.Millisecond))
	return min(d, maxScriptTimeout)
}

type scriptHTTPOptions struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Timeout float64           `json:"timeout"`
}

type scriptHTTPResponse struct {
	Status  int               `json:"status"`
	OK      bool              `json:"ok"`
	Headers map[string]string `json:"headers"`
	Text    string            `json:"text"`
}

func installScriptHTTP(h *scriptHost, udeck *goja.Object) {
	rt := h.rt
	httpObj := rt.NewObject()
	_ = udeck.Set("http", httpObj)

	options := func(v goja.Value) scriptHTTPOptions {
		var opts scriptHTTPOptions
		if v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
			if err := rt.ExportTo(v, &opts); err != nil {
				h.throw("http options: %v", err)
			}
		}
		return opts
	}

	// getJSON(url, {headers, timeout}) resolves with the decoded body and
	// rejects on a network error or a non-2xx status.
	_ = httpObj.Set("getJSON", func(call goja.FunctionCall) goja.Value {
		opts := options(call.Argument(1))
		rawURL := call.Argument(0).String()
		headers := map[string]string{"Accept": "application/json"}
		for k, v := range opts.Headers {
			headers[k] = v
		}
		client := &http.Client{Timeout: scriptDuration(opts.Timeout, defaultScriptHTTPTimeout)}
		return h.async("udeck.http.getJSON", func() (any, error) {
			return httpGet(client, rawURL, headers)
		}, func(body any) (goja.Value, error) {
			v, err := scriptParseJSON(rt, string(body.([]byte)))
			if err != nil {
				return nil, fmt.Errorf("decode JSON from %s: %s", redactURL(rawURL), scriptMessage(err))
			}
			return v, nil
		})
	})

	// request({url, method, headers, body, timeout}) resolves with
	// {status, ok, headers, text} for any status and rejects only when
	// there is no response.
	_ = httpObj.Set("request", func(call goja.FunctionCall) goja.Value {
		opts := options(call.Argument(0))
		if opts.URL == "" {
			h.throw("udeck.http.request needs a url")
		}
		return h.async("udeck.http.request", func() (any, error) {
			return scriptHTTPDo(opts)
		}, func(resp any) (goja.Value, error) {
			return scriptJSON(rt, resp)
		})
	})

	// insecure(url) reports whether credentials sent to url would cross
	// the network unencrypted.
	_ = httpObj.Set("insecure", sendsCredentialsInClear)
}

func scriptHTTPDo(opts scriptHTTPOptions) (*scriptHTTPResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(opts.Method))
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if opts.Body != "" {
		body = strings.NewReader(opts.Body)
	}
	req, err := http.NewRequest(method, opts.URL, body)
	if err != nil {
		return nil, err
	}
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: scriptDuration(opts.Timeout, defaultScriptHTTPTimeout)}
	resp, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return nil, fmt.Errorf("%s %s%s: %w", method, req.URL.Host, req.URL.Path, urlErr.Err)
		}
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxHTTPBodyBytes {
		return nil, fmt.Errorf("%s %s: response larger than %d bytes", method, req.URL.Path, maxHTTPBodyBytes)
	}
	headers := map[string]string{}
	for k := range resp.Header {
		headers[strings.ToLower(k)] = resp.Header.Get(k)
	}
	return &scriptHTTPResponse{
		Status:  resp.StatusCode,
		OK:      resp.StatusCode >= 200 && resp.StatusCode < 300,
		Headers: headers,
		Text:    string(data),
	}, nil
}

// redactURL drops credentials and the query from a URL for messages.
func redactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "the URL"
	}
	return u.Host + u.Path
}

// sendsCredentialsInClear reports whether credentials sent to rawURL
// cross the network unencrypted: plain http to a host that is neither
// loopback nor a private (LAN/VPN) address.
func sendsCredentialsInClear(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.EqualFold(u.Scheme, "http") {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return !addr.IsLoopback() && !addr.IsPrivate()
	}
	return true // a host name: assume it is reached over the internet
}

type scriptExecOptions struct {
	Dir     string            `json:"dir"`
	Env     map[string]string `json:"env"`
	Timeout float64           `json:"timeout"`
	Input   string            `json:"input"`
}

type scriptExecResult struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// scriptCommand starts commands; tests replace it.
var scriptCommand = exec.CommandContext

func installScriptExec(h *scriptHost, udeck *goja.Object) {
	rt := h.rt
	// exec(cmd, [args], {dir, env, timeout, input}) resolves with
	// {code, stdout, stderr} whatever the exit code, and rejects when the
	// command cannot start or runs past its timeout.
	_ = udeck.Set("exec", func(call goja.FunctionCall) goja.Value {
		name := expandWidgetPath(call.Argument(0).String())
		if strings.TrimSpace(name) == "" || goja.IsUndefined(call.Argument(0)) {
			h.throw("udeck.exec needs a command")
		}
		var args []string
		if a := call.Argument(1); !goja.IsUndefined(a) && !goja.IsNull(a) {
			if err := rt.ExportTo(a, &args); err != nil {
				h.throw("udeck.exec args must be a list of strings: %v", err)
			}
		}
		var opts scriptExecOptions
		if o := call.Argument(2); !goja.IsUndefined(o) && !goja.IsNull(o) {
			if err := rt.ExportTo(o, &opts); err != nil {
				h.throw("udeck.exec options: %v", err)
			}
		}
		return h.async("udeck.exec", func() (any, error) {
			return scriptRun(name, args, opts)
		}, func(res any) (goja.Value, error) {
			return scriptJSON(rt, res)
		})
	})
}

func scriptRun(name string, args []string, opts scriptExecOptions) (*scriptExecResult, error) {
	timeout := scriptDuration(opts.Timeout, defaultScriptExecTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := scriptCommand(ctx, name, args...)
	if opts.Dir != "" {
		cmd.Dir = expandWidgetPath(opts.Dir)
	}
	if len(opts.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range opts.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	if opts.Input != "" {
		cmd.Stdin = strings.NewReader(opts.Input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedBuffer{buf: &stdout}
	cmd.Stderr = &limitedBuffer{buf: &stderr}
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("%s timed out after %v", filepath.Base(name), timeout)
	}
	res := &scriptExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return nil, err
		}
		res.Code = exitErr.ExitCode()
	}
	return res, nil
}

// limitedBuffer keeps the first maxHTTPBodyBytes of a command's output
// and discards the rest, so a chatty command cannot exhaust memory.
type limitedBuffer struct{ buf *bytes.Buffer }

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxHTTPBodyBytes - l.buf.Len(); room > 0 {
		l.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
