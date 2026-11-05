# Personal widgets

`freqtrade`, `site`, `project` and the `close_all_projects` action are
left out of the default build and the Homebrew package. To include them:

```sh
go build -tags personal -o udeck ./cmd/udeck
```

The demo project lives in [personal-project-demo](personal-project-demo).

The same build adds `udeck probe`, a research tool that sends test
uploads to the device:

```text
udeck probe [--key N] [--sizes 3,8,14] [--gaps 2000,...,0] [--reps 3] [--read]
udeck probe --playpause N [--key N]
udeck probe --recovery
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

### `widget.type: site`

Visitors on a site right now and its page views over the last 24 hours,
with the change against a typical day. Tap refreshes at once.

```yaml
widget:
  type: site
  url: "https://example.com"
  token: "secret"      # optional bearer token
  title: "example"     # optional, defaults to the site name from url
```

The widget requests `<url>/api/metrics` and expects:

```json
{
  "online": {"visitors": 7, "views": 12},
  "daily": {"views": 1520, "visitors": 410, "change": 12.5}
}
```

`change` is a percentage, or `null` when there is nothing to compare
with.

Fields:

- `url`
  - required, `http://` or `https://`
- `token`
  - optional, sent as `Authorization: Bearer <token>`
- `title`
  - optional
- `poll_ms`
  - optional, default `60000`, minimum `30000`

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

- polls freqtrade REST APIs directly from `udeck`
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

Current demo behavior:

- `start`
  - creates a tmux session with a two-pane layout if missing
  - opens `PROJECT_EDITOR` on `PROJECT_EDITOR_TARGET` when configured
- `stop`
  - kills the tmux session
- `focus`
  - opens a new tab in `Ghostty`, `iTerm` or `Terminal`
  - runs `tmux attach-session -t <session>`

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

### Notes

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
- The demo `start` script is idempotent: if the tmux session already
  exists it does nothing. Otherwise it creates a two-pane `main` window;
  replace its commands with your own.
- Project scripts are invoked through `/bin/sh`, so they do not need the
  executable bit as long as the script contents are valid shell.
- Freqtrade widget polls configured REST APIs directly; no separate
  freqtrade daemon or local bridge is required.
