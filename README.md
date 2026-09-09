<img src="build/appicon.svg" width="72" align="left" alt="wate icon">

# wate — Wails Terminal Emulator

[![CI](https://github.com/Gerry3010/wate/actions/workflows/ci.yml/badge.svg)](https://github.com/Gerry3010/wate/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/Gerry3010/wate?logo=go&logoColor=white)](go.mod)
[![Wails v3](https://img.shields.io/badge/Wails-v3-df0000?logo=wails&logoColor=white)](https://v3.wails.io)
[![Platforms](https://img.shields.io/badge/platforms-Linux%20%7C%20macOS-8b5cf6)](#building)
[![License: MIT](https://img.shields.io/badge/license-MIT-89b4fa.svg)](LICENSE)
[![Latest commit](https://img.shields.io/github/last-commit/Gerry3010/wate?color=a6e3a1)](https://github.com/Gerry3010/wate/commits/main)
[![Ko-fi](https://img.shields.io/badge/Ko--fi-support%20wate-ff5e5b?logo=ko-fi&logoColor=white)](https://ko-fi.com/gerre01)

A small, pretty terminal for Linux and macOS. Go backend, system WebView, no Electron.
(*wate* as in **W**ails **T**erminal **E**mulator — or *Why Another Terminal Emulator*. Pronounced like
"wait", which it never makes you do.)

Why: Warp is nice to look at but drowning in AI features. wate keeps the good parts —
blurred wallpaper, themes, sane split/tab keybinds, clickable links, a built-in
Markdown/code editor — and adds a *thin* Claude Code integration (launcher, status badge,
notifications, session sidebar) without any AI of its own.

![wate: terminal, Markdown preview, Claude Code pane and session sidebar](docs/screenshot.png)

> Status: usable daily driver on Linux (all planned features below work); macOS build untested so far.

## Features (planned → done)

- [x] Terminal panes via [xterm.js](https://xtermjs.org) (WebGL renderer) + `creack/pty`
- [x] Tabs and split panes with keyboard navigation, snapping dividers
- [x] Themes (TOML), blurred wallpaper / translucent window
- [x] Ctrl-click: URLs → browser, text files → built-in editor, everything else → default app
- [x] Editor pane (CodeMirror 6) with Code / Split / Preview modes for Markdown
- [x] `wate open <file>[:line]` from any wate shell opens the editor next to it
- [x] Session restore: tabs, splits, directories and open files come back on the next start
- [x] Settings pane (`Ctrl+,`) with theme preview cards and a keybinding recorder
- [x] Claude Code: launcher, tab badge, "waiting for input" notifications, session sidebar

## Keybindings (defaults)

| Action | Key |
|---|---|
| Split → new pane to the right | `Ctrl+Space` |
| Split → new pane below | `Ctrl+Shift+Space` |
| Focus pane left / right / up / down | `Shift+←` / `Shift+→` / `Shift+↑` / `Shift+↓` |
| Resize: move the nearest divider left / right / up / down | `Alt+Shift+←` / `→` / `↑` / `↓` |
| Close pane (last pane closes the tab) | `Ctrl+W` |
| New tab / next / previous / tab *n* | `Ctrl+Shift+T` / `Ctrl+Tab` / `Ctrl+Shift+Tab` / `Alt+n` |
| Copy / paste | `Ctrl+Shift+C` / `Ctrl+Shift+V` (macOS also `Cmd+C` / `Cmd+V`) |
| Launch Claude Code in the current directory | `Ctrl+Shift+K` |
| Toggle agent sidebar | `Ctrl+Shift+A` |
| Settings pane | `Ctrl+,` |
| Editor: cycle Code / Split / Preview | `Ctrl+Shift+P` |
| Editor: save | `Ctrl+S` |
| Search in terminal | `Ctrl+Shift+F` |
| Font bigger / smaller / reset | `Ctrl++` / `Ctrl+-` / `Ctrl+0` |

Everything is configurable in `~/.config/wate/config.toml` (created with the defaults on
first start). `[keys.darwin]` overrides bindings on macOS only. Terminal and editor appearance
are separate sections (`[terminal]`, `[editor]`): font, size, ligatures, line height, …
Default font is JetBrains Mono at 13px with ligatures. Powerline/Nerd-Font prompts (p10k,
starship) need a patched font: the default stack tries `JetBrainsMono Nerd Font` first —
Arch: `ttf-jetbrains-mono-nerd`, macOS: `brew install font-jetbrains-mono-nerd-font`.

## Installing

Grab a build from the [Releases](https://github.com/Gerry3010/wate/releases) page:

- **Linux x86_64:** `.AppImage` (portable), `.deb`, `.rpm`, or a plain `.tar.gz` with the binary.
- **macOS (Apple Silicon + Intel):** `.dmg` / `.app.zip`. The bundle is ad-hoc signed, so on the
  first start right-click → *Open* (or `xattr -d com.apple.quarantine wate.app`).

Every tag `vX.Y.Z` is built by the [release workflow](.github/workflows/release.yml); the notes come
from [CHANGELOG.md](CHANGELOG.md) and each asset has a SHA-256 in `SHA256SUMS.txt`.

## Building

Requirements: Go 1.24+, Node 20+, and the platform toolchain:

- **Linux:** GTK 4 and WebKitGTK 6.0 dev packages — Arch: `gtk4 webkitgtk-6.0`,
  Debian/Ubuntu: `libgtk-4-dev libwebkitgtk-6.0-dev`.
- **macOS:** Xcode command line tools.

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.18
make build      # → bin/wate
make dev        # hot-reloading dev build
make test       # go test + vitest
```

## Claude Code

No AI inside wate — just a good seat for Claude Code:

- `Ctrl+Shift+K` starts `claude` in a new pane in the current directory (`[claude] command`).
- Any pane running Claude gets a status dot in its tab: blue = working, yellow = waiting for
  you, green = finished. The pane frame lights up while Claude waits.
- `Ctrl+Shift+A` opens the session sidebar (directory, state, last message); click a row to jump.
- A desktop notification fires when Claude needs you and you are looking elsewhere.

Process detection works out of the box. For precise states run **`wate install-hooks`** once:
it registers `wate hook <event>` for `SessionStart`, `UserPromptSubmit`, `Notification`, `Stop`
and `SessionEnd` in `~/.claude/settings.json` (existing hooks are kept, a backup is written).
Outside wate the hooks are silent no-ops.

### Desktop integration (Linux)

`make install` puts `wate` in `~/.local/bin` with a desktop entry that declares
`MimeType=inode/directory`, so folders offer *Open With → wate*. `make install-nautilus` adds a
first-level **Open in wate** entry to Nautilus' context menu (folder and background; needs the
`nautilus-python` package). `wate <dir>` opens the directory as a new tab in the running wate
(or starts one) — handy for launcher keybindings like Super+T.

## Shell integration

wate loads a small zsh snippet automatically (via `ZDOTDIR`, no rc-file edits) that emits the
working directory (OSC 7) and prompt marks (OSC 133). That is what makes new panes open in the
current directory and — like kitty — lets wate resize without the duplicated-prompt artifact
xterm.js-based terminals otherwise show with powerlevel10k. Disable with
`shell_integration = false`. bash and fish: `source ~/.config/wate/shell/wate.bash` /
`source ~/.config/wate/shell/wate.fish` in your rc file.

## CLI

Every shell started by wate has `$WATE_SOCKET`, `$WATE_PANE_ID` and `$WATE_TAB_ID` set, so the
`wate` binary can talk to the running instance:

```sh
wate open README.md:12      # open in an editor pane next to this terminal
wate ctl action split_down  # run any keybinding action by name
wate ctl input 'ls\n'       # type into the current pane
```

## Configuration

`Ctrl+,` opens the settings pane — vertical tabs for Themes, Background, Terminal, Editor,
Shell & Startup, Claude Code and Keybindings (click a binding, press the new keys). Every option
from `config.toml` is there. Saving edits the file in place: comments, order and your own keys
survive, and changes apply live. Editing the file by hand works just as well.

### Themes

Five themes are built in (Catppuccin Mocha, Dracula, Nord, Gruvbox Dark, Tokyo Night). wate
also **imports Ghostty and Alacritty colour schemes**, which means every scheme in
[iTerm2-Color-Schemes](https://github.com/mbadolato/iTerm2-Color-Schemes) (400+) works:
open a file in its `ghostty/` or `alacritty/` folder, copy the text and use *Settings → Themes →
Paste theme…* (give it a name), or import the downloaded file (*Import file…* or
`wate theme import <file>`). Your themes appear in their own group above the built-in ones, can be
deleted from their card, and live in `~/.config/wate/themes/<id>.toml` where you can tweak them;
the "Browse compatible themes" button links to the catalog. See [`internal/config/defaults.toml`](internal/config/defaults.toml) for every
key with its default. `wate --config <file>` uses a different file.

### Import from another terminal

*Settings → Import* lists the terminals it finds on the machine — **Warp, Ghostty, Alacritty
and kitty** — with what each one can contribute: colour theme, font (family, size, ligatures),
window opacity, background image, key bindings, and for Warp the **saved tabs with their split
layouts, working directories, titles and colours** — optionally with Warp's **command history**:
the recent blocks (command + output) of every pane are replayed as scrollback, so the new tab
starts where the old one left off. Tick what you want and import; everything else stays untouched. Warp tabs are read from its SQLite database with the `sqlite3` command-line tool
(preinstalled on macOS, a small package on Linux).

### Sessions, tab titles and colours

The `⌄` button at the right of the tab bar (or a right-click on empty tab-bar space) saves the
current tabs — layouts, directories, open files, titles, colours and the terminal scrollback — under
a name and reopens saved sessions later (the scrollback comes back as dimmed history above a fresh
prompt). Sessions are plain JSON in `~/.config/wate/sessions/`. Double-click a tab to
rename it; right-click for colours.

### Translucent window on GNOME

`mode = "translucent"` relies on the compositor for blur. On GNOME install
[Blur my Shell](https://extensions.gnome.org/extension/3193/blur-my-shell/) and add
`org.wails.wate` (or the `wate` window class) to its *Applications* whitelist.
`mode = "wallpaper"` blurs an image of your choice inside the window and works everywhere.

## Support

If wate makes your terminal life nicer, you can [buy Gerry a coffee on Ko-fi](https://ko-fi.com/gerre01) ☕

## License

MIT — see [LICENSE](LICENSE).
