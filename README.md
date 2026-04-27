# ulanzi-deck-go

Minimal headless daemon for the **Ulanzi D200** stream deck. Single Go
binary, YAML config, no GUI. Replaces the official "Ulanzi Studio" app
for the subset of features it implements.

> Status: **early v2**. Reads device info, uploads a button page ZIP,
> renders button labels and icons, listens for button events, applies
> brightness and label style, and dispatches shell / keyboard actions.
> SmallView support and richer profile management are still pending. See
> [Roadmap](#roadmap).

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

- Query basic device info
- Listen for button press/release events
- Upload a 5x3 button page definition on startup
- Render per-button text labels
- Render per-button icons from local image files
- Apply global device brightness
- Apply global button label style
- Dispatch button actions:
  - `shell`: run a shell command asynchronously
  - `key`: send a macOS keyboard shortcut

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
./ulanzi-deck run --config examples/config.yaml
```

`run` uploads the configured button layout, switches the device into
host-controlled mode, applies label style / brightness, and starts
listening for button events.

If something goes wrong on the device (frozen display, garbled output),
**unplug the USB cable** to power-cycle. No persistent damage is
possible from bad packets.

## Config

See [examples/config.yaml](examples/config.yaml).

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
  - `action`
    - `type: shell` with `cmd: "..."` to run a shell command
    - `type: key` with `key: "cmd shift m"`-style shortcuts
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
    label: "Term"
    icon: ~/decks/icons/term.png
    action:
      type: shell
      cmd: "open -a Terminal"

  - at: [2, 0]
    label: "Mute"
    action:
      type: key
      key: "cmd shift m"
```

Notes:

- `shell` actions are launched asynchronously.
- `key` actions are implemented on macOS; on other platforms they
  currently return an unsupported error.

## Roadmap

- v2: SmallView STATS / CLOCK modes (system stats feed)
- v2: more button/page management polish and validation
- v3: SmallView BACKGROUND image (needs more reverse-engineering)
- v3: hot config reload, multi-page profiles, screensaver, launchd plist
- Library split: migrate `internal/` → `pkg/ulanzid200/` and publish

## Credits

Wire protocol reverse-engineered by [redphx/strmdck][strmdck] (Python,
MIT). This is an independent Go reimplementation; no code is lifted from
GPL-licensed sources.

[strmdck]: https://github.com/redphx/strmdck

## License

MIT — see [LICENSE](LICENSE).
