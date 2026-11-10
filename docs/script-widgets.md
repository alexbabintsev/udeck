# Script widgets

A widget type that udeck does not ship can be written in JavaScript: one
file per type in the `widgets` folder next to the config file. A button
with `widget.type: http_status` runs `widgets/http_status.js`; with the
default config that is `~/.config/udeck/widgets/http_status.js`.

Scripts run inside udeck (an embedded engine, ES2020 and later:
`async`/`await`, classes, `?.`, `??`), so the same `udeck` binary, the
Homebrew one included, runs any widget. Editing a script reloads the
config, like editing the config itself.

[examples/widgets/http_status.js](../examples/widgets/http_status.js) is a
complete example.

## The definition

The script calls `widget()` once:

```js
widget({
  pollMS: 10000,          // default poll interval; poll_ms in the config overrides it
  minPollMS: 1000,        // the smallest poll_ms the config may set (default 1000)

  // Optional: check the widget's options when the config loads. Throw to
  // reject the config, which then reports the button and the message.
  validate(params) {
    if (!params.url) throw new Error("url is empty");
  },

  // Optional: the gestures the key reacts to. A list, an object, or a
  // function of params returning either. Default: ["tap"].
  //   ["tap", "double_tap", "hold", "repeat"]
  //   {tap: true, hold: true, hold_ms: 900, double_tap_ms: 300, repeat_ms: 200}
  gestures: ["tap"],

  // Called once per key; returns the key's handlers.
  create(w) {
    return {
      draw(c) {},           // required: paint the key
      async poll() {},      // every poll interval while the key is on screen
      async gesture(kind) {}, // "tap" | "double_tap" | "hold" | "repeat"
      resume() {},          // the key is shown again (default: poll)
      close() {},           // the config reloaded or udeck stops
    };
  },
});
```

The key is redrawn after every handler (and the promise it returns)
settles; a frame identical to the last one is not uploaded again. Call
`w.redraw()` to repaint in the middle of slow work.

Each key has its own engine and runs one handler at a time, but a
handler waiting on a promise (HTTP, a command, a timer) lets the others
run: a tap is handled while a slow poll is still waiting for its server.
A new poll does not start until the previous one has settled.

A synchronous call into a script must finish within 2 seconds, or it is
interrupted. Slow work belongs in promises, which have no such limit.

Errors thrown by handlers, or promises they reject, are logged — once
until the message changes, so a server that stays down does not flood
the log. If `create` fails, the key shows a warning sign and the type's
name, and the rest of the deck keeps working.

## `w`, the key

| | |
|---|---|
| `w.params` | The button's `widget:` mapping, as written in the YAML |
| `w.settings` | `widgets.<type>` from the config: settings shared by every key of the type, `{}` if absent |
| `w.button` | `{label, icon}` of the button; `icon` is resolved against the config folder |
| `w.id`, `w.type` | The button's internal ID and the widget type |
| `w.width`, `w.height` | The key's size: 196×196, or 458×196 at `at: [2, 3]` |
| `w.redraw()` | Repaint now |
| `w.visible()` | Whether the key is on screen |

Shared settings look like this in the config:

```yaml
widgets:
  font: ~/Library/Fonts/Inter.ttf   # built in: the font of all widget text
  http_status:                      # handed to every http_status key as w.settings
    timeout: 5000
```

## `c`, the canvas

`draw(c)` gets a black canvas. Coordinates are pixels from the top left;
colors are `"#rrggbb"`.

| | |
|---|---|
| `c.width`, `c.height` | The canvas size |
| `c.clear(color)` | Fill the canvas (default black) |
| `c.rect(x, y, w, h, color, radius)` | A rectangle, rounded when `radius` is set |
| `c.circle(cx, cy, r, color)` | A filled circle |
| `c.text(text, x, y, w, h, opts)` | Text in a block; see below |
| `c.measure(text, size, bold)` | The width of text in pixels |
| `c.icon(id, x, y, w, h, color)` | A pack icon such as `lucide/play`, scaled into the block |
| `c.image(path, x, y, w, h)` | A PNG or JPEG file scaled into the block; returns whether it could |

`c.text` options: `color` (default white), `size` (default 28), `bold`
(default true), `align` (`left` \| `center` \| `right`, default center),
`valign` (`top` \| `middle` \| `bottom`, default middle), `minSize`, and
`tracking` (extra pixels between letters). Text shrinks from `size` down
to `minSize` (default 18) to fit the block's width, and is then
shortened with an ellipsis.

## `udeck`, the API

The asynchronous calls return promises and work in `create` and the
handlers, not in `validate` or `gestures`.

| | |
|---|---|
| `udeck.http.getJSON(url, {headers, timeout})` | Resolves with the decoded JSON body; rejects on a network error or a non-2xx status |
| `udeck.http.request({url, method, headers, body, timeout})` | Resolves with `{status, ok, headers, text}` for any status; rejects only without a response |
| `udeck.http.insecure(url)` | Whether credentials sent to `url` would cross the network unencrypted (plain `http://` to a host that is not loopback or a private address) |
| `udeck.exec(cmd, args, {dir, env, timeout, input})` | Runs a command; resolves with `{code, stdout, stderr}` whatever the exit code, rejects when it cannot start or runs past its timeout. `env` adds to udeck's environment |
| `udeck.sleep(ms)` | Resolves after `ms` |
| `setTimeout(fn, ms)`, `clearTimeout(id)` | Timers on the key's loop |
| `udeck.fs.exists(path)` | Whether a file exists (`~` expanded) |
| `udeck.path.expand`, `join`, `base`, `dir`, `isAbs` | Path helpers; `expand` turns `~/` into the home folder |
| `udeck.configDir` | The folder of the config file |
| `udeck.base64(text)` | Base64 of a string, e.g. for HTTP basic auth |
| `udeck.log(...)`, `console.log(...)` | Write to udeck's log |
| `udeck.debug(...)` | Write to the log only with `--debug` |

Timeouts are in milliseconds: HTTP defaults to 10 seconds, commands to
20, and none may exceed 10 minutes. Response bodies and command output
are capped at 4 MB.

## Security

A script widget runs with your user's permissions, like `shell` actions:
only use scripts you wrote or reviewed, and keep the `widgets` folder
writable only by you.
