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
- Widget runtimes now activate lazily for the currently visible page
  instead of all widget definitions in the config tree.
- Polished widget rendering for Spotify and Memory states.

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
  - `double_press`
  - `long_press`
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
