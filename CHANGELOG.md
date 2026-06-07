# Changelog

All notable changes to this project should be documented in this file.

The format is intentionally lightweight and follows the repository's
actual development milestones rather than formal releases.

## Unreleased

- Keep updating this section for user-visible changes that have not been
  grouped into a tagged release yet.
- `device.SendPacket` now retries on `IOHIDDeviceSetReport` failures with
  vendor code `0xE0005000` (transient macOS HID write error), preventing
  spurious full-page reinitializations / screen flicker.
- Runtime diagnostic messages on stderr (sleep/wake, recover, config
  reload, action errors, etc.) now go through the standard `log` package
  and are prefixed with a `YYYY/MM/DD HH:MM:SS.uuuuuu` timestamp so the
  frequency of recurring errors is visible at a glance. CLI usage/error
  output is unchanged.
- `recoverDevice` now tries to refresh the current page on the existing
  HID handle before detaching and reopening the device. The reopen path
  is still used when the lightweight refresh fails or after system wake.
  This eliminates visible flicker for transient HID errors where the
  device is still actually responsive.
- HID stability: macOS `hid_read_timeout: error waiting for more data` is
  no longer treated as fatal — it's a soft poll miss returned as empty
  read, eliminating spurious device recoveries and UI flicker during
  power transitions or read contention.
- HID writes now retry on `IOHIDDeviceSetReport` "device not responding"
  (0xE00002ED) and "not ready" (0xE00002D8). Retry budget increased to 8
  attempts at 60 ms — transient setReport failures recover in-place
  instead of triggering a full device reopen.
- Inbound `cmd=0x010B` frames are now recognized as `LABEL_STYLE_ACK`
  and silently consumed, replacing the noisy `UNKNOWN cmd=010B` log
  line that fired after every page apply.
- `project` widget redesign: black background, large centered seed icon,
  uppercase title rendered via `drawText` with auto shrink-to-fit, and a
  small green status dot in the top-left when the tmux session is running.
  Removed the old gradient, badge, glyph fallback, and footer status bar.
- `project` widget config now supports an explicit `title` field that
  overrides the button `label` and directory-derived name.
- Removed bundled `assets/Space_Mono/` TTF fonts (no longer referenced).
- `examples/project-demo`: rewrote `start` and `stop` to target iTerm
  instead of macOS Terminal, with AppleScript helpers that focus the
  existing tmux client tab or spawn a new one on start, and close the
  matching iTerm tab plus the editor project window on stop. Removed the
  separate `focus` script (logic merged into `start`).
- `page_next` / `page_prev` action buttons now render with a white rounded
  badge in the top-left corner showing the current page number, using the
  button's existing `icon` and `icon_color` — no config changes required.
- Removed page-number chip from `wide_dashboard` — it now appears only on
  the dedicated navigation button.
- Added `drawText` / `drawIcon` / `drawWeatherIconAt` layout helpers in
  `textblock.go`: edge-based anchors (`FromLeft`, `FromRight`, `CenterX`, …),
  9-position block alignment, automatic font shrink-to-fit (min 18 px), and
  optional letter-spacing (`tracking`).
- `wide_dashboard` clock mode redesigned: date and weekday use `drawText`,
  weekday turns red on weekends, weather summary shows icon + temperature
  on one row using colored Lucide weather icons matching the `weather_day`
  widget palette.
- `wide_dashboard` back mode redesigned: black background, Lucide
  `arrow-left` icon on the left, "BACK" label on the right, both centered
  vertically via `drawText`/`drawIcon`.
- Removed `weather` and `clockweather` dashboard modes — the dedicated
  `weather_day` widget covers that surface.
- Added `weather_day` widget: full-width daily forecast card for the
  double-wide `3_2` slot with current conditions, 5-day outlook, and
  Lucide weather icons.
- Added hot reload for configured static icon files, not just the YAML
  config file.
- `run` now defaults to `examples/test-config.yaml` when `--config` is
  omitted.
- Added toggle sequences for action buttons, with per-step
  `label` / `icon` / `action` and looping state on a single gesture.
- Added chained button actions with ordered `action` / `delay_ms`
  steps on a single gesture.
- Added `project` widget support with tmux-backed status and
  `start/stop/focus` shell scripts.
- Project widgets can now remap gestures explicitly, for example
  `tap: start` and `hold: stop`.
- Added global `widgets.project` defaults for terminal/editor selection,
  passed into project scripts and used by fallback focus behavior.
- Added `editor_subdir` support for project widgets and the derived
  `PROJECT_EDITOR_TARGET` script environment variable.
- Added demo project scripts in
  [examples/project-demo](/Volumes/Black/Projects/ulanzi-deck-go/examples/project-demo:1).
- Demo project `start` / `stop` scripts now also open/close a Chrome
  project through Slate when available.
- Project fallback focus now attaches to the running tmux session in a
  new `iTerm` or `Terminal` tab instead of only opening the project
  directory.
- Demo project `start` is now idempotent, opens the attached terminal
  tab through `focus`, and boots the default `tmux` service layout for
  `npm run dev`, `php85 artisan queue:listen`, and `claude`.
- Widget instances are created on first page visit and then cached for
  the lifetime of the active config.
- Spotify shared runtime is now prewarmed when Spotify widgets exist in
  the config, avoiding cold-start lag when entering Spotify pages.
- Polished widget rendering for Spotify and Memory states.
- Gesture handling now uses a shared engine for action buttons and
  widgets, with `double_tap` and hold-time `hold` behavior.
- macOS key actions now use a more reliable shortcut-posting path with
  explicit modifier down/up events and clearer Accessibility errors.
- `run` now proactively requests macOS Accessibility permission during
  startup when the active config contains key actions.
- Added global `device.icon_margins` and per-button `icon_margins` for
  static icons, so button artwork can avoid the D200 edge distortion
  zone.
- Added top-level `pages` with looping `page_next` / `page_prev`
  actions, and migrated the demo config to the new multi-page layout.
- Added a dedicated config reference in
  [docs/config.md](/Volumes/Black/Projects/ulanzi-deck-go/docs/config.md:1)
  with field-by-field schema documentation, allowed values, and
  examples.
- Added support for the physical double-wide `3_2` button slot, including
  correct icon sizing, icon-margin validation, and rejection of the
  nonexistent `4_2` slot.
- Added `close_all_projects` action to run the `stop` lifecycle for all
  configured `project` widgets.
- Hammerspoon integrations now prefer `hs.ipc` shell actions over
  synthetic hotkeys or a long-lived local HTTP bridge.
- Added combined `freqtrade` widget support with direct polling of one or
  more freqtrade REST APIs, no separate daemon required.
- Freqtrade widget rendering now uses `PragmataPro` when available to
  match the original plugin style.
- Added the `wide_dashboard` widget for the physical double-wide `3_2`
  slot with clock, weather, clock+weather, Spotify, page index, and
  contextual folder-back modes.
- Added direct Lucide icon-pack support via `icon: lucide/...`, including
  optional `icon_color` for pack/SVG icons.
- Pack/SVG icons now render on a black background as centered `128x128`
  artwork for button actions, while Lucide stroke width scales relative
  to the final icon size in a shared renderer used by both buttons and
  widgets.
- Reduced runtime widget overhead by skipping identical rendered frames,
  rewriting widget PNGs only when bytes change, and refreshing visible
  widget slots with partial updates instead of full-page reflashes.
- `wide_dashboard` clock-based modes now refresh on minute changes
  instead of every second, lowering idle CPU use and improving mode
  switching smoothness.
- Fixed the contextual `wide_dashboard` folder-back state so entering or
  leaving a folder no longer leaves the `3_2` slot blank.
- Simplified `spotify_play_pause` into a minimal black-card widget with
  green Lucide play/pause iconography, plus hold-to-skip-next behavior.
- Fixed the initial `spotify_play_pause` render so its icon is visible
  immediately on first page load instead of only after the first press.
- Reworked the `memory` widget into a minimal black-card design with a
  centered numeric reading, circular progress ring, and immediate
  `refresh-cw` busy state on tap.

## v2 Workbench

### Added

- Direct HID runtime for `Ulanzi D200` with `info`, `dump-events`,
  `set-brightness`, and `run`.
- Button icons, labels, shell actions, and keyboard actions.
- Global label-style configuration and startup label-style apply.
- Nested button directories with automatic `Back` navigation.
- HTTP actions for local automation bridges such as Hammerspoon.
- Memory widget with dynamic icon rendering.
- Spotify widgets:
  - `spotify_play_pause`
  - `spotify_next`
  - `spotify_artwork`
- Unified `on:` button handler model with:
  - `tap`
  - `double_tap`
  - `hold`
  - `repeat`
- Config hot reload during `run`.
- Canonical demo profile in [examples/test-config.yaml](/Volumes/Black/Projects/ulanzi-deck-go/examples/test-config.yaml:1).

### Changed

- Reworked examples into a single test/demo config with subdirectories
  for Spotify, Hammerspoon, gestures, and general actions.
- README now documents the current runtime model, widgets, gestures,
  recovery behavior, and troubleshooting.

### Fixed

- HID timeout handling for non-fatal read timeouts.
- Device initialization by sending an empty page ZIP so button events
  start flowing reliably.
- ZIP layout/structure handling for button page uploads.
- Strict config validation for duplicate button positions, label colors,
  label alignment, and conflicting button modes.
- More resilient HID open retry/backoff for transient macOS startup
  failures.
- Keepalive refreshes to keep the deck out of screensaver while running.
- Safer recovery around HID interruptions and write/read failures.
- macOS sleep, wake, and shutdown handling with close-and-restore flow.

## v1 MVP

### Added

- Initial single-binary Go daemon for direct control of the D200.
- YAML configuration and basic runtime loop.
