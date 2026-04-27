# ulanzi-deck-go

Headless daemon for the **Ulanzi D200** stream deck. Single Go binary,
YAML config, no GUI. Replaces the official "Ulanzi Studio" app for the
subset of features implemented here and is designed for direct local
automation.

> Status: **v2 workbench**. Direct HID control works, nested directories
> work, widgets work, config hot reload works, and the runtime keeps the
> device awake / recovers from common HID interruptions. SmallView and
> richer profile management are still pending.

## Why

Ulanzi Studio holds an exclusive HID lock on the D200 and is the only
sanctioned way to interact with it. This daemon talks the wire protocol
directly so you can drive the device from a config file and integrate
it with whatever automation you already run.

## Requirements

- macOS (Linux/Windows untested in v1)
- Go 1.21+
- `hidapi` system library: `brew install hidapi`

## Install

```sh
git clone https://github.com/alexbabintsev/ulanzi-deck-go
cd ulanzi-deck-go
go build -o ulanzi-deck ./cmd/ulanzi-deck
```

## Features

- Direct HID control of the D200 without Ulanzi Studio
- `info`, `dump-events`, `set-brightness`, and `run`
- Upload and refresh a full `5x3` page layout
- Global label style and brightness control
- Local icon loading and resize to `196x196`
- Nested button directories with automatic `Back`
- Unified button handlers via `on:`
  - `tap`
  - `double_press`
  - `long_press`
  - `repeat`
- Action types:
  - `shell`
  - `key`
  - `http`
- Widgets:
  - `memory`
  - `spotify_play_pause`
  - `spotify_next`
  - `spotify_artwork`
- Config hot reload while `run` is active
- Runtime keepalive to keep the deck out of screensaver
- Auto-recovery on common HID read/write/open failures

## Usage

**Quit Ulanzi Studio first** — it claims the HID device exclusively:

```sh
osascript -e 'tell application "Ulanzi Studio" to quit'
```

Then:

```sh
./ulanzi-deck info
./ulanzi-deck dump-events
./ulanzi-deck set-brightness 70
./ulanzi-deck run --config examples/test-config.yaml
```

`run` uploads the configured button layout, switches the device into
host-controlled mode, applies label style / brightness, starts listening
for button events, hot-reloads the config when the YAML changes, and
periodically refreshes device state to avoid falling back into the
firmware screensaver.

If something goes wrong on the device (frozen display, garbled output),
**unplug the USB cable** to power-cycle. No persistent damage is
possible from bad packets.

## Config

See [examples/test-config.yaml](examples/test-config.yaml). It is the
canonical demo profile and includes:

- nested directories
- standard shell / key / http actions
- `memory` widget
- `spotify_*` widgets
- gesture test buttons
- Hammerspoon integration examples

Current config surface:

- `device.brightness`: integer `0..100`
- `device.label_style`: optional global label style
  - `align`: `"top" | "bottom" | "center"`
  - `color`: hex `RRGGBB`, for example `"FFFFFF"`
  - `font_name`: currently defaults to `"Roboto"`
  - `show_title`: boolean
  - `size`: integer font size
  - `weight`: integer font weight
- `buttons[]`
  - `at`: `[col, row]`, zero-based, `col=0..4`, `row=0..2`
  - `label`: button text
  - `icon`: local image path; `~/...` is expanded; images are resized to
    `196x196` and encoded as JPEG before upload
  - `directory.buttons`: nested page; runtime injects a `Back` button
  - `widget`
    - `type: memory`
      - `apps`: optional app list restarted on press
      - `poll_ms`: optional polling override
    - `type: spotify_play_pause`
    - `type: spotify_next`
    - `type: spotify_artwork`
  - `on`
    - `tap`
    - `double_press`
    - `long_press`
    - `repeat`
    - optional timing overrides:
      - `long_press_ms`
      - `double_press_ms`
      - `repeat_every_ms`
  - each handler uses an action payload:
    - `type: shell` with `cmd: "..."` to run a shell command
    - `type: key` with `key: "cmd shift m"`-style shortcuts
    - `type: http` with `url: "http://127.0.0.1:..."` to call a local or remote endpoint
- `smallview`: accepted but currently reserved for future work

Example:

```yaml
device:
  brightness: 70
  label_style:
    align: bottom
    color: "FFFFFF"
    font_name: "Roboto"
    show_title: true
    size: 10
    weight: 80

buttons:
  - at: [0, 0]
    label: "Folder"
    icon: test-icons/apps-folder.png
    directory:
      buttons:
        - at: [0, 0]
          label: "Terminal"
          icon: test-icons/terminal.png
          on:
            tap:
              type: shell
              cmd: "open -a Terminal"

  - at: [2, 0]
    widget:
      type: spotify_artwork

  - at: [4, 1]
    label: "Double"
    icon: test-icons/tab.png
    on:
      tap:
        type: key
        key: "cmd t"
      double_press:
        type: shell
        cmd: "open README.md"
```

Notes:

- `shell` actions are launched asynchronously.
- `key` actions are implemented on macOS; on other platforms they
  currently return an unsupported error.
- `http` actions are useful for local automation bridges such as
  Hammerspoon.
- `run` hot-reloads the YAML config automatically when the file changes.
- Widgets write icons atomically so page refreshes do not race on partial
  PNG files.
- Spotify widgets use an internal shared service; no separate
  `spotify-daemon` is required.
- Spotify artwork fetch still uses network access to resolve current
  cover art from Spotify track metadata.
- The runtime periodically reapplies device state as a keepalive and
  attempts to reopen the device automatically after common HID failures.

## Roadmap

- v2: SmallView STATS / CLOCK modes (system stats feed)
- v2: more widget polish and rendering improvements
- v3: SmallView BACKGROUND image (needs more reverse-engineering)
- v3: richer profile management, launchd plist
- Library split: migrate `internal/` → `pkg/ulanzid200/` and publish

## Credits

Wire protocol reverse-engineered by [redphx/strmdck][strmdck] (Python,
MIT). This is an independent Go reimplementation; no code is lifted from
GPL-licensed sources.

[strmdck]: https://github.com/redphx/strmdck

## License

MIT — see [LICENSE](LICENSE).
