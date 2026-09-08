import { Clipboard, Window } from "@wailsio/runtime";

import { OpenerService, ThemeService, type Config, type Resolved, type Target } from "./api";
import { applyTheme, xtermTheme } from "./theme/apply";
import { Keymap } from "./keymap/keymap";
import { neighbor, type Dir, type Direction } from "./layout/tree";
import type { Pane } from "./pane";
import { Tab, nextId } from "./tabs/tab";
import { TabBar } from "./tabs/tabbar";
import { TerminalPane } from "./terminal/pane";

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

  /** Ctrl-click target: text files go to the editor (milestone 5), everything else to the OS. */
  openTarget(_tab: Tab, t: Target) {
    if (t.kind === "file" && t.text) {
      // TODO(editor): open in an editor pane next to the terminal.
      console.warn("editor pane not implemented yet, opening externally:", t.path);
    }
    OpenerService.Open(t.path).catch((err) => console.warn("open failed", err));
  }

  private removePane(tab: Tab, pane: Pane) {
    tab.remove(pane.id);
    if (tab.isEmpty) this.closeTab(tab);
    else tab.focusPane();
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
        if (tab?.focused) this.removePane(tab, tab.focused);
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
    if (p instanceof TerminalPane) {
      const sel = p.term.getSelection();
      if (sel) await Clipboard.SetText(sel);
    }
  }

  private async paste() {
    const p = this.active?.focused;
    if (p instanceof TerminalPane) {
      const text = await Clipboard.Text();
      if (text) p.term.paste(text);
    }
  }

  private zoom(step: number) {
    this.fontDelta = step === 0 ? 0 : this.fontDelta + step;
    for (const p of this.allPanes()) if (p instanceof TerminalPane) p.applyConfig(this.config.terminal, this.fontDelta, this.termTheme());
  }
}
