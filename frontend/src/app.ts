import { Clipboard, Window } from "@wailsio/runtime";

import { OpenerService, ThemeService, type Config, type Resolved, type Target } from "./api";
import { applyTheme, xtermTheme } from "./theme/apply";
import { Keymap } from "./keymap/keymap";
import { neighbor, type Dir, type Direction } from "./layout/tree";
import type { Pane } from "./pane";
import { Tab, nextId } from "./tabs/tab";
import { TabBar } from "./tabs/tabbar";
import { TerminalPane } from "./terminal/pane";
import { EditorPane } from "./editor/pane";

/** Top-level UI state: tabs, panes, keybindings and the actions they trigger. */
export class YateApp {
  readonly tabs: Tab[] = [];
  active: Tab | null = null;
  private tabBar: TabBar;
  private content = document.createElement("div");
  private keymap: Keymap;
  private fontDelta = 0;
  private theme?: Resolved;

  constructor(root: HTMLElement, public config: Config) {
    this.keymap = new Keymap(config.keys, config.general.passthrough ?? []);
    this.tabBar = new TabBar({
      activate: (t) => this.activate(t),
      close: (t) => this.closeTab(t),
      newTab: () => this.newTab(),
    });
    this.content.className = "content";
    root.append(this.tabBar.element, this.content);
    window.addEventListener("keydown", (e) => this.onKey(e), { capture: true });
  }

  async applyConfig(config: Config) {
    this.config = config;
    this.keymap = new Keymap(config.keys, config.general.passthrough ?? []);
    await this.loadTheme();
    for (const p of this.allPanes()) if (p instanceof TerminalPane) p.applyConfig(config.terminal, this.fontDelta, this.termTheme());
  }

  /** Fetch the resolved theme and push it into CSS + panes. */
  async loadTheme() {
    try {
      this.theme = await ThemeService.Get();
    } catch (err) {
      console.warn("theme:", err);
      if (!this.theme) return;
    }
    applyTheme(this.theme!, this.config.background);
    this.applyEditorVars();
  }

  private termTheme(): Record<string, string> | undefined {
    return this.theme ? xtermTheme(this.theme, this.config.background) : undefined;
  }

  // ---- tabs -------------------------------------------------------------

  async newTab(opts: { cwd?: string; command?: string[] } = {}): Promise<Tab> {
    const tab = new Tab();
    tab.onChange = () => this.refreshChrome(tab);
    this.tabs.push(tab);
    this.activate(tab);
    await this.addTerminal(tab, "row", opts);
    return tab;
  }

  activate(tab: Tab) {
    if (this.active === tab) return;
    this.active?.element.classList.remove("active");
    this.active = tab;
    tab.element.classList.add("active");
    if (!tab.element.parentElement) this.content.appendChild(tab.element);
    tab.render();
    this.refreshChrome(tab);
    tab.focusPane();
  }

  closeTab(tab: Tab) {
    const i = this.tabs.indexOf(tab);
    if (i < 0) return;
    this.tabs.splice(i, 1);
    tab.dispose();
    if (this.active === tab) {
      this.active = null;
      const next = this.tabs[Math.min(i, this.tabs.length - 1)];
      if (next) this.activate(next);
      else void Window.Close();
    }
    this.refreshChrome();
  }

  private refreshChrome(tab?: Tab) {
    this.tabBar.render(this.tabs, this.active);
    if (this.active && (!tab || tab === this.active)) void Window.SetTitle(`${this.active.title} — yate`).catch(() => {});
  }

  // ---- panes ------------------------------------------------------------

  private async addTerminal(tab: Tab, dir: Dir, opts: { cwd?: string; command?: string[] } = {}): Promise<TerminalPane> {
    const cwd = opts.cwd ?? (await tab.focused?.cwd().catch(() => "")) ?? "";
    const pane = new TerminalPane({
      paneId: nextId("pane"),
      tabId: tab.id,
      cwd,
      command: opts.command,
      terminal: this.config.terminal,
      theme: this.termTheme(),
      fontDelta: this.fontDelta,
      keyFilter: (e) => this.keymap.match(e) === null,
      onExit: () => this.removePane(tab, pane),
      onTitle: () => tab.onChange?.(),
      onOpenFile: (t) => this.openTarget(tab, t),
    });
    tab.add(pane, dir);
    await pane.start();
    pane.focus();
    return pane;
  }

  /** Ctrl-click target: text files go to an editor pane, everything else to the OS. */
  openTarget(tab: Tab, t: Target) {
    if (t.kind === "file" && t.text) {
      void this.openEditor(tab, t.path, t.line, t.col);
      return;
    }
    OpenerService.Open(t.path).catch((err) => console.warn("open failed", err));
  }

  /** Open (or focus) an editor for path, split to the right of the focused pane. */
  async openEditor(tab: Tab, path: string, line?: number, col?: number): Promise<void> {
    for (const p of tab.panes.values()) {
      if (p instanceof EditorPane && p.path === path) {
        tab.setFocus(p.id);
        if (line) p.gotoLine(line, col);
        p.focus();
        return;
      }
    }
    const pane = new EditorPane({
      paneId: nextId("pane"),
      path,
      line,
      col,
      editor: this.config.editor,
      onTitle: () => tab.onChange?.(),
      onClose: () => void this.closePane(tab, pane),
      onLink: (href, fromDir) => this.openPreviewLink(tab, href, fromDir),
    });
    try {
      tab.add(pane, "row");
      await pane.load();
      pane.focus();
    } catch (err) {
      console.warn("editor:", err);
      tab.remove(pane.id);
      tab.focusPane();
    }
  }

  private openPreviewLink(tab: Tab, href: string, fromDir: string) {
    if (/^[a-z][a-z0-9+.-]*:/i.test(href)) {
      OpenerService.OpenURL(href).catch((err) => console.warn(err));
      return;
    }
    const clean = href.split("#")[0];
    if (!clean) return;
    void OpenerService.Resolve(fromDir, [clean]).then((ts) => {
      const t = ts?.[0];
      if (t && t.kind !== "missing") this.openTarget(tab, t);
    });
  }

  /** Close a pane, asking about unsaved editor changes first. */
  private async closePane(tab: Tab, pane: Pane) {
    if (pane instanceof EditorPane && !(await pane.confirmClose())) return;
    this.removePane(tab, pane);
  }

  private removePane(tab: Tab, pane: Pane) {
    tab.remove(pane.id);
    if (tab.isEmpty) this.closeTab(tab);
    else tab.focusPane();
  }

  /** Editor font settings live in CSS variables so CodeMirror and the preview pick them up. */
  private applyEditorVars() {
    const e = this.config.editor;
    const root = document.documentElement;
    root.style.setProperty("--editor-font", e.font);
    root.style.setProperty("--editor-font-size", `${e.font_size + this.fontDelta}px`);
    root.style.setProperty("--editor-line-height", String(e.line_height || 1.5));
    root.style.setProperty("--editor-ligatures", e.ligatures ? "normal" : "none");
  }

  private allPanes(): Pane[] {
    return this.tabs.flatMap((t) => Array.from(t.panes.values()));
  }

  // ---- actions ----------------------------------------------------------

  private onKey(e: KeyboardEvent) {
    const action = this.keymap.match(e);
    if (!action) return;
    e.preventDefault();
    e.stopPropagation();
    void this.run(action);
  }

  async run(action: string): Promise<void> {
    const tab = this.active;
    const m = /^tab_(\d)$/.exec(action);
    if (m) {
      const t = this.tabs[Number(m[1]) - 1];
      if (t) this.activate(t);
      return;
    }
    switch (action) {
      case "split_right":
        if (tab) await this.addTerminal(tab, "row");
        break;
      case "split_down":
        if (tab) await this.addTerminal(tab, "col");
        break;
      case "focus_left":
      case "focus_right":
      case "focus_up":
      case "focus_down":
        this.focusDir(action.slice("focus_".length) as Direction);
        break;
      case "resize_left":
      case "resize_right":
      case "resize_up":
      case "resize_down":
        tab?.resize(action.slice("resize_".length) as Direction);
        break;
      case "close_pane":
        if (tab?.focused) await this.closePane(tab, tab.focused);
        break;
      case "editor_save":
        if (tab?.focused instanceof EditorPane) await tab.focused.save();
        break;
      case "editor_mode":
        if (tab?.focused instanceof EditorPane) tab.focused.cycleMode();
        break;
      case "search":
        if (tab?.focused instanceof EditorPane) tab.focused.openSearch();
        break;
      case "new_tab":
        await this.newTab();
        break;
      case "next_tab":
        this.cycleTab(1);
        break;
      case "prev_tab":
        this.cycleTab(-1);
        break;
      case "copy":
        await this.copy();
        break;
      case "paste":
        await this.paste();
        break;
      case "font_bigger":
        this.zoom(1);
        break;
      case "font_smaller":
        this.zoom(-1);
        break;
      case "font_reset":
        this.zoom(0);
        break;
      default:
        console.warn("unhandled action", action);
    }
  }

  private focusDir(dir: Direction) {
    const tab = this.active;
    if (!tab?.focusedId) return;
    const next = neighbor(tab.rects(), tab.focusedId, dir);
    if (next) {
      tab.setFocus(next);
      tab.focusPane();
    }
  }

  private cycleTab(delta: number) {
    if (!this.active || this.tabs.length < 2) return;
    const i = this.tabs.indexOf(this.active);
    this.activate(this.tabs[(i + delta + this.tabs.length) % this.tabs.length]);
  }

  private async copy() {
    const p = this.active?.focused;
    const sel = p instanceof TerminalPane ? p.term.getSelection() : p instanceof EditorPane ? p.selectedText() : "";
    if (sel) await Clipboard.SetText(sel);
  }

  private async paste() {
    const p = this.active?.focused;
    const text = await Clipboard.Text();
    if (!text) return;
    if (p instanceof TerminalPane) p.term.paste(text);
    else if (p instanceof EditorPane) p.insertText(text);
  }

  private zoom(step: number) {
    this.fontDelta = step === 0 ? 0 : this.fontDelta + step;
    this.applyEditorVars();
    for (const p of this.allPanes()) if (p instanceof TerminalPane) p.applyConfig(this.config.terminal, this.fontDelta, this.termTheme());
  }
}
