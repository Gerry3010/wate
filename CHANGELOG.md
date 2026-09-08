# Changelog

All notable changes to yate are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [SemVer](https://semver.org).
Each `## [x.y.z]` section becomes the body of the matching GitHub release.

## [Unreleased]

## [0.1.0] — 2026-09-09

First release: a small, pretty terminal for Linux and macOS (Go + Wails v3 + xterm.js).

### Added
- Terminal panes (xterm.js, WebGL renderer, ligatures) fed by a loopback WebSocket PTY bridge.
- Tabs and split panes with keyboard navigation (`Ctrl+Space`, `Ctrl+Shift+Space`, `Shift+Arrows`),
  keyboard resize (`Alt+Shift+Arrows`) and snapping dividers.
- Themes: five built-in, user themes in `~/.config/yate/themes`, import of Ghostty and Alacritty
  schemes (every scheme in iTerm2-Color-Schemes), live reload.
- Background modes: solid, translucent (OS/compositor blur), blurred wallpaper.
- Ctrl-click: URLs open in the browser, text files in the built-in editor, everything else in the
  default application.
- Editor pane (CodeMirror 6) with Code / Split / Preview modes for Markdown, save with on-disk
  change detection, unsaved-changes dialog, `file:line:col` jumps.
- Claude Code integration: launcher (`Ctrl+Shift+K`), tab status dots, pane highlight while Claude
  waits, desktop notifications, session sidebar (`Ctrl+Shift+A`), `yate install-hooks`.
- Shell integration for zsh (automatic), bash and fish: OSC 7 cwd and OSC 133 prompt marks —
  new panes inherit the directory and resizing leaves no duplicated prompts.
- Settings pane (`Ctrl+,`) with vertical tabs for every option, theme preview cards and a
  keybinding recorder; writes preserve comments in `config.toml`.
- Session restore (tabs, splits, directories, open files).
- CLI: `yate <dir>` (new tab in the running instance), `yate open <file>`, `yate ctl …`,
  `yate theme import`, `yate hook`, `yate install-hooks`.
- Desktop integration: `make install` (desktop entry, *Open With* for folders),
  `make install-nautilus` ("Open in yate" in the Nautilus context menu).

[Unreleased]: https://github.com/Gerry3010/yate/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Gerry3010/yate/releases/tag/v0.1.0
