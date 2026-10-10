# Changelog

All notable changes to wate are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [SemVer](https://semver.org).
Each `## [x.y.z]` section becomes the body of the matching GitHub release.

## [Unreleased]

### Added
- Once every agent has said go, the restart happens by itself after ten seconds
  (`[claude] restart_countdown`, 0 to wait for the button instead). The instruction was given
  when the round was started; there is nothing left to decide, and the button only existed to
  cover the agents who had not answered. The panel counts down and Cancel still stops it.
  Each session shows where it stands: an empty circle for no answer, a filled grey one for
  "heard you, but not yet", green for ready — the countdown starts when they are all green.
- `wate_pane_run` runs a command in a pane beside the agent and waits for it to finish. Its
  reason for existing is the command that needs *you*: sudo, an ssh passphrase, a host key, a
  second factor. The command runs where you can see it, and the moment it asks for something
  only you can type, the tool returns, brings that pane to the front and tells the agent to
  wait — so you type the password and nothing else. It watches the pane's foreground process
  to know when the command is done, and only treats the last line as a prompt, so a command
  that merely mentions a password does not stop everything.
- Closing a window with agents still working in it asks first: "N agents are still running",
  with Quit, Wait and Cancel. Wait hands over to the restart round below — the same question,
  put to the agents instead of to you. The close is genuinely called off rather than undone
  afterwards, so nothing has ended by the time you answer.
- wate can ask the agents before it restarts. "Prepare restart…" in the Sessions menu (or
  `wate ctl restart`) puts the question to every running Claude Code session; each can answer
  `go`, or `wait` with a rough number of minutes and a line about what it is in the middle of.
  The answers appear in the Claude panel as they come in, with two buttons. The deadline only
  changes what the panel says — nothing is ever ended without you pressing the button, which is
  why an estimate is worth asking for in the first place.
  The question reaches a session through wate's own hooks, which is the only way into a running
  agent that does not type into its terminal, and each session is told once and reminded once as
  the deadline comes up. `PostToolUse` joins the installed hooks for this: it is the one event
  that fires while an agent is busy, and a busy agent is exactly the one worth asking.
  Restarting means quitting and starting again, with the session restored — so the new build is
  the one that comes back. `[claude] restart_deadline` sets how long the agents get.
- A pane can be moved to another tab, or out into one of its own, without restarting anything:
  its shell keeps running, its scrollback comes along, and a command mid-flight does not notice.
  Nothing is rebuilt — a pane is an object with a socket and a terminal in it, and only its
  parent changes. Reach it from the pane's menu, by dragging the pane by its bar — onto the
  middle of another pane to swap the two, onto an edge to land there, or onto another tab's
  button, which outlines itself and says "Move to this tab"; rest on that button for a moment
  and the tab comes forward, so the pane can be aimed at a spot inside it — or with
  `wate ctl pane-move new|<n>`. An agent may only move a pane it opened itself; ownership
  survives the move, but reach does not while the pane sits in another tab, because the tab is
  the boundary.
- A pane sharing its tab with another gets a thin bar along its top: what it is running, an
  orange mark if an agent opened it, letters for anything the user has opened it up to, and a
  menu. A pane on its own keeps the uncluttered look — it is the tab, so there is nothing to
  tell apart. The bar's menu is where an agent is let into a pane that is not its own: Read,
  Write, Manage, each a tick, with "Revoke all" once something is on. Ticking Manage ticks the
  other two as well — managing a pane you may neither see nor type into is not a halfway house
  anybody would pick — and unticking it leaves them, because losing the right to close a pane
  is no reason to stop reading it. Nothing else implies anything, least of all Read: whatever
  is on that screen is whatever you typed. The letters are there so that a pane which has been
  opened up says so without anyone opening the menu.
  The grid moved into a box of its own underneath the bar, because FitAddon measures the
  terminal's parent and would otherwise have counted the bar's height as usable rows and let
  the pane clip the last one.
- Claude Code sessions wate starts can drive their own tab through an MCP server built into the
  binary (`wate mcp`, stdio). Seven tools: open a pane beside or below, give it a third, a half
  or two thirds of the space, type into it, read it back, close it, focus it. wate hands the
  server to the sessions it launches and to nobody else — your `~/.claude/settings.json` is not
  touched, your own MCP servers stay switched on alongside it, and a Claude started by hand in
  a pane does not get the tools. Turn it off with `[claude] mcp = false`.
- Panes can be opened, resized, read, written, closed and focused from the control socket:
  `wate ctl pane-split [row|col] [third|half|two-thirds|<fraction>]`, `split-ratio`,
  `pane-write`, `pane-read`, `pane-close`, `pane-focus` and `pane-list`. `--pane` says who is
  asking and `--target` says which pane to act on, because the two being the same thing is what
  makes every permission check pass.
- A pane may always act on itself and on panes it opened; reaching any other pane needs the
  user to allow it, and never reaches outside the tab. What Write alone buys is an offer: the
  text lands on your prompt without the Enter, so an agent can suggest a command but not run it
  in your shell. Tick Read as well and the pane is handed over, Enter included — at that point
  the agent can already type a command and watch what comes of it, so holding back the Return
  key would only leave it waiting, not make it safer. The menu says which of the two is in
  force. None of this is written to disk: the session a grant was made for does not survive a
  restart, and its successor inheriting the rights would be an escalation nobody asked for.
- `wate ctl pane-read [<lines>]` prints what is on a pane's screen as plain text. The terminal's
  text lives in the web view, not in the backend — the bytes from the shell have long since been
  parsed into a grid — so this is the first thing wate asks its own window and waits for an
  answer to. It reads the live screen, a full-screen program's included, counting back from the
  cursor rather than from the bottom of the grid, where a terminal keeps its blank padding.
- Dragging a tab down off the bar tears it into a window of its own, the way a browser does.
  The shell keeps running and the history comes with it. To put a tab into an *existing*
  window, use "Move to window N" in its right-click menu: pointer capture reports coordinates
  past the window's edge only while wate has a single window, so the gesture cannot tell which
  other window it was let go over.
- A tab can be moved to another window, or out into a window of its own, from its right-click
  menu or with `[keys] move_tab_to_new_window`. Its shells keep running: the receiving window
  re-attaches to the very same sessions rather than starting new ones, so a build, an ssh
  connection or a Claude Code session survives the move along with the visible history.
- wate can have more than one window. `[keys] new_window` (Ctrl+Shift+N) opens one where the
  focused pane is looking, `wate --new-window [<dir>]` and `wate ctl new-window [<dir>]` do it
  from a shell, and each window restores its own tabs on the next start. A window you close
  stays closed; quitting keeps them all. `wate --standalone` starts a separate instance instead
  of handing over to the running one.
- The Claude sidebar groups sessions into Favourites and the rest, both foldable and both
  remembering whether they were folded. A favourite stays listed after its session ends, with
  `Resume →` to pick it up again (`claude --resume` in the directory it ran in).
- Each session row has a menu: favourite it, give it a name of your own (which is also sent to
  Claude as `/rename`, so both agree), open `/remote-control`, end it with two Ctrl-Cs — the
  pane stays, you land back at the shell — or delete it, which asks first and then removes its
  transcript. Favourites and names are keyed on the session id, so they need wate's hooks
  installed; without them the menu says so rather than pretending.
- Tabs can be reordered: drag one along the bar and a caret shows where it will land, or bind
  `[keys] move_tab_left` / `move_tab_right` to shift the active tab without the mouse. The order
  is part of the restored session, so it survives a restart.
- `[terminal] option_as_meta` decides what Option+key does on macOS. It is off by default, so
  Option types the character the layout puts there — on a German Mac that is the at sign,
  the euro sign, the pipe, the backslash and the braces, none of which reached the shell
  before. Turn it on for Alt+b/Alt+f style Meta sequences; the word-wise keys are unaffected
  either way, since they go through the arrows and Backspace.
- `[terminal] word_keys` picks the modifier for word-wise jumping and deleting at the prompt —
  `"ctrl"` (default), `"alt"`, `"both"` or `"off"`. wate's shell integration binds the sequences
  in zsh, bash and fish, so `Ctrl+Backspace` deletes a word instead of a character.
- Programs can put text on the clipboard with OSC 52 (`[terminal] osc52`, on by default): copying
  inside Claude Code, tmux or vim over ssh now actually reaches the system clipboard. Reading the
  clipboard is never answered.

### Changed
- The Claude sidebar spans windows: a session running in another window is listed with
  "other window", and clicking it raises that window and focuses the pane instead of doing
  nothing. Notification suppression is now per window too — with two open, one window losing
  focus used to silence the one that still had it.
- The Claude badge's popup shows the session id in full and copies it on click, instead of
  cutting it to eight characters that were no use to anyone. The popup suppresses text
  selection, so a button is the only way to get the id out of it.
- Word-wise deleting works inside Claude Code too: it reads the `0x08` a terminal sends for
  `Ctrl+Backspace` as a plain backspace on Linux and macOS, so wate tells it otherwise
  (`CLAUDE_CODE_BS_AS_CTRL_BACKSPACE`) whenever `word_keys` includes Ctrl.
- The zsh integration re-applies the word-key bindings before every prompt (a plugin that
  rebinds later would otherwise win) and also accepts `ESC ^H`, which is what a remapped
  Ctrl/Alt key can produce.
- `wate ctl debug` reports where a frame's time goes (ligature joiner, tab-bar rebuilds, title
  changes, saves, bytes in, and the latency from a PTY chunk arriving to the screen showing it);
  `wate ctl action __perf` turns the timing on.
- Resolved file links are cached per directory until the next command runs, so hovering a busy
  pane no longer asks the backend to stat the same paths over and over.
- Ligatures no longer go through `@xterm/addon-ligatures`, which could never load a font in a
  WebView and fell back to scanning every character against 62 strings on every rendered frame.
  wate now joins them itself — same ligatures, ~27× less work per frame.
- Less work while a TUI is busy: the tab bar only repaints when something visible changed, a
  shell title that does not change the tab title no longer triggers anything, the session
  autosave serializes a capped number of lines and skips a per-pane backend call, the hidden
  sidebar does not render, and the Claude poller remembers the process instead of walking the
  pane's process tree every two seconds.

- A file dragged into wate lands where it is dropped: in the middle of a pane its (shell-quoted)
  path goes to the prompt, near an edge the pane splits towards that edge and the file opens
  there — a text file in the editor, a directory as a shell that starts in it, anything else as a
  shell with the path typed in. A highlight shows which of the two a drop would do. The paths come
  from the OS rather than from the WebView, because WebKitGTK reports a file drop with an empty
  DataTransfer: it advertises `text/uri-list` and then hands out nothing.

### Fixed
- A pane's offer to resume its Claude session survives more than one restart. The offer lived
  only in the state file, and the save that followed a restart read the *live* sessions, found
  none for a pane that had just been restored, and wrote it out empty — so the pointer made it
  through exactly one restart and was gone by the second, which is when you want it most. A
  restored pane now keeps its pointer until a real session takes the pane over.
- `wate_pane_run` opens a new work pane when the one it remembers has gone. The agent outlives
  its panes: you can close one, and a restart takes them all, while the MCP server carries on
  holding the id. It never checked, so it never opened a replacement either — every later run
  without a named pane failed on a pane that had not existed for hours.
- A pane that has been moved to another tab still answers for itself. Its callbacks held on to
  the tab it was created in, so after a move the drag did nothing, a shell exiting left the
  pane on screen as a dead terminal, and a file opened from it landed in the wrong tab. They
  look up the tab that holds the pane now.
- "Resume" on a restored pane brings the session back with the same flags wate would have
  started it with. It assembled the command line by hand, which meant a session picked up
  after a restart came back without the pane tools — exactly the session most likely to be
  asked about the next restart.
- A control-socket action now lands in the tab it was sent from. `wate ctl action split_right`
  from a pane in a background tab used to split whichever tab happened to be in front, because
  the pane the request named was thrown away on the way to the frontend. The new split also
  starts in the directory of the pane that asked for it, not of whatever was last focused over
  there.
- wate asks GTK for its OpenGL renderer on Linux rather than taking the default. GTK 4.22
  picks Vulkan, and two of the three paths that import WebKit's frames into GDK lose a little
  memory per frame and never give it back. Under the same load, measured on a window drawing
  continuously: Vulkan grew the main process by 76 kB/s and Cairo by 113 kB/s, OpenGL by
  nothing at all — and OpenGL did it on 6% of a CPU core where the others needed 27%. Left
  running, that was gigabytes of swap over a few days. Setting `GSK_RENDERER` yourself still
  wins, and the variable is kept out of the shells, so programs you start in a pane are
  unaffected.
- The status dot blinks in steps instead of fading smoothly, which stops wate from redrawing
  itself sixty times a second for as long as any Claude session is running. The smooth pulse
  kept the compositor busy whether or not anything else changed: measured against an idle
  window, it cost a full CPU core, and on the Wayland/dma-buf path the main process leaked
  about half a kilobyte per frame — two megabytes a minute, eleven gigabytes over twelve days,
  which is what pushed this machine into swap. The leak itself is in the platform's frame
  path, not in wate; this takes away the thing that kept feeding it.
- Dragging feels lighter: the drop highlight is moved with a transform (the browser composites
  that, where animating its position and size re-painted the whole translucent rectangle every
  frame), and both it and a divider drag do their work once per frame instead of once per
  reported mouse position — a mouse reports several of those per frame, and each one was laying
  out every pane again.
- The bottom row of a pane is no longer cut in half. The gap around the grid was padding on the
  pane, while `FitAddon` divides the pane's `getComputedStyle().height` by the cell height —
  and WebKitGTK resolves that to the padding box (Blink resolves it to the content box), so the
  padding counted as usable space and the grid got a row too many. The padding now sits on the
  terminal element, which is the one the addon subtracts. `wate ctl debug` reports the grid's
  geometry (cell height, grid height, and how far it overflows its pane) so this stays visible.
- Dragging a file into a pane no longer takes the app down: the WebView used to navigate to
  `file:///…`, so the image covered every pane with no way back and wate had to be killed.
- On macOS the first tab no longer sits underneath the window's traffic lights. The window uses
  `MacTitleBarHiddenInset`, so the system draws them over wate's own content, while the tab bar
  started at 8px the way it does on Linux, where there is nothing to dodge. It now keeps their
  80px clear, and hands the room back in fullscreen, where the buttons are gone — the backend
  reports that transition as `window:fullscreen`, which the webview cannot observe by itself.
- The Dock shows wate's own icon instead of a white Wails "W". `build/appicon.icon` still held
  the template's logo, and because `Info.plist` names `CFBundleIconName`, macOS preferred the
  `Assets.car` compiled from it over the `.icns` generated from `build/appicon.png`. The icon
  now carries wate's prompt chevron, cursor and split panes; `make icons` recompiles the
  catalogue from those layers.
- The translucent background on macOS follows the theme instead of the system: AppKit draws the
  frosted glass from the window's `NSAppearance`, so a dark wate theme on a light Mac came out
  milky white however low the opacity went. `setPreferDark` now points AppKit at the
  appearance the theme asks for, the same hook the GTK title bar already used on Linux.
- `[background] opacity = 0` is a fully see-through pane again instead of a fully opaque one.
  The config read a zero as "not set" and replaced it with 1, which it never had to: the file
  is decoded on top of the defaults, so an absent key already keeps 0.85. Values outside 0..1
  are clamped now, and the settings slider goes down to 0.
- The note that translucent mode needs a restart is readable: it was inside the mode dropdown,
  where the select cut it off ("Translucent (OS"), and now sits in the hint below the field.

## [0.2.2] — 2026-09-10

### Changed
- The tab's number (and its Claude status dot) moved to the end of the tab, where the close
  button takes its place on hover.

### Fixed
- Restoring a session no longer stacks the history: a pane saved everything on its screen,
  including the scrollback that had just been replayed into it, so every restart added another
  copy with its own `── restored ──` line. Only what a session wrote itself is saved now.

## [0.2.1] — 2026-09-10

### Added
- `close_tab` (`Ctrl+Shift+W`, `Cmd+Shift+W` on macOS) closes the whole tab with all its panes;
  kitty's and Ghostty's `close_tab` binding is picked up on import.

### Changed
- Tabs are named like the shell prompt — the working directory with `~` for home and shortened
  parents (`~/Sy/GO-Projekte/wate`) — instead of the shell's `user@host:path` title. While an ssh
  session runs in the focused pane the tab reads `ssh <host>`; the old title and the full path
  moved into the tab's tooltip.
- The tab bar no longer shifts when the mouse passes over it: the close button now appears in the
  slot of the tab's number instead of next to the title. Tabs also breathe a little more on the right.

## [0.2.0] — 2026-09-10

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

### Changed
- Pane dividers draw their hairline only between two inactive panes: the stretch beside the
  focused pane is left out (also along sub-splits, where the line stops at the focused pane).
  The divider still lights up on hover so it can be dragged. The Claude sidebar's left edge
  follows the same rule.

### Fixed
- Reopening wate right after closing it came up empty (and only the next start restored the
  session): the closing process still counted as the running instance while its shells wound
  down, so the new window started as a secondary one. The primary record is now released the
  moment shutdown begins, and secondary windows no longer claim it.
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

[Unreleased]: https://github.com/Gerry3010/wate/compare/v0.2.2...HEAD
[0.2.2]: https://github.com/Gerry3010/wate/releases/tag/v0.2.2
[0.2.1]: https://github.com/Gerry3010/wate/releases/tag/v0.2.1
[0.2.0]: https://github.com/Gerry3010/wate/releases/tag/v0.2.0
[0.1.0]: https://github.com/Gerry3010/wate/releases/tag/v0.1.0
