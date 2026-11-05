# Changelog

All notable changes to this project should be documented in this file.

The format is intentionally lightweight and follows the repository's
actual development milestones rather than formal releases.

## Unreleased

- Keep updating this section for user-visible changes that have not been
  grouped into a tagged release yet.
- The `project` widget's fallback focus now attaches tmux in a new
  Ghostty tab, as it already did for iTerm and Terminal.
- New widgets: `volume` (mute or up/down with hold-to-repeat; handles
  devices without a mute switch or with fixed volume), `microphone`
  (system-wide input mute), `audio_output` (switch the default output
  device) and `media` (system play/pause, next, previous keys).
- New `clock` widget: local time or a world clock for an IANA time
  zone, with the date (red on weekends) and the city name.
- New `timer`, `stopwatch` and `pomodoro` widgets: tap to start/pause,
  hold to reset; timers and pomodoro phases ring with a system sound,
  even when their page is hidden.
- New `type: window` action: halves, quarters, thirds, maximize,
  center, next/previous display and restore for the focused window,
  natively through the Accessibility API instead of Hammerspoon.
- New system monitor widgets: `cpu`, `disk` (free space), `network`
  (download/upload rate) and `battery`, next to `memory`.
- General-purpose widgets share one look: level ring or large icon with
  an upper-case label, with the same state colors. The `memory` widget
  moved to it.
- Lucide icons with rounded rectangles (349 of them: the mic, batteries,
  panels, ...) are drawn with round corners again instead of square ones.
- `docs/config.md` covers `weather_day` and `site` and no longer lists
  the removed dashboard modes.
- `run` without `--config` now reads
  `$XDG_CONFIG_HOME/ulanzi-deck/config.yaml` (default
  `~/.config/ulanzi-deck/config.yaml`) instead of the tracked demo
  profile, so personal configs and tokens stay out of the checkout.
- `close_all_projects` no longer freezes the deck while stop scripts run.
- The wide dashboard shows fetched weather at once instead of on the next
  minute change; it no longer has a Spotify mode (tap does nothing).
- Freqtrade `dsns` accept passwords containing `@`.
- Chains that combine `delay_ms` with `page_next`/`page_prev` run off the
  event loop, so their delays no longer stall key handling.
- HTTP error messages and debug logs no longer include URL query strings
  or credentials; widgets warn when credentials would go over plain HTTP
  to a public host.
- `site` widgets: the minimum `poll_ms` is now 30000, matching the
  effective interval.
- Generic `examples/project-demo` scripts and relative documentation links.
- CI on GitHub Actions: gofmt, vet, staticcheck, race tests, govulncheck.
- Sleep/wake and Spotify notifications use IOKit and CoreFoundation
  in-process instead of two `swift` interpreter helpers (~190 MB RSS
  together, and an "install developer tools" prompt on Macs without
  them). System sleep now waits until the deck is dimmed and closed.
- Shutdown dimming is driven by `SIGTERM`/`SIGHUP` (also sent on logout,
  terminal close and service stop); Ctrl-C still leaves the deck as is.
- Encoded key images are cached by content: a full page builds in about
  0.5 ms instead of 12 ms (24 MB less garbage per page switch).
- Widgets render into memory instead of temp PNG files; no `ulanzi-*`
  directories are left in `$TMPDIR`.
- `weather_day` and `wide_dashboard` widgets for the same city share one
  forecast request, which now also carries the current conditions.
- The freqtrade widget shows OFFLINE when no bot answers and NO TRADES
  when there is nothing to show, instead of loading dots forever.
- `run` warns when the config file is writable by other users.
- Stop the periodic full-page re-upload: the idle keepalive is now a
  single brightness (or label-style) packet, so the keys no longer
  flash every 30 seconds.
- Widget updates are coalesced and sent as one partial update for the
  visible widgets only. Updates from widgets on other pages no longer
  trigger a full-page redraw, and bursts of updates are never dropped.
- Toggle state changes redraw only the toggled key.
- Device recovery reconnects the HID handle and repeats only the failed
  request; the full page is re-sent only when the device really vanished
  from the bus. Icon/widget render errors no longer trigger recovery.
- The stale handle is now closed (not leaked) on recovery, which on
  macOS's exclusive open caused repeated privilege-violation retries.
- A transfer interrupted by an in-place reopen restarts from its header
  packet instead of sending orphaned continuation chunks.
- When recovery fails the daemon stays up and redraws the page once the
  device is reconnected, instead of exiting.
- Shell, key, HTTP and delayed chain actions run on a worker goroutine
  so they no longer block HID reads; shell children are reaped.
- New `--debug` flag / `ULANZI_DEBUG=1` for verbose diagnostics.
- Lucide icons are embedded into the binary.
- Widget HTTP responses are size-limited and remote artwork dimensions
  are checked before decoding.
- Key images are sent as WebP on firmware 5.3.6+ (detected at startup,
  `device.image_format: auto|webp|jpeg`) and at quality 75 by default
  (`device.image_quality`). Together they cut uploads to about a third:
  the main page went from ~60 to 18 packets. libwebp is compiled in via
  cgo, no extra install is needed.
- The memory widget reads system memory in-process (sysctl and
  host_statistics64) instead of running `sysctl` and `vm_stat` on
  every poll, i.e. two fewer processes spawned every two seconds.
- Fixed the device hanging for up to a minute (and finally rebooting)
  after a write error: an interrupted upload is now completed with
  filler packets before it is resent. Restarting from the header made
  the D200 swallow the resend as the tail of the broken upload.
- A device reboot is detected by its new IORegistry path, even when it
  is back within milliseconds, and the full page is uploaded again
  (previously only changed widgets were sent and the rest of the keys
  stayed on the firmware's default icons).
- Page ZIPs never end exactly on a 1024-byte packet boundary (as Ulanzi
  Studio's writer). It removed stalls seen while the device was in a bad
  state; after a reboot that ZIP no longer stalled, so this is a
  precaution rather than a proven fix.
- The keepalive is now the device-info request (command 0x0003) that
  Ulanzi Studio uses to check the device is connected; `info` actually
  requests the info, and `run` logs serial, firmware and hardware
  version at startup.
- Device writes run on their own goroutine, so key presses are handled
  even while the D200 refuses writes for seconds. Queued uploads are
  coalesced (only the newest page / newest image of a key is sent), and
  keys already showing the same content are skipped.
- The Spotify play/pause icon flips immediately on tap.
- Widgets that are not on screen pause their polling and refresh as soon
  as their page is shown again (previously a page opened once, e.g.
  freqtrade, kept polling its API forever). The Spotify service stops
  calling osascript while no Spotify widget is visible.
- Widget text now renders on every machine: the embedded Go Mono family
  is the default and `widgets.font` selects a custom TTF/OTF. Previously
  text silently disappeared unless a specific commercial font was
  installed.
- Fixed an infinite loop when dashboard text did not fit even at the
  minimum font size (could hang the event loop on long track names).
- Closing one `weather_day` widget no longer unsubscribes a different
  widget of the same city; closed widgets are actually unsubscribed.
- Widget background loops stop immediately on close instead of at their
  next tick (up to minutes after a config reload).
- The dashboard no longer starts overlapping weather requests while the
  API is slow, and Spotify artwork is fetched outside the service lock.
- AppleScript numbers in comma-decimal locales are parsed correctly.
- `weather_day` wind speed is requested in m/s to match its field.
- A multi-packet transfer that hits a write error is restarted from its
  header instead of retrying the failed chunk in place (which could
  corrupt the page ZIP so the device kept showing the old page).
- Internal: shared widget lifecycle (`widgetBase`), single HTTP/JSON
  helper, single text-fitting routine, event loop behind a `deck`
  interface, testable CLI; removed dead code. Test coverage ~93%.
- `device.SendPacket` / `device.ReadPacket` now silently re-open the HID
  handle in place when macOS hidapi returns "Device is disconnected" /
  "no such device" on a still-physically-connected D200. The device
  stays in host mode and the screen contents survive, so this avoids
  the visible full-page redraw (key flicker) that previously fired
  several times per hour from spurious IOHIDDevice client teardowns.
- In-place reopen now uses the same retry loop as the initial `Open`
  (privilege violations, busy device, and transient "no HID devices
  found" results are all retried for up to a few seconds) and closes
  the stale hid handle before opening a fresh one. This handles the
  common macOS pattern where the old IOHIDDevice client lingers in
  IORegistry for a few hundred milliseconds after teardown.
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
  [examples/project-demo](examples/project-demo).
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
  [docs/config.md](docs/config.md)
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
- Canonical demo profile in [examples/test-config.yaml](examples/test-config.yaml).

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
