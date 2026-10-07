# ulanzi-deck-go

Headless daemon for the **Ulanzi D200** stream deck. Single Go binary,
YAML config, no GUI. Replaces the official "Ulanzi Studio" app for the
subset of features implemented here and is designed for direct local
automation.

> Status: **v2 workbench**. Direct HID control works, nested directories
> work, widgets work, config hot reload works, and the runtime keeps the
> device awake / recovers from common HID interruptions. The runtime
> also handles macOS sleep, wake, and shutdown events. SmallView and
> richer profile management are still pending.

## Why

Ulanzi Studio holds an exclusive HID lock on the D200 and is the only
sanctioned way to interact with it. This daemon talks the wire protocol
directly so you can drive the device from a config file and integrate
it with whatever automation you already run.

## Requirements

- macOS (Linux/Windows untested in v1)
- Go 1.27.1+
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
- Upload and refresh multi-page D200 layouts, including the double-wide bottom-row slot
- Global label style and brightness control
- Local icon loading and resize to the physical slot size (`196x196`, except `at: [2, 3]` = `458x196`)
- Nested button directories with automatic `Back`
- Top-level pages with looping `page_next` / `page_prev`
- Unified button handlers via `on:`
  - `tap`
  - `double_tap`
  - `hold`
  - `repeat`
- Toggle sequences for action buttons, including per-step label/icon/action
- Chains for action buttons, including `delay_ms` steps between actions
- Action types:
  - `shell`
  - `key`
  - `http`
  - `page_next`
  - `page_prev`
  - `close_all_projects`
- Widgets:
  - `memory`
  - `freqtrade`
  - `spotify_play_pause`
  - `spotify_next`
  - `spotify_artwork`
  - `wide_dashboard`
  - `weather_day`
  - `project`
- Config hot reload while `run` is active
- Runtime keepalive to keep the deck out of screensaver
- Auto-recovery on common HID read/write/open failures
- Widget instances are created on first page visit and then cached
- Spotify shared runtime is prewarmed when the config contains Spotify widgets
- Widget icon files are only rewritten when the rendered frame actually changes
- Visible widget refreshes now update only the changed slot instead of reflashing the whole page
- macOS power-event handling for sleep / wake / shutdown
- Startup retry/backoff to survive transient HID access errors

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
./ulanzi-deck run
./ulanzi-deck run --debug   # or ULANZI_DEBUG=1
```

`run` uploads the configured button layout, switches the device into
host-controlled mode, applies label style / brightness, starts listening
for button events, hot-reloads the config when the YAML changes, and
hot-reloads static button icons when their image files change, and
sends a lightweight keepalive packet (re-applied brightness) when the
link is idle so the firmware screensaver does not take over. Widget and
toggle changes redraw only the affected keys; the full layout is
uploaded only on start, page switch, config reload, wake, or after the
device was actually reset. On macOS it also listens for system sleep, wake,
and shutdown notifications so it can dim/close the device cleanly before
sleep and restore the current page after wake.

If `--config` is omitted, `run` uses `examples/test-config.yaml`.

If something goes wrong on the device (frozen display, garbled output),
**unplug the USB cable** to power-cycle. No persistent damage is
possible from bad packets.

## Config

Use these two files together:

- [docs/config.md](/Volumes/Black/Projects/ulanzi-deck-go/docs/config.md:1) for the full config reference
- [examples/test-config.yaml](/Volumes/Black/Projects/ulanzi-deck-go/examples/test-config.yaml:1) for the canonical demo profile

The demo config includes:

- top-level pages with looped next/prev navigation
- nested directories
- standard shell / key / http actions
- `memory` widget
- `spotify_*` widgets
- `wide_dashboard` widget for the double-wide `3_2` slot
- `weather_day` widget for the double-wide `3_2` slot
- `project` widget with demo `start/stop/focus` scripts
- Lucide icon-pack examples via `icon: lucide/...`
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
- `device.icon_margins`: optional global static-icon insets
  - `top`, `right`, `bottom`, `left`: non-negative integers
  - useful when the D200 distorts pixels near the physical screen edges
- `widgets.project`: optional global defaults for project widgets
  - `terminal`: app name used by default `focus` behavior and passed to
    scripts as `PROJECT_TERMINAL`
  - `editor`: app name passed to scripts as `PROJECT_EDITOR`
  - `editor_subdir`: optional subdirectory opened by the editor by
    default, for example `www`
- `pages[]`
  - `name`: optional human-readable page name
  - `buttons[]`
    - `at`: `[row, col]`, zero-based, `row=0..2`, `col=0..4`, except `at: [2, 4]` does not exist because `at: [2, 3]` is double-wide
    - `label`: button text
    - `icon`: local image path or icon-pack id such as `lucide/arrow-right`
      - raster files: `~/...` is expanded; images are resized to the target slot size and encoded as JPEG before upload
      - pack icons: rendered on a black background as a centered `128x128` icon
    - `icon_color`: optional hex `RRGGBB` override for pack/SVG icons; defaults to white
    - `icon_margins`: optional per-button override for raster icon insets; pack icons ignore margins
    - `folder.buttons`: nested page; runtime injects a `Back` button
      - if the current page has `widget.type: wide_dashboard` at `at: [2, 3]`, that wide widget becomes the folder `Back` button automatically
    - `widget`
      - `type: memory`
        - `apps`: optional app list restarted on press
        - `poll_ms`: optional polling override
      - `type: freqtrade`
        - `dsns`: newline-separated `user:password@http://host:port`
        - `title`: optional short title shown at the top
        - `poll_ms`: optional polling override, minimum `3000`
      - `type: spotify_play_pause`
        - tap toggles play/pause
        - hold skips to next track
      - `type: spotify_next`
      - `type: spotify_artwork`
      - `type: wide_dashboard`
        - only valid at `at: [2, 3]`
        - `city`: optional weather city name
        - `poll_ms`: optional weather refresh override
        - tap cycles `clock -> spotify`
        - inside folders it turns into the folder `Back` button
        - on top-level pages `2+` it shows the page number in the corner
        - clock mode refreshes on minute changes instead of every second
      - `type: project`
        - `project_dir`: project root; relative paths are resolved relative
          to the config file
        - `session`: optional tmux session override; defaults to the project
          directory name
        - `editor_subdir`: optional per-project override for the editor
          target subdirectory
        - `on`: optional gesture mapping for project widgets
          - `tap`: `start | stop | focus`
          - `double_tap`: `start | stop | focus`
          - `hold`: `start | stop | focus`
          - `repeat`: `start | stop | focus`
        - `hold_ms`: optional stop threshold override
        - `scripts.start`: optional override for the start script path
        - `scripts.stop`: optional override for the stop script path
        - `scripts.focus`: optional override for the focus script path
    - `on`
      - `tap`
      - `double_tap`
      - `hold`
      - `repeat`
      - each gesture accepts either:
        - a direct action payload
        - `toggle:` with 2+ steps
        - `chain:` with ordered `action` / `delay_ms` steps
      - optional timing overrides:
        - `hold_ms`
        - `double_tap_ms`
        - `repeat_every_ms`
    - each handler uses an action payload:
      - `type: shell` with `cmd: "..."` to run a shell command
      - `type: key` with `key: "cmd shift m"`-style shortcuts
      - `type: http` with `url: "http://127.0.0.1:..."` to call a local or remote endpoint
      - `type: page_next` to switch to the next top-level page
      - `type: page_prev` to switch to the previous top-level page
      - `type: close_all_projects` to run `stop` for every configured
        `widget.type: project`
- `smallview`: accepted but currently reserved for future work

Example:

```yaml
device:
  brightness: 70
  icon_margins:
    top: 8
    right: 8
    bottom: 8
    left: 8
  label_style:
    align: bottom
    color: "FFFFFF"
    font_name: "Roboto"
    show_title: true
    size: 10
    weight: 80

widgets:
  project:
    terminal: "Ghostty"
    editor: "PhpStorm"
    editor_subdir: "www"

pages:
  - name: "Main"
    buttons:
      - at: [0, 0]
        label: "Folder"
        icon: test-icons/apps-folder.png
        icon_margins:
          top: 12
          right: 12
          bottom: 12
          left: 12
        folder:
          buttons:
            - at: [0, 0]
              label: "Terminal"
              icon: test-icons/terminal.png
              on:
                tap:
                  type: shell
                  cmd: "open -a Terminal"

      - at: [0, 1]
        label: "Next Page"
        icon: lucide/arrow-right
        icon_color: "33BBFF"
        on:
          tap:
            type: page_next

      - at: [0, 2]
        widget:
          type: spotify_artwork

      - at: [0, 3]
        label: "Project"
        icon: test-icons/work-folder.png
        widget:
          type: project
          project_dir: project-demo
          on:
            tap: start
            double_tap: focus
            hold: stop
          hold_ms: 800

  - name: "Shortcuts"
    buttons:
      - at: [0, 0]
        label: "Prev Page"
        icon: test-icons/system-folder.png
        on:
          tap:
            type: page_prev

      - at: [0, 1]
        label: "Double"
        icon: test-icons/tab.png
        on:
          tap:
            type: key
            key: "cmd t"
          double_tap:
            type: shell
            cmd: "open README.md"

      - at: [1, 0]
        label: "Mode A"
        icon: test-icons/apps-folder.png
        on:
          tap:
            toggle:
              - label: "Mode A"
                icon: test-icons/apps-folder.png
                action:
                  type: shell
                  cmd: "echo toggle-a"
              - label: "Mode B"
                icon: test-icons/media-folder.png
                action:
                  type: shell
                  cmd: "echo toggle-b"

      - at: [1, 1]
        label: "Chain"
        icon: test-icons/work-folder.png
        on:
          tap:
            chain:
              - action:
                  type: shell
                  cmd: "echo chain-a"
              - delay_ms: 400
              - action:
                  type: shell
                  cmd: "echo chain-b"
```

Notes:

- `shell` actions are launched asynchronously.
- `chain` steps run strictly in order. `delay_ms` pauses before the next
  step. Since `shell` actions are asynchronous, a delay spaces out
  launches but does not wait for the shell command to finish.
- `key` actions are implemented on macOS; on other platforms they
  currently return an unsupported error.
- `http` actions are useful for local automation bridges such as
  Hammerspoon.
- `page_next` and `page_prev` loop across top-level pages and reset any
  currently open nested `folder`.
- `run` hot-reloads the YAML config automatically when the file changes.
- `run` also watches configured static icon files and reloads the page
  when those assets change on disk.
- For Hammerspoon on macOS, prefer `type: shell` with the bundled
  `hs` CLI and `ULANZI_ACTIONS.*()` over `type: key` hotkeys or a custom
  HTTP bridge. This avoids flaky synthetic shortcuts and long-lived
  `hs.httpserver` state.
- Gesture semantics:
  - `tap` fires on release
  - `double_tap` fires on the second tap
  - `hold` fires as soon as the hold threshold is reached
  - `repeat` starts after the hold threshold when both are set
- Toggle semantics:
  - toggle steps run in a loop on the gesture that owns them
  - the current step controls the button's visible `label` / `icon`
  - after the step runs, the button advances to the next step
  - only one gesture per button can define a toggle sequence
- Widgets write icons atomically so page refreshes do not race on partial
  PNG files.
- Widget renders skip identical frames, and visible widget updates use a
  single-slot partial refresh path to reduce CPU use and avoid full-page
  flicker.
- Project widgets detect status from a tmux session and run
  `start` / `stop` / `focus` scripts from the configured project
  directory. By default those paths are `<project_dir>/start`,
  `<project_dir>/stop`, and `<project_dir>/focus`.
- Project widgets can remap gestures explicitly with `widget.on`, for
  example `tap: start`, `double_tap: focus`, `hold: stop`.
- Project widgets inherit optional global defaults from `widgets.project`
  and pass them into scripts as `PROJECT_TERMINAL` and
  `PROJECT_EDITOR`.
- Project widgets also compute `PROJECT_EDITOR_TARGET`, which resolves to
  either `<project_dir>` or `<project_dir>/<editor_subdir>` depending on
  global and per-widget config.
- The built-in fallback `focus` behavior tries to open a new terminal tab
  and run `tmux attach-session -t <session>` for `iTerm` and `Terminal`.
  Other terminals currently fall back to opening the project directory.
- The demo project scripts also honor `PROJECT_CHROME_PROJECT`,
  `SLATE_CLI_PATH`, and `SLATE_EXT_ID` so they can open/close a Chrome
  project through Slate when that tooling is available.
- The demo `start` script is idempotent: if the tmux session already
  exists, it jumps straight to `focus`. On first launch it creates a
  three-pane `main` window and starts:
  - `PROJECT_WEB_CMD`
  - `PROJECT_QUEUE_CMD`
  - `PROJECT_CLAUDE_CMD`
- The defaults for those demo commands match the current Laravel-style
  workflow: `npm run dev`, `php85 artisan queue:listen`, and `claude`
  inside `PROJECT_EDITOR_TARGET`.
- Project scripts are invoked through `/bin/sh`, so they do not need the
  executable bit as long as the script contents are valid shell.
- Spotify widgets use one internal shared service inside the
  `ulanzi-deck` process; no separate `spotify-daemon` is required.
- Freqtrade widget polls configured REST APIs directly; no separate
  freqtrade daemon or local bridge is required.
- Freqtrade widget text uses `PragmataPro` when that font is available
  locally, with a fallback renderer otherwise.
- Widget instances are created when you first open the page that uses
  them and then stay cached until config reload or shutdown.
- Spotify uses one shared runtime that is prewarmed when the config
  contains Spotify widgets, so entering a Spotify directory does not
  incur a cold-start delay.
- Spotify artwork fetch still uses network access to resolve current
  cover art from Spotify track metadata.
- The runtime periodically reapplies device state as a keepalive and
  attempts to reopen the device automatically after common HID failures.
- On macOS, system sleep and shutdown dim the deck and close the HID
  session cleanly; wake triggers a reopen-and-restore flow.
- Device open uses retry/backoff, so transient startup failures such as
  `privilege violation` often recover automatically without needing to
  restart the program manually.
- Key actions on macOS are sent as full modifier-down / key /
  modifier-up sequences rather than one flagged key event, which is more
  reliable for shortcuts like `cmd t` and `cmd shift 4`.
- If the active config contains any `type: key` handlers, `run` now
  proactively asks macOS for Accessibility access during startup.
- Hammerspoon integrations work best through `require("hs.ipc")` plus
  shell actions that invoke `/Applications/Hammerspoon.app/Contents/Frameworks/hs/hs`.
- Static button icons support global and per-button margins, which lets
  you keep important pixels away from the D200's edge distortion zone.

## Troubleshooting

- `hid_open_path ... exclusive access`
  - Quit `Ulanzi Studio` and any other process that may still own the
    device.
- `hid_open_path ... privilege violation`
  - Keep the process running for a moment before retrying manually; the
    runtime now retries with backoff during startup, and this error is
    often transient on macOS.
- The deck falls into screensaver or stops responding
  - The runtime already sends periodic keepalive packets. If the
    screen is still frozen, unplugging and reconnecting USB remains the
    safest hard reset. While the device is unplugged the daemon keeps
    running and redraws the page when it comes back.
- Keys flicker / the device seems to disconnect
  - Run with `--debug` (or `ULANZI_DEBUG=1`). Every HID write/read
    error, retry, handle reopen, recovery decision, page upload and
    widget fetch failure is logged with a `[debug]` prefix.
- Spotify artwork updates but then breaks
  - Artwork icons are written atomically now; if updates still fail,
    check Spotify itself is running and that network access is available
    for cover-art lookup.
- Key actions do nothing
  - Grant Accessibility permission to the app that launched
    `ulanzi-deck` in macOS System Settings. For example, if you run it
    from Terminal or iTerm, that terminal app must be allowed under
    Privacy & Security → Accessibility. The runtime now also triggers
    the native macOS prompt automatically when key actions are
    configured, but the user still needs to approve it.

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
