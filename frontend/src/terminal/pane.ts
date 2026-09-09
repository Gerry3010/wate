import { Terminal, type IMarker, type ITerminalOptions } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SerializeAddon } from "@xterm/addon-serialize";
import { WebglAddon } from "@xterm/addon-webgl";
import "@xterm/xterm/css/xterm.css";

import { LigaturesAddon } from "@xterm/addon-ligatures";
import { WebLinksAddon } from "@xterm/addon-web-links";

import { OpenerService, PtyService, type Target, type TerminalConfig } from "../api";
import type { Pane } from "../pane";
import { FileLinkProvider, isModifierClick } from "./links";

export interface TerminalPaneOptions {
  paneId: string;
  tabId: string;
  cwd?: string;
  command?: string[];
  /** Terminal text (ANSI) written before the shell starts: restored scrollback / imported history. */
  replay?: string;
  terminal: TerminalConfig;
  theme?: Record<string, string>;
  fontDelta?: number;
  /** Return false for keys the app handles itself (so xterm ignores them). */
  keyFilter?: (e: KeyboardEvent) => boolean;
  onExit?: (code: number) => void;
  onTitle?: (title: string) => void;
  /** Ctrl/Cmd-click on an existing file or directory. */
  onOpenFile?: (t: Target) => void;
}

/** One xterm.js instance wired to a PTY session over the loopback WebSocket. */
export class TerminalPane implements Pane {
  readonly id: string;
  readonly kind = "terminal" as const;
  readonly element: HTMLElement;
  readonly term: Terminal;
  title = "";
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
      theme: opts.theme,
    };
    this.term = new Terminal(termOpts);
    this.term.loadAddon(this.fit);
    this.term.loadAddon(this.serializer);
    this.term.open(this.element);
    this.enableWebgl();
    if (t.ligatures) {
      // Without Node the addon can't read the font file; it falls back to a built-in
      // list of common programming ligatures, which is what we want here.
      try {
        this.term.loadAddon(new LigaturesAddon());
      } catch (err) {
        console.warn("ligatures addon unavailable", err);
      }
    }

    this.term.onData((data) => this.send(data));
    this.term.onBinary((data) => this.send(data, true));
    this.term.onResize(({ cols, rows }) => this.sendResize(cols, rows));
    this.term.onTitleChange((title) => {
      this.title = title;
      opts.onTitle?.(title);
    });
    // OSC 7: file://host/path — the shell tells us its cwd (cheaper and more reliable than /proc).
    this.term.parser.registerOscHandler(7, (data) => {
      const m = /^file:\/\/[^/]*(\/.*)$/.exec(data);
      if (m) this.osc7Cwd = decodeURIComponent(m[1]);
      return true;
    });
    // OSC 133;A marks where the prompt starts (FinalTerm / kitty / VS Code convention).
    this.term.parser.registerOscHandler(133, (data) => {
      if (data.startsWith("A")) {
        this.promptMarker?.dispose();
        this.promptMarker = this.term.registerMarker(0) ?? undefined;
      }
      return true;
    });
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
  }

  /** The focused pane blinks its cursor; the others show a still outline. */
  setActive(active: boolean) {
    this.active = active;
    this.term.options.cursorBlink = active && this.opts.terminal.cursor_blink;
  }

  private enableWebgl() {
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => webgl.dispose());
      this.term.loadAddon(webgl);
    } catch (err) {
      // WebKitGTK without GPU acceleration ends up here; the canvas/DOM renderer still works.
      console.warn("webgl renderer unavailable, falling back", err);
    }
  }

  /** Spawn the PTY and connect. Call once the element is in the DOM. */
  async start(): Promise<void> {
    this.fitNow();
    if (this.opts.replay) {
      // Written before the PTY connects so the shell's first prompt lands below it.
      await new Promise<void>((done) => this.term.write(this.opts.replay!, done));
    }
    const res = await PtyService.Spawn({
      paneId: this.opts.paneId,
      tabId: this.opts.tabId,
      cwd: this.opts.cwd ?? "",
      command: this.opts.command ?? [],
      cols: this.term.cols,
      rows: this.term.rows,
    });
    if (this.disposed) {
      PtyService.Kill(res.id);
      return;
    }
    this.sessionId = res.id;
    const ws = new WebSocket(res.url);
    ws.binaryType = "arraybuffer";
    this.ws = ws;
    ws.onmessage = (ev) => {
      if (ev.data instanceof ArrayBuffer) this.term.write(new Uint8Array(ev.data));
    };
    ws.onopen = () => this.sendResize(this.term.cols, this.term.rows);
    ws.onclose = (ev) => {
      const m = /^exit:(-?\d+)$/.exec(ev.reason);
      this.opts.onExit?.(m ? Number(m[1]) : -1);
    };
  }

  private send(data: string, binary = false) {
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    if (binary) {
      const bytes = new Uint8Array(data.length);
      for (let i = 0; i < data.length; i++) bytes[i] = data.charCodeAt(i) & 0xff;
      this.ws.send(bytes);
    } else {
      this.ws.send(new TextEncoder().encode(data));
    }
  }

  private sendResize(cols: number, rows: number) {
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    this.ws.send(JSON.stringify({ type: "resize", cols, rows }));
  }

  /** Re-apply appearance settings (config reload, zoom). */
  applyConfig(t: TerminalConfig, fontDelta = 0, theme?: Record<string, string>) {
    if (theme) this.term.options.theme = theme;
    this.term.options.fontFamily = t.font;
    this.term.options.fontSize = Math.max(6, t.font_size + fontDelta);
    this.term.options.lineHeight = t.line_height || 1;
    this.term.options.scrollback = t.scrollback;
    this.term.options.cursorStyle = t.cursor_style as ITerminalOptions["cursorStyle"];
    this.term.options.cursorBlink = this.active && t.cursor_blink;
    this.element.style.padding = `${t.padding}px`;
    this.fitNow();
  }

  /** Scrollback + screen as ANSI text (for sessions); at most maxBytes, cut at a line boundary. */
  serialize(maxBytes: number): string {
    let text: string;
    try {
      text = this.serializer.serialize({ scrollback: this.term.options.scrollback ?? 1000 });
    } catch {
      return "";
    }
    text = text.replace(/\s+$/, "");
    if (!text) return "";
    if (text.length > maxBytes) {
      const cut = text.indexOf("\n", text.length - maxBytes);
      text = text.slice(cut >= 0 ? cut + 1 : text.length - maxBytes);
    }
    return text.replace(/\r?\n/g, "\r\n") + "\r\n\x1b[0m\x1b[2m── restored ──\x1b[0m\r\n";
  }

  /** Synchronous best guess (OSC 7 or the spawn cwd) for use where we can't await. */
  lastKnownCwd(): string {
    return this.osc7Cwd ?? this.opts.cwd ?? "";
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
    this.resizeObserver.disconnect();
    this.ws?.close();
    if (this.sessionId) PtyService.Kill(this.sessionId);
    this.term.dispose();
    this.element.remove();
  }
}
