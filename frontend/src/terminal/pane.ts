import { Terminal, type IDecoration, type IMarker, type ITerminalOptions } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SerializeAddon, type ISerializeOptions } from "@xterm/addon-serialize";
import { WebglAddon } from "@xterm/addon-webgl";
import "@xterm/xterm/css/xterm.css";

import { WebLinksAddon } from "@xterm/addon-web-links";

import { Clipboard } from "@wailsio/runtime";

import { Events, OpenerService, PtyService, type Target, type TerminalConfig } from "../api";
import type { Pane } from "../pane";
import { FileLinkProvider, clearLinkCache, isModifierClick } from "./links";
import { paneTitle, type PaneCommand } from "./title";
import { parseOsc52 } from "./osc52";
import { perf, sample } from "../perf";
import { logData } from "../keys-debug";
import { ligatureJoiner } from "./ligatures";
import { CLAUDE_LOGO, PLAY } from "../ui/icons";

/** A call-to-action block drawn across the pane between the restored history and the prompt. */
export interface PaneNotice {
  /** Session title, shown in quotes. */
  title: string;
  /** Second line: directory, model, context usage. */
  detail?: string;
  /** Action label ("Resume"). */
  action: string;
  /** Tooltip: the command the action runs. */
  hint?: string;
  onActivate(): void;
}

export interface TerminalPaneOptions {
  paneId: string;
  tabId: string;
  cwd?: string;
  command?: string[];
  /** Terminal text (ANSI) written before the shell starts: restored scrollback / imported history. */
  replay?: string;
  /** false for panes created in a hidden tab: no GPU renderer until the tab is shown. */
  visible?: boolean;
  terminal: TerminalConfig;
  theme?: Record<string, string>;
  fontDelta?: number;
  /** Block shown after the replay (a Claude Code session that ran here before the restart). */
  notice?: PaneNotice;
  /** Return false for keys the app handles itself (so xterm ignores them). */
  keyFilter?: (e: KeyboardEvent) => boolean;
  onExit?: (code: number) => void;
  onTitle?: (title: string) => void;
  /** Ctrl/Cmd-click on an existing file or directory. */
  onOpenFile?: (t: Target) => void;
}

/**
 * Written after a replay: the text may have switched terminal modes on (a TUI's mouse tracking,
 * focus reports, bracketed paste, the alternate screen, application keys). The fresh shell knows
 * nothing about them and would echo the reports as garbage, so everything goes back to defaults.
 */
const MODE_RESET =
  "\x1b[?9l\x1b[?1000l\x1b[?1001l\x1b[?1002l\x1b[?1003l\x1b[?1004l\x1b[?1005l\x1b[?1006l\x1b[?1015l\x1b[?1016l" +
  "\x1b[?2004l\x1b[?1l\x1b>\x1b[?7h\x1b[?25h\x1b[0m\x1b(B\x1b[4l";

/** Rough cost of a serialized line (text plus its colour escapes); see savedRange(). */
const BYTES_PER_LINE = 24;

/** One encoder for every keystroke; allocating one per send shows up under fast typing. */
const ENCODER = new TextEncoder();

/** Terminal rows the notice block occupies. */
const NOTICE_ROWS = 3;


/** Spawns run one after another: a burst of concurrent binding calls can lose an answer in
 *  the WebView IPC (seen on WebKitGTK), and a single queue keeps restores deterministic. */
let spawnChain: Promise<unknown> = Promise.resolve();
function spawnQueued<T>(fn: () => Promise<T>): Promise<T> {
  const run = spawnChain.then(fn, fn);
  spawnChain = run.catch(() => undefined);
  return run;
}

/** Retry a binding call whose answer never arrives (the Go side makes the call idempotent). */
async function withRetry<T>(fn: () => Promise<T>, what: string, timeoutMs = 6000, attempts = 3): Promise<T> {
  let lastErr: unknown;
  for (let i = 1; i <= attempts; i++) {
    try {
      return await Promise.race([
        fn(),
        new Promise<never>((_, reject) => setTimeout(() => reject(new Error(`${what}: no answer after ${timeoutMs} ms`)), timeoutMs)),
      ]);
    } catch (err) {
      lastErr = err;
      console.warn(`${what} attempt ${i}/${attempts} failed:`, err);
    }
  }
  throw lastErr;
}

/** One xterm.js instance wired to a PTY session over the loopback WebSocket. */
export class TerminalPane implements Pane {
  readonly id: string;
  readonly kind = "terminal" as const;
  readonly element: HTMLElement;
  readonly term: Terminal;
  /** The shell's own title (OSC 0/2), kept for tooltips and as a last fallback. */
  oscTitle = "";
  /** What the pane runs right now; the backend polls it (see PtyService.watchForeground). */
  private cmd?: PaneCommand;
  private unsubscribe?: () => void;
  private fit = new FitAddon();
  private serializer = new SerializeAddon();
  private ws?: WebSocket;
  private sessionId?: string;
  private resizeObserver: ResizeObserver;
  private disposed = false;
  private fitTimer?: ReturnType<typeof setTimeout>;
  private fitSuspended = false;
  private active = false;
  /** Working directory reported by the shell via OSC 7 (shell integration). */
  private osc7Cwd?: string;
  /** Start of the current prompt (OSC 133;A), used to resize without artifacts. */
  private promptMarker?: IMarker;
  /** The CTA block hanging on a buffer line (see showNotice). */
  private notice?: IDecoration;
  /** Where the replayed history ends: only what this session added is saved again. */
  private replayEnd?: IMarker;
  /** Id of the ligature joiner while ligatures are on. */
  private joinerId?: number;
  /** When the oldest unrendered PTY chunk arrived (perf timing only). */
  private pendingSince = 0;

  constructor(private opts: TerminalPaneOptions) {
    this.id = opts.paneId;
    this.element = document.createElement("div");
    this.element.className = "pane pane-terminal";
    this.element.dataset.paneId = opts.paneId;

    const t = opts.terminal;
    this.element.style.padding = `${t.padding}px`;
    const termOpts: ITerminalOptions = {
      fontFamily: t.font,
      fontSize: t.font_size + (opts.fontDelta ?? 0),
      lineHeight: t.line_height || 1,
      scrollback: t.scrollback,
      cursorStyle: t.cursor_style as ITerminalOptions["cursorStyle"],
      cursorBlink: false,
      cursorInactiveStyle: "outline",
      allowProposedApi: true,
      allowTransparency: true,
      macOptionIsMeta: true,
      // macOS has no Shift-drag override, so Option+drag is what selects text while a
      // program (Claude Code, vim, htop) has mouse reporting on.
      macOptionClickForcesSelection: true,
      theme: opts.theme,
    };
    this.term = new Terminal(termOpts);
    this.term.loadAddon(this.fit);
    this.term.loadAddon(this.serializer);
    this.term.open(this.element);
    this.wantVisible = opts.visible !== false;
    if (this.wantVisible) this.enableWebgl();
    if (t.ligatures) this.enableLigatures();

    this.term.onRender(() => {
      if (this.pendingSince === 0) return;
      sample(performance.now() - this.pendingSince);
      this.pendingSince = 0;
    });
    this.term.onData((data) => {
      logData(data);
      this.send(data);
    });
    this.term.onBinary((data) => this.send(data, true));
    this.term.onResize(({ cols, rows }) => this.sendResize(cols, rows));
    this.term.onTitleChange((title) => {
      // A TUI rewrites its title constantly; only a changed *pane* title is worth a repaint
      // of the tab bar (paneTitle() prefers the ssh host and the cwd over the shell's title).
      const before = this.title;
      this.oscTitle = title;
      if (this.title !== before) {
        perf.titleFires++;
        opts.onTitle?.(this.title);
      }
    });
    // OSC 7: file://host/path — the shell tells us its cwd (cheaper and more reliable than /proc).
    this.term.parser.registerOscHandler(7, (data) => {
      const m = /^file:\/\/[^/]*(\/.*)$/.exec(data);
      if (m) {
        const cwd = decodeURIComponent(m[1]);
        if (cwd !== this.osc7Cwd) {
          this.osc7Cwd = cwd;
          opts.onTitle?.(this.title);
        }
      }
      return true;
    });
    // OSC 133 (FinalTerm / kitty / VS Code convention): A marks where the prompt starts, C that a
    // command is running — from then on the shell won't redraw, so the prompt must not be cleared.
    this.term.parser.registerOscHandler(133, (data) => {
      if (data.startsWith("A")) {
        this.promptMarker?.dispose();
        this.promptMarker = this.term.registerMarker(0) ?? undefined;
      } else if (data.startsWith("C")) {
        this.promptMarker?.dispose();
        this.promptMarker = undefined;
        // A command runs: it may create or delete files, so stop trusting what we resolved.
        clearLinkCache();
      }
      return true;
    });
    // OSC 52: the program hands us text for the clipboard. Claude Code copies this way (it owns
    // the mouse, so there is no terminal selection to copy), as do tmux and vim over ssh.
    if (t.osc52) {
      this.term.parser.registerOscHandler(52, (data) => {
        const text = parseOsc52(data);
        if (text) Clipboard.SetText(text).catch((err) => console.warn("osc52 clipboard:", err));
        return true;
      });
    }
    if (opts.keyFilter) this.term.attachCustomKeyEventHandler((e) => opts.keyFilter!(e));

    // Debounced: a divider drag fires dozens of size changes per second and every
    // column change makes the shell redraw its prompt.
    this.resizeObserver = new ResizeObserver(() => this.scheduleFit());
    this.resizeObserver.observe(this.element);

    const openUrl = (e: MouseEvent, uri: string) => {
      if (isModifierClick(e)) OpenerService.OpenURL(uri).catch((err) => console.warn(err));
    };
    this.term.loadAddon(new WebLinksAddon(openUrl));
    this.term.options.linkHandler = { activate: openUrl };
    this.term.registerLinkProvider(new FileLinkProvider(this.term, () => this.cwd(), (t) => opts.onOpenFile?.(t)));
    // The backend polls what each pane runs; the title follows it ("ssh io-main").
    this.unsubscribe = Events.On("pty:command", (ev: { data: PaneCommand }) => this.onCommand(ev.data));
  }

  /**
   * Prompt-style title: the ssh host while a session is up, otherwise the working directory
   * the way the shell prompt writes it. The shell's own title is only the last resort.
   */
  get title(): string {
    return paneTitle(this.cmd, this.lastKnownCwd(), this.oscTitle);
  }

  /** Tooltip: what the shell calls itself (user@host:path) plus the full directory. */
  get detail(): string {
    const cwd = this.lastKnownCwd();
    return [this.oscTitle, cwd && cwd !== this.oscTitle ? cwd : ""].filter(Boolean).join("\n");
  }

  private onCommand(c: PaneCommand) {
    if (c.pane !== this.opts.paneId) return;
    const before = this.title;
    this.cmd = c;
    if (this.title !== before) this.opts.onTitle?.(this.title);
  }

  /** The focused pane blinks its cursor; the others show a still outline. */
  setActive(active: boolean) {
    this.active = active;
    this.term.options.cursorBlink = active && this.opts.terminal.cursor_blink;
  }

  private webgl?: WebglAddon;
  private wantVisible = true;
  /** True while the replay text is being parsed: renderer swaps wait until it is done. */
  private replaying = false;

  /** Browsers allow ~16 WebGL contexts per page: only panes in the visible tab keep one,
   *  hidden tabs fall back to the DOM renderer until they are shown again. */
  setVisible(visible: boolean) {
    this.wantVisible = visible;
    if (!this.replaying) this.applyVisibility();
  }

  private applyVisibility() {
    if (this.disposed) return;
    if (this.wantVisible && !this.webgl) this.enableWebgl();
    else if (!this.wantVisible && this.webgl) {
      const w = this.webgl;
      this.webgl = undefined;
      try {
        w.dispose();
      } catch (err) {
        console.warn("webgl dispose:", err);
      }
    }
  }

  private enableWebgl() {
    if (this.disposed) return;
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => {
        webgl.dispose();
        if (this.webgl === webgl) this.webgl = undefined;
      });
      this.term.loadAddon(webgl);
      this.webgl = webgl;
    } catch (err) {
      // WebKitGTK without GPU acceleration ends up here; the canvas/DOM renderer still works.
      console.warn("webgl renderer unavailable, falling back", err);
    }
  }


  /** Spawn the PTY and connect. Call once the element is in the DOM. */
  async start(): Promise<void> {
    this.fitNow();
    if (this.opts.replay) {
      // Written before the PTY connects so the shell's first prompt lands below it. Swapping the
      // renderer mid-parse can stall xterm's write queue, so visibility changes wait for this.
      this.replaying = true;
      try {
        await new Promise<void>((done) => this.term.write(this.opts.replay!, done));
        // Leaving the alternate screen restores a saved cursor; done blindly it would send the
        // cursor to the top-left of a normal buffer, so only when the replay really switched.
        const leaveAlt = this.term.buffer.active.type === "alternate" ? "\x1b[?1049l" : "";
        await new Promise<void>((done) => this.term.write(leaveAlt + MODE_RESET, done));
      } finally {
        this.replaying = false;
        this.applyVisibility();
      }
    }
    if (this.opts.notice) await this.showNotice(this.opts.notice);
    // Everything above this line came out of the session file; saving it again would stack
    // one restored copy of the history on top of the next with every restart.
    this.replayEnd = this.term.registerMarker(0) ?? undefined;
    const res = await spawnQueued(() =>
      withRetry(
        () =>
          PtyService.Spawn({
            paneId: this.opts.paneId,
            tabId: this.opts.tabId,
            cwd: this.opts.cwd ?? "",
            command: this.opts.command ?? [],
            cols: this.term.cols,
            rows: this.term.rows,
          }),
        `spawn ${this.id}`,
      ),
    );
    if (this.disposed) {
      PtyService.Kill(res.id);
      return;
    }
    this.sessionId = res.id;
    // The poller only reports changes, so ask once for where this shell starts out.
    PtyService.Command(this.opts.paneId)
      .then((c) => this.onCommand(c))
      .catch(() => {});
    const ws = new WebSocket(res.url);
    ws.binaryType = "arraybuffer";
    this.ws = ws;
    ws.onmessage = (ev) => {
      if (!(ev.data instanceof ArrayBuffer)) return;
      perf.writes++;
      perf.bytes += ev.data.byteLength;
      // Time from "bytes arrived" to "the screen showed them" (see the onRender hook below) —
      // the number that decides whether typing feels immediate.
      if (perf.timing && this.pendingSince === 0) this.pendingSince = performance.now();
      this.term.write(new Uint8Array(ev.data));
    };
    ws.onopen = () => this.sendResize(this.term.cols, this.term.rows);
    ws.onclose = (ev) => {
      const m = /^exit:(-?\d+)$/.exec(ev.reason);
      this.opts.onExit?.(m ? Number(m[1]) : -1);
    };
  }

  /** Join ligature runs so the renderer draws them as one glyph (see terminal/ligatures.ts). */
  private enableLigatures() {
    if (this.joinerId !== undefined) return;
    this.joinerId = this.term.registerCharacterJoiner(ligatureJoiner());
    // The joiner groups the cells; the font feature is what substitutes the glyph when a pane
    // falls back to the DOM renderer (hidden tabs have no WebGL context).
    if (this.term.element) this.term.element.style.fontFeatureSettings = '"calt" on';
  }

  private disableLigatures() {
    if (this.joinerId === undefined) return;
    this.term.deregisterCharacterJoiner(this.joinerId);
    this.joinerId = undefined;
    if (this.term.element) this.term.element.style.fontFeatureSettings = "";
  }

  private send(data: string, binary = false) {
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    if (binary) {
      const bytes = new Uint8Array(data.length);
      for (let i = 0; i < data.length; i++) bytes[i] = data.charCodeAt(i) & 0xff;
      this.ws.send(bytes);
    } else {
      this.ws.send(ENCODER.encode(data));
    }
  }

  private sendResize(cols: number, rows: number) {
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    this.ws.send(JSON.stringify({ type: "resize", cols, rows }));
  }

  /** Re-apply appearance settings (config reload, zoom). */
  applyConfig(t: TerminalConfig, fontDelta = 0, theme?: Record<string, string>) {
    // Keep the pane's copy current: setActive() and the OSC 52 gate read from it.
    this.opts.terminal = t;
    if (theme) this.term.options.theme = theme;
    this.term.options.fontFamily = t.font;
    this.term.options.fontSize = Math.max(6, t.font_size + fontDelta);
    this.term.options.lineHeight = t.line_height || 1;
    this.term.options.scrollback = t.scrollback;
    this.term.options.cursorStyle = t.cursor_style as ITerminalOptions["cursorStyle"];
    this.term.options.cursorBlink = this.active && t.cursor_blink;
    this.element.style.padding = `${t.padding}px`;
    if (t.ligatures) this.enableLigatures();
    else this.disableLigatures();
    this.fitNow();
  }

  /** Scrollback + screen as ANSI text (for sessions); at most maxBytes, cut at a line boundary. */
  serialize(maxBytes: number): string {
    const started = perf.timing ? performance.now() : 0;
    let text: string;
    try {
      // Modes (mouse tracking, alt screen, …) belong to the program that set them, not to the text.
      text = this.serializer.serialize({ ...this.savedRange(maxBytes), excludeModes: true, excludeAltBuffer: true });
    } catch {
      return "";
    }
    if (perf.timing) perf.serializeMs += performance.now() - started;
    text = text.replace(/\s+$/, "");
    if (!text) return "";
    if (text.length > maxBytes) {
      const cut = text.indexOf("\n", text.length - maxBytes);
      text = text.slice(cut >= 0 ? cut + 1 : text.length - maxBytes);
    }
    return text.replace(/\r?\n/g, "\r\n") + "\r\n\x1b[0m\x1b[2m── restored ──\x1b[0m\r\n";
  }

  /**
   * What gets saved: the lines this session wrote, never the history that was replayed into the
   * pane — saving that again stacks another copy of it with every restart. Once the marker has
   * scrolled out of the buffer the whole scrollback belongs to this session anyway.
   */
  private savedRange(maxBytes: number): ISerializeOptions {
    // Serialising 10 000 lines for a 48 KiB budget is most of the cost of an autosave, so the
    // line count is capped to what the budget can hold anyway (colour escapes included).
    const cap = Math.max(200, Math.ceil(maxBytes / BYTES_PER_LINE));
    const limit = Math.min(this.term.options.scrollback ?? 1000, cap);
    const end = this.term.buffer.normal.length - 1;
    if (!this.replayEnd || this.replayEnd.isDisposed) return { scrollback: limit };
    const start = Math.max(this.replayEnd.line, end - limit);
    return { range: { start, end: Math.max(start, end) } };
  }

  /** Set (or clear) the CTA block before start(); a later call replaces the visible one. */
  setNotice(notice: PaneNotice | undefined) {
    this.opts.notice = notice;
    if (this.sessionId && notice) void this.showNotice(notice);
  }

  /**
   * Reserve rows below the replayed history and hang a full-width block on them: an xterm
   * decoration, so the block scrolls with the buffer and vanishes with its lines.
   */
  private async showNotice(n: PaneNotice) {
    this.clearNotice();
    await new Promise<void>((done) => this.term.write("\r\n", done));
    const marker = this.term.registerMarker(0);
    if (!marker) return;
    await new Promise<void>((done) => this.term.write("\r\n".repeat(NOTICE_ROWS), done));
    const dec = this.term.registerDecoration({ marker, x: 0, width: this.term.cols, height: NOTICE_ROWS, layer: "top" });
    if (!dec) return;
    this.notice = dec;
    dec.onRender((el) => {
      // The width is fixed in cells at registration; keep it full width across resizes.
      // onRender fires every frame, so only touch the style when it actually differs.
      if (el.style.width !== "100%") el.style.width = "100%";
      if (el.firstChild) return;
      el.classList.add("pane-notice");
      const font = { family: String(this.term.options.fontFamily ?? ""), size: Number(this.term.options.fontSize ?? 13) };
      el.appendChild(noticeBlock(n, font, () => {
        this.clearNotice();
        n.onActivate();
      }));
    });
  }

  private clearNotice() {
    this.notice?.dispose();
    this.notice = undefined;
  }

  /** Type a command into the shell and run it (the restore CTA). */
  runCommand(text: string) {
    this.send(text + "\r");
  }

  /** Synchronous best guess (OSC 7, the polled process cwd, or the spawn cwd). */
  lastKnownCwd(): string {
    return this.osc7Cwd ?? this.cmd?.cwd ?? this.opts.cwd ?? "";
  }

  async cwd(): Promise<string> {
    if (this.osc7Cwd) return this.osc7Cwd;
    if (!this.sessionId) return this.opts.cwd ?? "";
    return PtyService.Cwd(this.sessionId);
  }

  /**
   * Shells redraw their prompt on SIGWINCH assuming it still occupies the same rows, but
   * xterm.js reflows lines that no longer fit (p10k's full-width ruler always does), leaving
   * a stale copy behind. Like kitty, we blank the prompt region before the width changes so
   * nothing reflows and the shell's redraw lands exactly where it expects.
   */
  private promptClearSequence(newCols: number): string | null {
    const m = this.promptMarker;
    const buf = this.term.buffer.active;
    const cursorLine = buf.baseY + buf.cursorY;
    if (!m || m.isDisposed || newCols === this.term.cols) return null;
    if (buf.type !== "normal") return null;
    const up = cursorLine - m.line;
    if (up < 0 || up > 8) return null;
    // DECSC, cursor up to the prompt line, erase to end of screen, DECRC: rows stay blank.
    return `\x1b7${up > 0 ? `\x1b[${up}A` : ""}\r\x1b[J\x1b8`;
  }

  relayout() {
    this.fitSuspended = false;
    this.scheduleFit();
    // Moving the element in the DOM (pane swap) leaves the renderer with a blank canvas.
    this.term.refresh(0, this.term.rows - 1);
  }

  setFitSuspended(suspended: boolean) {
    this.fitSuspended = suspended;
    if (suspended) clearTimeout(this.fitTimer);
  }

  private scheduleFit() {
    if (this.fitSuspended) return;
    clearTimeout(this.fitTimer);
    this.fitTimer = setTimeout(() => this.fitNow(), 60);
  }

  fitNow() {
    if (this.element.clientWidth === 0 || this.element.clientHeight === 0) return;
    try {
      const dims = this.fit.proposeDimensions();
      const clear = dims ? this.promptClearSequence(dims.cols) : null;
      // term.write is asynchronous: resize only once the clear has been parsed.
      if (clear) this.term.write(clear, () => this.fit.fit());
      else this.fit.fit();
    } catch {
      /* not attached yet */
    }
  }

  focus() {
    this.term.focus();
  }

  /** Current session id (undefined before start resolves). */
  get session(): string | undefined {
    return this.sessionId;
  }

  dispose() {
    if (this.disposed) return;
    this.disposed = true;
    this.unsubscribe?.();
    this.resizeObserver.disconnect();
    this.clearNotice();
    this.ws?.close();
    if (this.sessionId) PtyService.Kill(this.sessionId);
    this.term.dispose();
    this.element.remove();
  }
}

/**
 * The notice's contents, styled as an inset in the terminal's own typeface: Claude's mark in an
 * orange gutter, the session on two lines, and the action as a quiet chip on the right.
 */
function noticeBlock(n: PaneNotice, font: { family: string; size: number }, activate: () => void): HTMLElement {
  const box = document.createElement("div");
  box.className = "pane-notice-box";
  box.style.fontFamily = font.family;
  box.style.fontSize = `${font.size}px`;
  const icon = document.createElement("span");
  icon.className = "pane-notice-icon";
  icon.innerHTML = CLAUDE_LOGO;
  const text = document.createElement("div");
  text.className = "pane-notice-text";
  const line1 = document.createElement("div");
  line1.className = "pane-notice-line";
  const kind = document.createElement("span");
  kind.className = "pane-notice-kind";
  kind.textContent = "claude session";
  const name = document.createElement("span");
  name.className = "pane-notice-title";
  name.textContent = `"${n.title}"`;
  line1.append(kind, " ", name);
  text.appendChild(line1);
  if (n.detail) {
    const line2 = document.createElement("div");
    line2.className = "pane-notice-line pane-notice-sub";
    line2.textContent = n.detail;
    text.appendChild(line2);
  }
  const cta = document.createElement("span");
  cta.className = "pane-notice-cta";
  cta.innerHTML = `${PLAY} ${n.action}`;
  box.append(icon, text, cta);
  if (n.hint) box.title = n.hint;
  // The decoration sits on top of the terminal: swallow the events xterm would read as
  // a selection drag, and let a click anywhere in the block trigger the action.
  box.addEventListener("mousedown", (e) => e.stopPropagation());
  box.addEventListener("click", (e) => {
    e.preventDefault();
    e.stopPropagation();
    activate();
  });
  return box;
}
