import { Clipboard, Window } from "@wailsio/runtime";

import type { Config } from "./api";
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

  applyConfig(config: Config) {
    this.config = config;
    this.keymap = new Keymap(config.keys, config.general.passthrough ?? []);
    for (const p of this.allPanes()) if (p instanceof TerminalPane) p.applyConfig(config.terminal, this.fontDelta);
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
      fontDelta: this.fontDelta,
      keyFilter: (e) => this.keymap.match(e) === null,
      onExit: () => this.removePane(tab, pane),
      onTitle: () => tab.onChange?.(),
    });
    tab.add(pane, dir);
    await pane.start();
    pane.focus();
    return pane;
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
    for (const p of this.allPanes()) if (p instanceof TerminalPane) p.applyConfig(this.config.terminal, this.fontDelta);
  }
}
