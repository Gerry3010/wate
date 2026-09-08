import { Terminal, type ITerminalOptions } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import "@xterm/xterm/css/xterm.css";

import { LigaturesAddon } from "@xterm/addon-ligatures";

import { PtyService, type TerminalConfig } from "../api";

export interface TerminalPaneOptions {
  paneId: string;
  tabId: string;
  cwd?: string;
  command?: string[];
  terminal: TerminalConfig;
  onExit?: (code: number) => void;
  onTitle?: (title: string) => void;
}

/** One xterm.js instance wired to a PTY session over the loopback WebSocket. */
export class TerminalPane {
  readonly element: HTMLElement;
  readonly term: Terminal;
  private fit = new FitAddon();
  private ws?: WebSocket;
  private sessionId?: string;
  private resizeObserver: ResizeObserver;
  private disposed = false;

  constructor(private opts: TerminalPaneOptions) {
    this.element = document.createElement("div");
    this.element.className = "pane pane-terminal";
    this.element.dataset.paneId = opts.paneId;

    const t = opts.terminal;
    this.element.style.padding = `${t.padding}px`;
    const termOpts: ITerminalOptions = {
      fontFamily: t.font,
      fontSize: t.font_size,
      lineHeight: t.line_height || 1,
      scrollback: t.scrollback,
      cursorStyle: t.cursor_style as ITerminalOptions["cursorStyle"],
      cursorBlink: t.cursor_blink,
      allowProposedApi: true,
      allowTransparency: true,
      macOptionIsMeta: true,
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
    this.term.onTitleChange((t) => opts.onTitle?.(t));

    this.resizeObserver = new ResizeObserver(() => this.fitNow());
    this.resizeObserver.observe(this.element);
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
