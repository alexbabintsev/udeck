# Config Reference

This file is the canonical reference for the `ulanzi-deck-go` YAML
config format.

When config functionality changes, update this document together with:

- [README.md](/Volumes/Black/Projects/ulanzi-deck-go/README.md:1)
- [CHANGELOG.md](/Volumes/Black/Projects/ulanzi-deck-go/CHANGELOG.md:1)

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
  project:
    terminal: "iTerm"
    editor: "PhpStorm"
    editor_subdir: "www"
```

#### `widgets.project`

Defaults for `widget.type: project`.

Fields:

- `terminal`
  - App name used by fallback focus behavior
  - Also passed to scripts as `PROJECT_TERMINAL`
- `editor`
  - Passed to scripts as `PROJECT_EDITOR`
- `editor_subdir`
  - Default subdirectory for editor target
  - Also used to build `PROJECT_EDITOR_TARGET`

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

### Hammerspoon via `hs.ipc`

Recommended pattern for Hammerspoon integration on macOS:

```yaml
tap:
  type: shell
  cmd: '/Applications/Hammerspoon.app/Contents/Frameworks/hs/hs -A -c "ULANZI_ACTIONS.windowLeftHalf()"'
```

Expected Hammerspoon setup:

- `require("hs.ipc")` in `init.lua`
- a global table such as `ULANZI_ACTIONS = { ... }`

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

### `type: close_all_projects`

Runs the `stop` lifecycle for every configured `widget.type: project`.

```yaml
tap:
  type: close_all_projects
```

Behavior:

- visits all configured project widgets, even if their folder has not
  been opened yet in the current run
- uses each project's explicit `scripts.stop` when present
- otherwise falls back to the built-in tmux session shutdown logic

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

- `memory`
- `freqtrade`
- `spotify_play_pause`
- `spotify_next`
- `spotify_artwork`
- `wide_dashboard`
- `project`

### `widget.type: memory`

```yaml
widget:
  type: memory
  poll_ms: 2000
  apps:
    - "Google Chrome"
    - "Slack"
```

Fields:

- `type`
- `poll_ms`
  - optional
  - minimum `1000`
- `apps`
  - optional list of app names
  - used by the widget's restart behavior

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

Double-wide multifunction widget for the physical `3_2` slot.

Modes cycle on tap:

- `clock`
- `weather`
- `clock+weather`
- `spotify`

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
  - if omitted, weather modes render a setup placeholder
- `poll_ms`
  - optional weather refresh override
  - minimum effective value is `60000`

### `widget.type: freqtrade`

Combined freqtrade widget with two modes:

- `summary`
  - closed P&L on top
  - open P&L on bottom
- `log`
  - last 3 closed trades by `profit_abs`

Tap the widget to cycle `summary -> log -> summary`.

```yaml
widget:
  type: freqtrade
  title: "BTC"
  dsns: |
    trader:secret@http://10.8.0.1:9910
    trader:secret@http://10.8.0.2:9910
  poll_ms: 10000
```

Fields:

- `type`
- `dsns`
  - required
  - newline-separated list of `user:password@http://host:port`
  - empty lines and lines starting with `#` are ignored
- `title`
  - optional short title shown at the top of the widget
- `poll_ms`
  - optional
  - minimum `3000`

Behavior:

- polls freqtrade REST APIs directly from `ulanzi-deck`
- no separate daemon or localhost bridge is needed
- aggregates all configured bots into one combined view
- ignores failed bots and keeps rendering data from the remaining ones

### `widget.type: project`

Project widget manages a tmux-backed project lifecycle.

```yaml
widget:
  type: project
  project_dir: project-demo
  session: "project_demo"
  editor_subdir: "www"
  hold_ms: 500
  on:
    tap: start
    double_tap: focus
    hold: stop
  scripts:
    start: start
    stop: stop
    focus: focus
```

Fields:

- `type`
- `project_dir`
  - required
  - relative paths resolve relative to the config file
- `session`
  - optional tmux session name override
  - default is derived from the project directory name
- `editor_subdir`
  - optional per-project editor subdirectory override
- `poll_ms`
  - optional
  - minimum `1000`
- `hold_ms`
  - optional
  - minimum `100`
- `on`
  - optional mapping from gestures to project actions
- `scripts`
  - optional overrides for script paths

#### Project Widget Gesture Values

Allowed values for `widget.on.*`:

- `start`
- `stop`
- `focus`

Example:

```yaml
on:
  tap: start
  double_tap: focus
  hold: stop
```

#### Project Scripts

Default script lookup:

- `<project_dir>/start`
- `<project_dir>/stop`
- `<project_dir>/focus`

Override example:

```yaml
scripts:
  start: scripts/dev-start
  stop: scripts/dev-stop
  focus: scripts/dev-focus
```

Project scripts receive environment variables such as:

- `PROJECT_DIR`
- `PROJECT_SESSION`
- `PROJECT_TERMINAL`
- `PROJECT_EDITOR`
- `PROJECT_EDITOR_SUBDIR`
- `PROJECT_EDITOR_TARGET`
- `PROJECT_CHROME_PROJECT`

Current demo behavior:

- `start`
  - creates a tmux session if missing
  - boots the default pane layout
  - opens editor and Chrome project when configured
  - then calls `focus`
- `stop`
  - kills the tmux session
  - closes the Chrome project through Slate when available
- `focus`
  - opens a new tab in `iTerm` or `Terminal`
  - runs `tmux attach-session -t <session>`

## Path Resolution Rules

Relative paths are resolved relative to the config file for:

- `button.icon`
- `widget.project_dir`
- nested icons inside `toggle` steps

`~/...` is expanded for icon paths and widget project paths.

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
- valid toggle structure
- valid chain structure
- known widget types
- `project` widgets require `project_dir`
- known project widget gesture actions

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
      cmd: '/Applications/Hammerspoon.app/Contents/Frameworks/hs/hs -A -c "ULANZI_ACTIONS.windowLeftHalf()"'
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

### Project Widget

```yaml
widgets:
  project:
    terminal: "iTerm"
    editor: "PhpStorm"
    editor_subdir: "www"

pages:
  - name: "Projects"
    buttons:
      - at: [0, 0]
        label: "Demo"
        icon: test-icons/work-folder.png
        widget:
          type: project
          project_dir: project-demo
          on:
            tap: start
            double_tap: focus
            hold: stop
          hold_ms: 500
```

## Canonical Demo Config

The main living example is:

- [examples/test-config.yaml](/Volumes/Black/Projects/ulanzi-deck-go/examples/test-config.yaml:1)

Use it as the fastest way to see supported combinations in practice.
