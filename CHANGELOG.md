# Changelog

All notable changes to wate are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [SemVer](https://semver.org).
Each `## [x.y.z]` section becomes the body of the matching GitHub release.

## [Unreleased]

### Added
- Claude badge: a small rounded tab tucked into the pane's bottom-right corner (drawn like part of the
  pane border, so it covers no text) appears when Claude Code runs there; click it for the
  session popup — title, state, directory, model, last prompt, the context bar with its split
  (cached / cache write / fresh input / output) and an "All sessions →" link to the sidebar.
  The sidebar got a close button.
- Claude Code sessions end gracefully when wate quits: the agents get a SIGTERM (and a moment to
  flush their transcript and run their `SessionEnd` hooks) before the shells are closed. A
  SIGINT/SIGTERM on wate itself now takes the same route instead of pulling the plug.
- A restored pane that held a Claude Code session shows an inset between its history and the new
  prompt, set in the terminal's font: Claude's mark in an orange gutter, the session title, its
  directory / model / context usage, and a `▶ Resume` chip that runs `claude --resume <id>`
  right there. The title comes from the session transcript (its summary,
  otherwise the first prompt) and is shown in the Claude sidebar too.
- Swap panes: `Ctrl+Shift+Arrow` exchanges the focused pane with its neighbour, Alt+drag a pane
  onto another does the same with the mouse.
- `[window] width/height/remember_size`: default window size, and the last size/position/maximised
  state is restored. A second wate started while one runs opens a default-size empty window that
  leaves the saved session alone.
- Themes page: paste a Ghostty/Alacritty/wate theme as text and save it under a name; your themes
  are listed in their own group (with delete), the grid scrolls while the buttons stay put; the
  catalog link sits on its own line.
- *Settings → Import*: pull theme, font, window opacity, background image, key bindings and (for
  Warp) saved tabs with split layouts, cwds, titles and colours from Warp, Ghostty, Alacritty or
  kitty — with a checkbox per item. Warp's command history (recent blocks with output) can be
  replayed into the imported panes.
- Named sessions: the `⌄` dropdown (or right-click) on the tab bar saves the current tabs under a
  name and reopens them later (`~/.config/wate/sessions/*.json`). Sessions and the automatic
  restore keep each pane's scrollback and replay it above the new prompt.
- Tab titles (double-click to rename) and tab colours (right-click), kept in sessions.
- Settings button (gear) in the tab bar; settings open in their own tab when the current one is split.
- Claude sidebar shows each session's context usage (tokens used / window, percent, colour-coded)
  read from the Claude Code transcript, so you can tell when a `/compact` is due.
  `[claude] context_window` overrides the guessed window size.

### Fixed
- Restoring a session in which a TUI (Claude Code) had been running left the shell echoing
  mouse and focus reports as garbage: the replayed text no longer carries terminal modes and
  every mode is reset before the new shell starts.
- Resizing a pane while a command was running wiped the previous prompt (it is only cleared
  for a redraw while the shell is actually at its prompt).
- Settings: the Themes page stayed visible above whichever page was selected.
- Opening many tabs at once (Warp import, session restore) stopped after a few: a burst of
  concurrent PTY spawn calls could lose an answer in the WebView IPC. Spawns now run one after
  another, time out and retry, and a retry replaces the orphaned shell. A failing pane no
  longer aborts the remaining tabs.
- Pane swap left the moved terminal blank until clicked; Alt+drag now previews the swapped
  layout live while dragging (Esc cancels).
- "Already running" detection is per config: the socket path is recorded in the state dir, so a
  wate with another config dir is not mistaken for the primary window.
- Linux: the GTK title bar follows the theme (dark for dark themes) instead of always being light.
- Linux: `background.mode = "translucent"` now actually makes the window see-through on GTK4.
- Background settings: choosing wallpaper mode without an image opens the file dialog; sliders
  that have no effect in the current mode are hidden.
- GNOME shows wate's icon and name in the dock (desktop entry named after the GTK application id).
- Pane outlines no longer get clipped by the window's rounded corners.
- Panes in hidden tabs release their WebGL context (browsers allow only ~16), so visible panes
  never fall back to the DOM renderer whose text could spill into neighbouring panes; panes
  clip their rows as a safety net.

## [0.1.0] — 2026-09-09

First release: a small, pretty terminal for Linux and macOS (Go + Wails v3 + xterm.js).
Briefly published as *yate* — renamed to **wate** (Wails Terminal Emulator, or *Why Another
Terminal Emulator*) because the old name was taken several times over. Existing `~/.config/yate`
and `~/.local/state/yate` directories are migrated automatically; run `wate install-hooks` once
to replace the old Claude Code hooks.

### Added
- Terminal panes (xterm.js, WebGL renderer, ligatures) fed by a loopback WebSocket PTY bridge.
- Tabs and split panes with keyboard navigation (`Ctrl+Space`, `Ctrl+Shift+Space`, `Shift+Arrows`),
  keyboard resize (`Alt+Shift+Arrows`) and snapping dividers.
- Themes: five built-in, user themes in `~/.config/wate/themes`, import of Ghostty and Alacritty
  schemes (every scheme in iTerm2-Color-Schemes), live reload.
- Background modes: solid, translucent (OS/compositor blur), blurred wallpaper.
- Ctrl-click: URLs open in the browser, text files in the built-in editor, everything else in the
  default application.
- Editor pane (CodeMirror 6) with Code / Split / Preview modes for Markdown, save with on-disk
  change detection, unsaved-changes dialog, `file:line:col` jumps.
- Claude Code integration: launcher (`Ctrl+Shift+K`), tab status dots, pane highlight while Claude
  waits, desktop notifications, session sidebar (`Ctrl+Shift+A`), `wate install-hooks`.
- Shell integration for zsh (automatic), bash and fish: OSC 7 cwd and OSC 133 prompt marks —
  new panes inherit the directory and resizing leaves no duplicated prompts.
- Settings pane (`Ctrl+,`) with vertical tabs for every option, theme preview cards and a
  keybinding recorder; writes preserve comments in `config.toml`.
- Session restore (tabs, splits, directories, open files).
- CLI: `wate <dir>` (new tab in the running instance), `wate open <file>`, `wate ctl …`,
  `wate theme import`, `wate hook`, `wate install-hooks`.
- Desktop integration: `make install` (desktop entry, *Open With* for folders),
  `make install-nautilus` ("Open in wate" in the Nautilus context menu).

[Unreleased]: https://github.com/Gerry3010/wate/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Gerry3010/wate/releases/tag/v0.1.0
