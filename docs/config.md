# Config Reference

This file is the canonical reference for the `udeck` YAML
config format.

When config functionality changes, update this document together with:

- [README.md](../README.md)
- [CHANGELOG.md](../CHANGELOG.md)

## Overview

The runtime loads one YAML file and builds the whole device layout from
it.

Top-level sections:

- `device`
- `widgets`
- `pages`
- `buttons`
- `smallview`

Important rules:

- Use either `pages` or `buttons`, but not both.
- `pages` is the preferred modern format.
- `buttons` is a shorthand for a single implicit page named `Main`.
- Every page must contain at least one button.
- Button coordinates use the D200 physical layout:
  - rows: `0..2`
  - columns: `0..4`
  - `at` is written as `[row, col]`
  - `at: [2, 4]` does not exist because `at: [2, 3]` is double-wide

## Minimal Example

```yaml
device:
  brightness: 65

pages:
  - name: "Main"
    buttons:
      - at: [0, 0]
        label: "Terminal"
        icon: test-icons/terminal.png
        on:
          tap:
            type: shell
            cmd: "open -a Terminal"
```

## Top-Level Fields

### `device`

Optional global device settings.

```yaml
device:
  brightness: 65
  icon_margins:
    top: 8
    right: 8
    bottom: 8
    left: 8
  label_style:
    align: top
    color: "#F6F6F6"
    font_name: "Roboto"
    show_title: true
    size: 7
    weight: 90
```

#### `device.brightness`

- Type: integer
- Range: `0..100`

Examples:

```yaml
brightness: 0
brightness: 65
brightness: 100
```

#### `device.image_format`

- Type: string
- Allowed values: `auto` (default), `webp`, `jpeg`

Encoding of key images sent to the device. WebP is roughly half the size
of JPEG at the same quality, which keeps fast page switching smooth on
the D200's limited link. `auto` asks the device for its firmware version
at startup and uses WebP on firmware 5.3.6 and newer (verified), JPEG on
older or unknown firmware. Force `webp` or `jpeg` if needed; if keys
stay black with `webp`, your firmware cannot decode it - update it from
Ulanzi Studio or use `jpeg`.

#### `device.image_quality`

- Type: integer
- Range: `1..100`
- Default: `75`

Quality of key images for both formats. Raise it only if you see
compression artifacts.

```yaml
image_format: auto
image_quality: 85
```

#### `device.icon_margins`

Global inset for static icons.

- Type: object
- Fields:
  - `top`
  - `right`
  - `bottom`
  - `left`
- Each value must be a non-negative integer.
- `left + right` must fit within the target slot width
- `top + bottom` must be `< 196`

Use this when the D200 distorts pixels near the physical screen edges.

#### `device.label_style`

Global hardware-label style for the current page.

Supported fields:

- `align`
  - Allowed values:
    - `top`
    - `bottom`
    - `center`
- `color`
  - Hex `RRGGBB` or `#RRGGBB`
  - Examples:
    - `FFFFFF`
    - `#F6F6F6`
    - `00FF00`
- `font_name`
  - Type: string
  - Usually `Roboto`
- `show_title`
  - Type: boolean
- `size`
  - Type: integer
- `weight`
  - Type: integer

Example:

```yaml
label_style:
  align: bottom
  color: "#FFFFFF"
  font_name: "Roboto"
  show_title: true
  size: 10
  weight: 80
```

Notes:

- Label style is global for the active page render, not per-button.
- If your icon already contains text, `show_title: false` usually looks
  better.

### `widgets`

Optional global defaults for widgets.

Currently supported:

```yaml
widgets:
  font: ~/Library/Fonts/MyFont-Regular.ttf
```

#### `widgets.font`

Optional path (`~` expanded) to a TTF/OTF file used for all widget text.
When omitted, or when the file cannot be loaded, the embedded Go Mono
family is used, so widgets render the same on every machine. A custom
font is used for both regular and bold text.

### `pages`

Preferred top-level layout format.

```yaml
pages:
  - name: "Main"
    buttons: ...
  - name: "Tools"
    buttons: ...
```

Fields:

- `name`
  - Optional human-readable name
- `buttons`
  - Required non-empty array

### `buttons`

Legacy shorthand for a single implicit page.

```yaml
buttons:
  - at: [0, 0]
    ...
```

Rules:

- Cannot be used together with `pages`
- If used, runtime treats it as:

```yaml
pages:
  - name: "Main"
    buttons: ...
```

### `smallview`

Currently reserved for future work.

Accepted fields:

- `mode`
- `refresh_ms`

This block is parsed but not actively used yet.

## Page Structure

Each page contains `buttons`.

```yaml
pages:
  - name: "Main"
    buttons:
      - at: [0, 0]
        label: "Open"
        icon: test-icons/browser.png
        on:
          tap:
            type: shell
            cmd: "open -a 'Google Chrome'"
```

## Button Structure

Supported button fields:

- `at`
- `label`
- `icon`
- `icon_margins`
- `on`
- `folder`
- `widget`

### `at`

Required.

- Type: `[row, col]`
- Valid range:
  - `row = 0..2`
  - `col = 0..4`
  - `at: [2, 4]` is invalid because `at: [2, 3]` is double-wide

Example:

```yaml
at: [1, 3]
```

### `label`

Optional hardware label text.

```yaml
label: "Terminal"
```

### `icon`

Optional static icon source.

Rules:

- Relative paths are resolved relative to the config file.
- `~/...` is expanded.
- Built-in icon packs can be referenced as `pack/name`, for example `lucide/a-arrow-down`.
- Pack-based SVG icons render on a black background as a centered `128x128` icon.
- Static icons are resized to the target slot size and encoded as JPEG for device upload.
- Most slots are `196x196`; `at: [2, 3]` is `458x196`.

Example:

```yaml
icon: test-icons/terminal.png
```

Or with Lucide:

```yaml
icon: lucide/a-arrow-down
```

### `icon_color`

Optional hex color for pack-based SVG icons such as `lucide/...`.

Rules:

- Format is `RRGGBB` or `#RRGGBB`.
- Applies to pack icons only.
- Raster image files ignore this field.

Example:

```yaml
icon: lucide/cloud-rain
icon_color: 33BBFF
```

### `icon_margins`

Optional per-button override for raster image icon margins.

Same format and constraints as `device.icon_margins`.
For the wide `at: [2, 3]` slot, `left + right` is validated against `458`.
Pack-based SVG icons such as `lucide/...` ignore `icon_margins` and always render centered at `128x128`.

Example:

```yaml
icon_margins:
  top: 12
  right: 12
  bottom: 12
  left: 12
```

### Button Modes

A button can be exactly one of:

- action button via `on`
- nested page via `folder`
- dynamic widget via `widget`

Invalid:

```yaml
on: ...
folder: ...
```

or:

```yaml
on: ...
widget: ...
```

## Action Buttons

Action buttons use `on:`.

```yaml
on:
  tap:
    type: shell
    cmd: "open -a Terminal"
```

Supported gestures:

- `tap`
- `double_tap`
- `hold`
- `repeat`

Optional gesture timing:

- `hold_ms`
  - Minimum supported value: `100`
- `double_tap_ms`
  - Minimum supported value: `100`
- `repeat_every_ms`
  - Minimum supported value: `50`

Example:

```yaml
on:
  tap:
    type: shell
    cmd: "open -a Notes"
  double_tap:
    type: key
    key: "cmd t"
  hold:
    type: shell
    cmd: "echo hold"
  repeat:
    type: key
    key: "space"
  hold_ms: 700
  double_tap_ms: 250
  repeat_every_ms: 100
```

### Gesture Semantics

- `tap`
  - Fires on release
- `double_tap`
  - Fires on the second tap
- `hold`
  - Fires as soon as the hold threshold is reached
- `repeat`
  - Repeats while holding after the repeat threshold starts

## Direct Actions

Each gesture can define one direct action:

- `shell`
- `key`
- `http`
- `window`
- `page_next`
- `page_prev`

### `type: shell`

Runs a shell command asynchronously.

```yaml
tap:
  type: shell
  cmd: "open -a Terminal"
```

Required fields:

- `type`
- `cmd`

### `type: key`

Sends a keystroke or shortcut.

```yaml
tap:
  type: key
  key: "cmd shift 4"
```

Required fields:

- `type`
- `key`

Examples:

- `esc`
- `space`
- `cmd t`
- `cmd f`
- `cmd shift 4`
- `ctrl cmd q`

Notes:

- On macOS, key actions require Accessibility permission.
- `run` prompts for that permission automatically when the active config
  contains `key` actions.
- For Hammerspoon hotkeys specifically, synthetic key events may reach
  the foreground app without triggering `hs.hotkey`. Prefer the
  Hammerspoon IPC CLI pattern shown below instead of binding deck
  buttons to `type: key`.

### `type: http`

Calls a local or remote HTTP endpoint.

```yaml
tap:
  type: http
  url: "http://127.0.0.1:8080/action/ping"
```

Required fields:

- `type`
- `url`

Notes:

- `http` is still useful for local or remote automation bridges.
- For Hammerspoon on macOS, `type: shell` with the bundled `hs` CLI is
  usually more reliable than keeping a custom `hs.httpserver` running.

### `type: window`

Moves and resizes the focused window of the frontmost app, natively
through the macOS Accessibility API (no Hammerspoon or Rectangle
needed). Pressing a deck key does not take the focus, so the window you
were working in is the one that moves.

```yaml
tap:
  type: window
  to: left_half
```

Values of `to`:

- halves: `left_half`, `right_half`, `top_half`, `bottom_half`
- quarters: `top_left`, `top_right`, `bottom_left`, `bottom_right`
- thirds: `left_third`, `center_third`, `right_third`,
  `left_two_thirds`, `right_two_thirds`
- `maximize`: fill the screen without entering full-screen mode
- `center`: keep the size, center on the screen
- `next_display`, `previous_display`: move to the next display (left to
  right), keeping the window's relative place and size
- `restore`: undo the last window action on that app's window, unless
  the window has been moved by hand since

Layouts use the area of the display the window is on, without the menu
bar and the Dock. Notes:

- needs Accessibility access, like `type: key`; `run` asks for it at
  start-up when the config has window actions
- windows in native full-screen mode do not move
- apps that size in steps (terminals) may stop a few pixels short
- macOS has no public API to move windows between Spaces

### Hammerspoon via `hs.ipc`

Recommended pattern for Hammerspoon integration on macOS:

```yaml
tap:
  type: shell
  cmd: '/Applications/Hammerspoon.app/Contents/Frameworks/hs/hs -A -c "UDECK_ACTIONS.rotateDisplay()"'
```

Expected Hammerspoon setup:

- `require("hs.ipc")` in `init.lua`
- a global table such as `UDECK_ACTIONS = { ... }`

This avoids two common problems:

- synthetic `type: key` shortcuts that do not trigger `hs.hotkey`
- long-lived `hs.httpserver` instances that can hang until config reload

### `type: page_next`

Switches to the next top-level page.

```yaml
tap:
  type: page_next
```

Behavior:

- loops at the end
- clears currently open nested `folder`

### `type: page_prev`

Switches to the previous top-level page.

```yaml
tap:
  type: page_prev
```

Behavior:

- loops at the beginning
- clears currently open nested `folder`

## Toggle Actions

A gesture can own a `toggle` instead of a direct action.

```yaml
on:
  tap:
    toggle:
      - label: "A"
        icon: test-icons/apps-folder.png
        action:
          type: shell
          cmd: "echo a"
      - label: "B"
        icon: test-icons/work-folder.png
        action:
          type: shell
          cmd: "echo b"
```

Rules:

- `toggle` must contain at least 2 steps
- only one gesture per button may define `toggle`
- each step may override:
  - `label`
  - `icon`
  - `action`

Behavior:

- steps loop forever
- the current step controls visible label/icon
- after the step runs, the button advances to the next step

## Chain Actions

A gesture can own a `chain`.

```yaml
on:
  tap:
    chain:
      - action:
          type: shell
          cmd: "echo one"
      - delay_ms: 400
      - action:
          type: shell
          cmd: "echo two"
```

Rules:

- each chain step must define exactly one of:
  - `action`
  - `delay_ms`
- `delay_ms` minimum is `10`
- direct fields like `type`, `cmd`, `key`, `url` cannot be mixed with `chain`

Behavior:

- runs strictly in order
- `delay_ms` pauses before the next step
- shell commands are still launched asynchronously

## Folder Buttons

`folder` creates a nested page.

```yaml
- at: [0, 0]
  label: "Apps"
  icon: test-icons/apps-folder.png
  folder:
    buttons:
      - at: [0, 0]
        label: "Terminal"
        icon: test-icons/terminal.png
        on:
          tap:
            type: shell
            cmd: "open -a Terminal"
```

Rules:

- `folder.buttons` must be non-empty
- nested button positions must still be unique
- slot `[2, 3]` is reserved for runtime `Back`

Behavior:

- pressing the folder opens a nested page
- runtime injects a synthetic `Back` button automatically

## Widgets

Widgets are dynamic buttons that render their own icon and may manage
their own state.

Supported widget types:

- general purpose: `clock`, `timer`, `stopwatch`, `pomodoro`, `volume`,
  `microphone`, `audio_output`, `media`, `weather_day`, `wide_dashboard`
- system monitor: `cpu`, `memory`, `disk`, `network`, `battery`
- markets: `ticker`
- Spotify: `spotify_play_pause`, `spotify_next`, `spotify_artwork`

Every widget is independent: it reads its own state and needs no other
widget on the page. The general-purpose ones share one look: black
background, a level ring with the value inside (`volume`, the timers,
the system monitors) or a
large icon or value with an upper-case label (`clock`, `microphone`,
`audio_output`, `media`). Color carries the state the same way everywhere: green is
live, red is muted/off/failing, blue is neutral information, grey is "no
data".

### `widget.type: clock`

The time with the weekday and date above it (red on weekends). With a
`timezone` it becomes a world clock with the city's name under the time.

```yaml
- at: [0, 0]
  widget: {type: clock}                                # local time
- at: [0, 1]
  widget: {type: clock, timezone: "America/New_York"}  # "NEW YORK"
- at: [0, 2]
  widget: {type: clock, timezone: "Asia/Tokyo", title: "Tokyo office", hour12: true}
```

Fields:

- `timezone`
  - optional IANA time zone name, e.g. `Europe/Kyiv`; default local time
- `title`
  - optional label; defaults to the city of `timezone`, none for local
- `hour12`
  - optional, `true` for 12-hour time with AM/PM

Tap does nothing. The key is redrawn once a minute.

### `widget.type: timer`, `stopwatch`, `pomodoro`

Tap starts and pauses, hold (600 ms) resets. The ring shows what is
left (timer, pomodoro) or sweeps once a minute (stopwatch); the time is
inside it. Running is blue (pomodoro: red for work, green for breaks),
paused is amber, a set timer waiting for a tap has a dim full ring.

```yaml
- at: [0, 0]
  widget: {type: timer, duration: 5m}
- at: [0, 1]
  widget: {type: stopwatch}
- at: [0, 2]
  widget: {type: pomodoro}       # 4 × (25m work, 5m break), last break 15m
```

When a timer runs out it turns red with a bell and plays a sound; tap
resets it. When a pomodoro phase ends it plays the sound and loads the
next phase, which starts on the next tap. The alarm fires even when the
widget's page is not shown.

Fields:

- `duration` (timer)
  - Go duration such as `90s`, `5m`, `1h30m`; `1s..24h`; default `5m`
- `work`, `break`, `long_break` (pomodoro)
  - durations, default `25m`, `5m`, `15m`
- `rounds` (pomodoro)
  - work phases before the long break, `1..12`, default `4`
- `sound` (timer, pomodoro)
  - a sound from `/System/Library/Sounds` by name (`Glass`, `Ping`,
    `Hero`, ...), default `Glass`; `none` for silence

### `widget.type: cpu`, `disk`, `network`, `battery`

System monitors. Each key polls its own reading and is re-sent to the
deck only when the picture changes. Tap does nothing.

```yaml
- at: [0, 0]
  widget: {type: cpu}                     # busy %, all cores
- at: [0, 1]
  widget: {type: memory}                  # used %
- at: [0, 2]
  widget: {type: disk}                    # free space of /
- at: [0, 3]
  widget: {type: network}                 # Wi-Fi + Ethernet, down/up
- at: [0, 4]
  widget: {type: battery}
```

- `cpu`: busy share of all cores on a ring; green, amber from 60%, red
  from 85%. Default `poll_ms` `2000`, minimum `500`.
- `disk`: free space in large type, the ring how full the volume is;
  amber from 80% full, red from 90%. `path` picks the volume (any path
  on it, `~` expands; default `/`), `title` replaces the word FREE.
  Default `poll_ms` `60000`, minimum `5000`.
- `network`: download and upload rates in bytes per second. Counts the
  interfaces whose names start with `interface` (comma-separated
  prefixes, default `en`: Wi-Fi and Ethernet; VPN tunnels are not
  counted twice). Default `poll_ms` `2000`, minimum `500`.
- `battery`: charge on a ring; amber from 20%, red from 10% (not while
  plugged in); a bolt while charging, a plug when held on power. Shows
  NO BATTERY on desktop Macs. Default `poll_ms` `30000`, minimum `5000`.

### `widget.type: ticker`

The price of a stock, index, currency pair or coin: the symbol, the
price, the change in percent (green up, red down) and a chart with a
dotted line at the level the change is measured from. No API key is
needed. Tap fetches the price right away.

```yaml
- at: [0, 0]
  widget: {type: ticker, symbol: AAPL}
- at: [0, 1]
  widget: {type: ticker, symbol: "^GSPC", title: "S&P 500"}
- at: [0, 2]
  widget: {type: ticker, symbol: "EURUSD=X", title: "EUR/USD"}
- at: [0, 3]
  widget: {type: ticker, symbol: BTCUSDT, source: binance, title: BTC}
```

- `symbol` is required, spelled the way the source spells it.
- `source` is `yahoo` (default) or `binance`.
  - `yahoo`: stocks (`AAPL`), indices (`^GSPC`), currencies (`EURUSD=X`)
    and coins (`BTC-USD`). The change is from the previous close and
    the chart is the current or last trading day. Outside market hours
    the key shows the last close. This is Yahoo's unofficial chart
    endpoint: it has no guarantees and may change or rate-limit.
  - `binance`: spot pairs (`BTCUSDT`). The change and the chart cover
    the last 24 hours.
- `title` replaces the symbol on the key.
- Default `poll_ms` `60000`, minimum `15000`. Polling stops while the
  page is hidden.

When an update fails, the key keeps the last price and turns grey, and
it recovers by itself. "no data" under the symbol means no price has
arrived, usually a misspelled symbol.

### `widget.type: volume`

Output volume of the default device. The `mute` key shows the level on a
ring with the percentage inside (a red crossed-out speaker when muted);
the `up` and `down` keys show a speaker with a plus or minus and the percentage
underneath.

```yaml
- at: [0, 0]
  widget: {type: volume}              # tap mutes / unmutes
- at: [0, 1]
  widget: {type: volume, action: down}
- at: [0, 2]
  widget: {type: volume, action: up, step: 10}
```

Fields:

- `action`
  - optional, `mute` (default), `up` or `down`
  - `mute`: tap toggles mute
  - `up` / `down`: tap changes the volume by `step`; holding repeats
    every 120 ms after 400 ms. Turning the volume up unmutes.
- `step`
  - optional, percent per press, `1..50`, default `5`
  - values snap to multiples of `step`
- `poll_ms`
  - optional, default `500`, minimum `200`

Devices without a mute switch are muted by setting the volume to 0 and
restored to the previous level. Devices whose volume software cannot
change (many USB DACs, HDMI/DisplayPort outputs) show a grey `FIXED`
card and ignore presses.

### `widget.type: microphone`

Mutes and unmutes the default input device for every app at once, unlike
the in-app buttons of Zoom, Meet or Discord. Green `LIVE` / red `MUTED`.

```yaml
widget:
  type: microphone
  title: "Mic"   # optional: replaces the LIVE/MUTED word, color still tells
```

Fields:

- `title`
  - optional label instead of the state word
- `poll_ms`
  - optional, default `500`, minimum `200`

### `widget.type: audio_output`

Shows the default output device (a headphones icon for AirPods,
headphones and headsets, a speaker otherwise) and switches to the next
device on tap.

```yaml
widget:
  type: audio_output
  devices:            # optional: cycle through these, in this order
    - "AirPods"
    - "MacBook Pro Speakers"
```

Fields:

- `devices`
  - optional list of partial, case-insensitive device names
  - without it, tap cycles through all output devices
  - devices that are not connected are skipped
- `poll_ms`
  - optional, default `1000`, minimum `500`

### `widget.type: media`

System media keys, like the keyboard's: they control whatever is
playing (Music, Spotify, a browser tab, a podcast app).

```yaml
- at: [1, 0]
  widget: {type: media, action: previous}
- at: [1, 1]
  widget: {type: media, action: play_pause}
- at: [1, 2]
  widget: {type: media, action: next}
```

Fields:

- `action`
  - required: `play_pause`, `next` or `previous`
- `title`
  - optional label under the icon

Media keys need Accessibility access for the app running `udeck`
(the same permission as `type: key` actions); `run` asks for it at
start-up when the config has a media widget.

### `widget.type: memory`

```yaml
widget:
  type: memory
  poll_ms: 2000
```

Fields:

- `type`
- `poll_ms`
  - optional, default `2000`
  - minimum `1000`

Shows used memory on a ring: green below 70%, amber below 85%, red
above.

### `widget.type: spotify_play_pause`

```yaml
widget:
  type: spotify_play_pause
```

Optional:

- `poll_ms`
  - minimum `1000`
- `hold_ms`
  - optional hold delay override
  - tap toggles play/pause
  - hold skips to the next track

### `widget.type: spotify_next`

```yaml
widget:
  type: spotify_next
```

Optional:

- `poll_ms`
  - minimum `1000`

### `widget.type: spotify_artwork`

```yaml
widget:
  type: spotify_artwork
  poll_ms: 2000
```

Optional:

- `poll_ms`
  - minimum `1000`

### `widget.type: wide_dashboard`

Double-wide clock and weather for the physical `3_2` slot. Tap does
nothing.

Context-sensitive behavior:

- only valid at `at: [2, 3]`
- when you open a folder on the current page, this widget becomes the
  folder `Back` button automatically
- on top-level pages `2+`, it shows the current page number in the
  corner

```yaml
widget:
  type: wide_dashboard
  city: "Kyiv"
  poll_ms: 600000
```

Fields:

- `type`
- `city`
  - optional weather city name
  - if omitted, the weather part renders a setup placeholder
- `poll_ms`
  - optional weather refresh override
  - minimum effective value is `60000`

### `widget.type: weather_day`

The forecast for one day on a regular key: date, high temperature,
weather icon, humidity and wind.
Keys for the same city (and a `wide_dashboard` with that city) share one
request. Data comes from Open-Meteo, no API key needed.

```yaml
- at: [0, 0]
  widget: {type: weather_day, city: "Kyiv"}               # today
- at: [0, 1]
  widget: {type: weather_day, city: "Kyiv", day_index: 1} # tomorrow
```

Fields:

- `city`
  - required
- `day_index`
  - optional, `0` (today, default) to `13`
- `poll_ms`
  - optional, default `600000`, minimum effective value `60000`

## Path Resolution Rules

Relative paths are resolved relative to the config file for:

- `button.icon`
- nested icons inside `toggle` steps

`~/...` is expanded for icon paths.

## Validation Rules Summary

The loader validates:

- brightness range
- label color format
- label align value
- icon margin ranges
- page presence
- unique button coordinates per page/folder
- no mixed button modes (`on` + `folder` + `widget`)
- non-empty `folder.buttons`
- reserved `Back` slot in folders
- gesture timing minimums
- known action types
- `window` actions have a known `to`
- valid toggle structure
- valid chain structure
- known widget types
- `poll_ms` minimums per widget type
- `wide_dashboard` only at `at: [2, 3]`
- `volume.action` and `volume.step`, `media.action`, `clock.timezone`
- timer and pomodoro durations, `rounds` and `sound`
- `weather_day` requires `city`

## Example Patterns

### Simple App Launcher

```yaml
- at: [0, 0]
  label: "Terminal"
  icon: test-icons/terminal.png
  on:
    tap:
      type: shell
      cmd: "open -a Terminal"
```

### Shortcut Button

```yaml
- at: [0, 1]
  label: "Reload"
  icon: test-icons/reload.png
  on:
    tap:
      type: key
      key: "cmd r"
```

### HTTP Endpoint Button

```yaml
- at: [0, 2]
  label: "Webhook"
  icon: test-icons/lock.png
  on:
    tap:
      type: http
      url: "http://127.0.0.1:8080/action/ping"
```

### Hammerspoon Button

```yaml
- at: [0, 2]
  label: "Left"
  icon: test-icons/find.png
  on:
    tap:
      type: shell
      cmd: '/Applications/Hammerspoon.app/Contents/Frameworks/hs/hs -A -c "UDECK_ACTIONS.windowLeftHalf()"'
```

### Next / Previous Page

```yaml
- at: [0, 3]
  label: "Next"
  icon: test-icons/tab.png
  on:
    tap:
      type: page_next

- at: [0, 4]
  label: "Prev"
  icon: test-icons/find.png
  on:
    tap:
      type: page_prev
```

### Folder

```yaml
- at: [0, 0]
  label: "Apps"
  icon: test-icons/apps-folder.png
  folder:
    buttons:
      - at: [0, 0]
        label: "Notes"
        icon: test-icons/notes.png
        on:
          tap:
            type: shell
            cmd: "open -a Notes"
```

### Toggle

```yaml
- at: [1, 1]
  label: "Mode"
  icon: test-icons/apps-folder.png
  on:
    tap:
      toggle:
        - label: "A"
          icon: test-icons/apps-folder.png
          action:
            type: shell
            cmd: "echo a"
        - label: "B"
          icon: test-icons/work-folder.png
          action:
            type: shell
            cmd: "echo b"
```

### Chain

```yaml
- at: [1, 2]
  label: "Chain"
  icon: test-icons/marker.png
  on:
    tap:
      chain:
        - action:
            type: shell
            cmd: "echo first"
        - delay_ms: 300
        - action:
            type: shell
            cmd: "echo second"
```

## Canonical Demo Config

The main living example is:

- [examples/test-config.yaml](../examples/test-config.yaml)

Use it as the fastest way to see supported combinations in practice.
