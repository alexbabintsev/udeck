# ulanzi-deck-go

Minimal headless daemon for the **Ulanzi D200** stream deck. Single Go
binary, YAML config, no GUI. Replaces the official "Ulanzi Studio" app
for the subset of features it implements.

> Status: **v1 (MVP)**. Reads device info, dumps button events, sets
> brightness. Does not yet upload key images, drive the SmallView, or
> dispatch actions. See [Roadmap](#roadmap).

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

If something goes wrong on the device (frozen display, garbled output),
**unplug the USB cable** to power-cycle. No persistent damage is
possible from bad packets.

## Config

See [examples/config.yaml](examples/config.yaml). v1 only acts on
`device.brightness`; other fields are accepted to keep configs
forward-compatible.

## Roadmap

- v2: ZIP-upload buttons + icons + label style + shell/keyboard actions
- v2: SmallView STATS / CLOCK modes (system stats feed)
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
