import { Terminal, type ITerminalOptions } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
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
  private ws?: WebSocket;
  private sessionId?: string;
  private resizeObserver: ResizeObserver;
  private disposed = false;
  private fitTimer?: ReturnType<typeof setTimeout>;
  private fitSuspended = false;
  private active = false;

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

  async cwd(): Promise<string> {
    if (!this.sessionId) return this.opts.cwd ?? "";
    return PtyService.Cwd(this.sessionId);
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
      this.fit.fit();
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
