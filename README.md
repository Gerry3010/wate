# yate — yet another terminal emulator

A small, pretty terminal for Linux and macOS. Go backend, system WebView, no Electron.

Why: Warp is nice to look at but drowning in AI features. yate keeps the good parts —
blurred wallpaper, themes, sane split/tab keybinds, clickable links, a built-in
Markdown/code editor — and adds a *thin* Claude Code integration (launcher, status badge,
notifications, session sidebar) without any AI of its own.

> Status: early. Milestone 1 (bootstrap: PTY ↔ xterm.js over a loopback WebSocket) works.

## Features (planned → done)

- [x] Terminal panes via [xterm.js](https://xtermjs.org) (WebGL renderer) + `creack/pty`
- [x] Tabs and split panes with keyboard navigation, snapping dividers
- [x] Themes (TOML), blurred wallpaper / translucent window
- [ ] Ctrl-click: URLs → browser, text files → built-in editor, everything else → default app
- [ ] Editor pane (CodeMirror 6) with Code / Split / Preview modes for Markdown
- [ ] Claude Code: launcher, tab badge, "waiting for input" notifications, session sidebar

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
| Editor: cycle Code / Split / Preview | `Ctrl+Shift+P` |
| Editor: save | `Ctrl+S` |
| Search in terminal | `Ctrl+Shift+F` |
| Font bigger / smaller / reset | `Ctrl++` / `Ctrl+-` / `Ctrl+0` |

Everything is configurable in `~/.config/yate/config.toml` (created with the defaults on
first start). `[keys.darwin]` overrides bindings on macOS only. Terminal and editor appearance
are separate sections (`[terminal]`, `[editor]`): font, size, ligatures, line height, …
Default font is JetBrains Mono at 13px with ligatures. Powerline/Nerd-Font prompts (p10k,
starship) need a patched font: the default stack tries `JetBrainsMono Nerd Font` first —
Arch: `ttf-jetbrains-mono-nerd`, macOS: `brew install font-jetbrains-mono-nerd-font`.

## Building

Requirements: Go 1.24+, Node 20+, and the platform toolchain:

- **Linux:** GTK 4 and WebKitGTK 6.0 dev packages — Arch: `gtk4 webkitgtk-6.0`,
  Debian/Ubuntu: `libgtk-4-dev libwebkitgtk-6.0-dev`.
- **macOS:** Xcode command line tools.

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.18
make build      # → bin/yate
make dev        # hot-reloading dev build
make test       # go test + vitest
```

## Configuration

See [`internal/config/defaults.toml`](internal/config/defaults.toml) for every key with its
default. `yate --config <file>` uses a different file.

### Translucent window on GNOME

`mode = "translucent"` relies on the compositor for blur. On GNOME install
[Blur my Shell](https://extensions.gnome.org/extension/3193/blur-my-shell/) and add
`io.github.gerry3010.yate` (or the `yate` window class) to its *Applications* whitelist.
`mode = "wallpaper"` blurs an image of your choice inside the window and works everywhere.

## License

MIT — see [LICENSE](LICENSE).
