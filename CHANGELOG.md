# Changelog

All notable changes to this project should be documented in this file.

The format is intentionally lightweight and follows the repository's
actual development milestones rather than formal releases.

## Unreleased

- Keep updating this section for user-visible changes that have not been
  grouped into a tagged release yet.
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
