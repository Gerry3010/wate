import { Clipboard, Window } from "@wailsio/runtime";

import { AgentService, OpenerService, SessionService, StateService, ThemeService, type Config, type Resolved, type Session, type Target } from "./api";
import { fromImported, parseSession, remapTree, type ImportedTab, type SavedClaude, type SavedPane, type SavedSession, type SavedTab } from "./session";
import { AgentStore } from "./agent/store";
import { updateBadge } from "./agent/badge";
import { closeMenu } from "./ui/menu";
import { Sidebar, shortPath } from "./sidebar/sidebar";
import { applyTheme, xtermTheme } from "./theme/apply";
import { Keymap } from "./keymap/keymap";
import { neighbor, type Dir, type Direction } from "./layout/tree";
import type { Pane } from "./pane";
import { Tab, nextId } from "./tabs/tab";
import { TabBar } from "./tabs/tabbar";
import { TerminalPane, type PaneNotice } from "./terminal/pane";
import { EditorPane } from "./editor/pane";
import { SettingsPane } from "./settings/pane";

/** Top-level UI state: tabs, panes, keybindings and the actions they trigger. */
/** Scrollback kept per pane (bytes of ANSI text): named sessions keep more than the auto-restore file. */
const SESSION_SCROLLBACK = 256 * 1024;
const RESTORE_SCROLLBACK = 48 * 1024;

export class WateApp {
  readonly tabs: Tab[] = [];
  active: Tab | null = null;
  private tabBar: TabBar;
  private content = document.createElement("div");
  private main = document.createElement("div");
  readonly agents = new AgentStore();
  private sidebar: Sidebar;
  private keymap: Keymap;
  private fontDelta = 0;
  private theme?: Resolved;
  private saveTimer?: ReturnType<typeof setTimeout>;
  private restoring = false;

  configPath = "";

  constructor(root: HTMLElement, public config: Config) {
    this.keymap = new Keymap(config.keys, config.general.passthrough ?? []);
    this.tabBar = new TabBar({
      activate: (t) => this.activate(t),
      close: (t) => this.closeTab(t),
      newTab: () => this.newTab(),
      openSettings: () => this.openSettings(),
      renameTab: (t, title) => {
        t.customTitle = title;
        t.onChange?.();
      },
      colorTab: (t, color) => {
        t.color = color;
        t.onChange?.();
      },
      listSessions: async () => (await SessionService.List()) ?? [],
      saveSession: (name) => void this.saveNamedSession(name),
      openSession: (id) => void this.openNamedSession(id),
      deleteSession: (id) => void SessionService.Delete(id).catch((err) => console.warn("delete session:", err)),
      openImport: () => this.openSettings("import"),
      agentStatus: (t) => this.agents.forTab(t.id),
    });
    this.sidebar = new Sidebar(this.agents, {
      jumpTo: (s) => this.jumpToSession(s),
      launch: () => void this.launchClaude(),
      launchLabel: this.keymap.label("launch_claude") || "Launch",
      toggleLabel: this.keymap.label("toggle_sidebar"),
      onVisibility: () => this.active?.render(),
    });
    this.content.className = "content";
    this.main.className = "main";
    this.main.append(this.content, this.sidebar.element);
    root.append(this.tabBar.element, this.main);
    window.addEventListener("keydown", (e) => this.onKey(e), { capture: true });
    window.addEventListener("focus", () => this.reportFocus());
    window.addEventListener("blur", () => this.reportFocus());
    this.agents.subscribe(() => this.onAgentsChanged());
    AgentService.List().then((l) => this.agents.replaceAll(l ?? [])).catch(() => {});
    window.addEventListener("beforeunload", () => void this.saveSession(true));
  }

  // ---- session persistence ------------------------------------------------

  /** Debounced: called after every structural change. */
  /** Set for windows opened while another wate runs: they neither restore nor save the session. */
  secondary = false;

  scheduleSave() {
    if (this.restoring || this.secondary || !this.config.general.restore_session) return;
    clearTimeout(this.saveTimer);
    this.saveTimer = setTimeout(() => void this.saveSession(), 800);
  }

  async saveSession(immediate = false): Promise<void> {
    if (this.restoring || this.secondary || !this.config.general.restore_session) return;
    const state = await this.snapshot(immediate, RESTORE_SCROLLBACK);
    await StateService.Save(JSON.stringify(state)).catch((err) => console.warn("session save:", err));
  }

  /** Serialise every tab (layout, cwds, open files, titles, colours, and — with scrollback — the terminal text). */
  async snapshot(immediate = false, scrollback = 0): Promise<SavedSession> {
    const tabs: SavedSession["tabs"] = [];
    for (const t of this.tabs) {
      if (!t.tree) continue;
      const panes: SavedPane[] = [];
      for (const p of t.panes.values()) {
        if (p instanceof EditorPane) panes.push({ id: p.id, kind: "editor", path: p.path });
        else if (p instanceof TerminalPane) {
          const cwd = immediate ? p.lastKnownCwd() : await p.cwd().catch(() => "");
          panes.push({
            id: p.id,
            kind: "terminal",
            cwd,
            replay: scrollback > 0 ? p.serialize(scrollback) || undefined : undefined,
            claude: this.claudeOf(p),
          });
        }
      }
      if (panes.length === 0) continue;
      tabs.push({ tree: t.tree, focused: t.focusedId, panes, title: t.customTitle || undefined, color: t.color || undefined });
    }
    return { version: 1, active: this.active ? this.tabs.indexOf(this.active) : 0, tabs };
  }

  /** Store the current tabs under a name (tab bar dropdown). */
  async saveNamedSession(name: string): Promise<void> {
    try {
      const state = await this.snapshot(false, SESSION_SCROLLBACK);
      await SessionService.Save(name, JSON.stringify(state));
    } catch (err) {
      console.warn("save session:", err);
    }
  }

  /** Open a named session: its tabs are added after the current ones. */
  async openNamedSession(id: string): Promise<void> {
    try {
      const saved = parseSession(await SessionService.Load(id));
      if (!saved) throw new Error("session file is empty or invalid");
      const opened = await this.openTabs(saved.tabs);
      const active = opened[Math.min(saved.active, opened.length - 1)];
      if (active) this.activate(active);
    } catch (err) {
      console.warn("open session:", err);
    }
  }

  /** Open tabs delivered by the importer (Warp etc.). */
  async importTabs(tabs: ImportedTab[]): Promise<number> {
    const opened = await this.openTabs(fromImported(tabs));
    if (opened[0]) this.activate(opened[0]);
    return opened.length;
  }

  /** Rebuild tabs from the saved state; returns false when nothing was restored. */
  async restoreSession(): Promise<boolean> {
    if (this.secondary || !this.config.general.restore_session) return false;
    const saved = parseSession(await StateService.Load().catch(() => ""));
    if (!saved) return false;
    this.restoring = true;
    try {
      const opened = await this.openTabs(saved.tabs);
      const active = opened[saved.active];
      if (active) this.activate(active);
      this.active?.focusPane();
      return this.tabs.length > 0;
    } finally {
      this.restoring = false;
    }
  }

  /** Create tabs from saved descriptions (session restore, named sessions, imports). */
  private async openTabs(saved: SavedTab[]): Promise<Tab[]> {
    const opened: Tab[] = [];
    for (const st of saved) {
      const tab = new Tab();
      tab.customTitle = st.title ?? "";
      tab.color = st.color ?? "";
      tab.onChange = () => {
        this.refreshChrome(tab);
        if (tab === this.active) this.reportFocus();
        this.scheduleSave();
      };
      tab.onLayout = (r) => this.sidebar.cutEdge(tab === this.active ? r : null);
      const map = new Map<string, string>();
      const panes: Pane[] = [];
      const starts: Promise<unknown>[] = [];
      for (const sp of st.panes) {
        if (sp.kind === "editor") {
          const pane = this.makeEditor(tab, sp.path);
          map.set(sp.id, pane.id);
          panes.push(pane);
          starts.push(pane.load().catch((err) => {
            console.warn("restore editor:", err);
            tab.remove(pane.id);
          }));
        } else {
          const pane = this.makeTerminal(tab, { cwd: sp.cwd, replay: sp.replay, visible: false });
          if (sp.claude) pane.setNotice(this.claudeNotice(pane, sp.claude));
          map.set(sp.id, pane.id);
          panes.push(pane);
          starts.push(pane.start());
        }
      }
      const tree = remapTree(st.tree, map);
      if (!tree || panes.length === 0) continue;
      this.tabs.push(tab);
      // Attach without activating: tabs come up hidden (no GPU renderer yet) and the caller
      // picks the one to show; the tab bar is refreshed once at the end.
      if (!tab.element.parentElement) this.content.appendChild(tab.element);
      tab.restore(tree, panes, st.focused ? map.get(st.focused) ?? null : null);
      // One broken pane must not stop the remaining tabs from opening (and must never make
      // the next auto-save forget them).
      const results = await Promise.allSettled(starts);
      for (const r of results) if (r.status === "rejected") console.warn("restore pane:", r.reason);
      opened.push(tab);
    }
    if (this.active) this.refreshChrome(this.active);
    this.scheduleSave();
    return opened;
  }

  // ---- Claude Code --------------------------------------------------------

  /** The Claude Code session running in a pane, in the shape the session file keeps. */
  private claudeOf(p: TerminalPane): SavedClaude | undefined {
    const s = this.agents.forPane(p.id);
    if (!s) return undefined;
    return {
      title: s.title || s.message || "Claude Code",
      sessionId: s.session_id || undefined,
      cwd: s.cwd || undefined,
      model: s.context?.model || undefined,
      contextPercent: s.context_percent || undefined,
    };
  }

  /** The block a restored pane shows where its Claude session was: one click brings it back. */
  private claudeNotice(pane: TerminalPane, c: SavedClaude): PaneNotice {
    const cmd = () => this.config.claude.command || "claude";
    return {
      title: c.title,
      detail: [c.cwd ? shortPath(c.cwd) : "", c.model ?? "", c.contextPercent ? `context ${c.contextPercent} %` : ""].filter(Boolean).join("  ·  "),
      action: c.sessionId ? "Resume" : "Continue",
      hint: c.sessionId ? `${cmd()} --resume ${c.sessionId}` : `${cmd()} --continue`,
      onActivate: () => {
        pane.runCommand(c.sessionId ? `${cmd()} --resume ${c.sessionId}` : `${cmd()} --continue`);
        pane.focus();
      },
    };
  }

  /** Backend event: a session changed. */
  onAgentStatus(s: Session) {
    this.agents.apply(s);
  }

  private onAgentsChanged() {
    this.tabBar.render(this.tabs, this.active);
    for (const t of this.tabs) {
      for (const p of t.panes.values()) {
        const s = this.agents.forPane(p.id);
        p.element.classList.toggle("agent-waiting", s?.status === "waiting");
        p.element.classList.toggle("agent-done", s?.status === "done");
        if (p instanceof TerminalPane) {
          updateBadge(p.element, s, {
            session: () => this.agents.forPane(p.id),
            allSessions: () => {
              closeMenu();
              this.sidebar.toggle(true);
            },
          });
        }
      }
    }
    // Which panes hold which session belongs in the saved state, so a restore can offer them
    // again even if the save on quit does not get through.
    const sig = this.agents
      .list()
      .map((s) => `${s.pane_id}:${s.session_id}:${s.title}`)
      .sort()
      .join("|");
    if (sig !== this.claudeSig) {
      this.claudeSig = sig;
      this.scheduleSave();
    }
  }

  /** Signature of the live Claude sessions, to notice when the set changes. */
  private claudeSig = "";

  private reportFocus() {
    AgentService.SetFocus(this.active?.focusedId ?? "", document.hasFocus()).catch(() => {});
  }

  async launchClaude(): Promise<void> {
    const tab = this.active ?? (await this.newTab());
    const cmd = this.config.claude.command || "claude";
    await this.addTerminal(tab, "row", { command: cmd.split(/\s+/) });
  }

  private jumpToSession(s: Session) {
    const tab = this.tabs.find((t) => t.id === s.tab_id) ?? this.tabs.find((t) => t.panes.has(s.pane_id));
    if (!tab) return;
    this.activate(tab);
    if (tab.panes.has(s.pane_id)) {
      tab.setFocus(s.pane_id);
      tab.focusPane();
    }
  }

  async applyConfig(config: Config) {
    this.config = config;
    this.keymap = new Keymap(config.keys, config.general.passthrough ?? []);
    await this.loadTheme();
    for (const p of this.allPanes()) {
      if (p instanceof TerminalPane) p.applyConfig(config.terminal, this.fontDelta, this.termTheme());
      else if (p instanceof SettingsPane) p.applyConfig(config);
    }
  }

  /** Open (or focus) the settings pane in the active tab. */
  openSettings(section?: string) {
    // One settings pane per window: jump to it if it is already open somewhere.
    for (const t of this.tabs) {
      for (const p of t.panes.values()) {
        if (p instanceof SettingsPane) {
          this.activate(t);
          t.setFocus(p.id);
          p.focus();
          if (section) p.show(section);
          return;
        }
      }
    }
    // Split the active tab when it holds a single pane; a busy tab gets a dedicated one instead.
    const tab = this.active && this.active.panes.size <= 1 ? this.active : this.createTab();
    const pane: SettingsPane = new SettingsPane({
      paneId: nextId("pane"),
      config: this.config,
      configPath: this.configPath,
      keyLabels: {},
      onOpenConfigFile: () => void this.openEditor(tab, this.configPath),
      onClose: () => this.removePane(tab, pane),
      onImportTabs: (tabs) => this.importTabs(tabs),
    });
    tab.add(pane, "row");
    pane.focus();
    if (section) pane.show(section);
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
    const tab = this.createTab();
    await this.addTerminal(tab, "row", opts);
    return tab;
  }

  /** Create, register and activate an empty tab. */
  private createTab(): Tab {
    const tab = new Tab();
    tab.onChange = () => {
      this.refreshChrome(tab);
      if (tab === this.active) this.reportFocus();
      this.scheduleSave();
    };
    tab.onLayout = (r) => this.sidebar.cutEdge(tab === this.active ? r : null);
    this.tabs.push(tab);
    this.activate(tab);
    return tab;
  }

  activate(tab: Tab) {
    if (this.active === tab) return;
    this.scheduleSave();
    this.active?.element.classList.remove("active");
    for (const p of this.active?.panes.values() ?? []) p.setVisible?.(false);
    this.active = tab;
    tab.element.classList.add("active");
    if (!tab.element.parentElement) this.content.appendChild(tab.element);
    for (const p of tab.panes.values()) p.setVisible?.(true);
    tab.render();
    this.refreshChrome(tab);
    tab.focusPane();
  }

  closeTab(tab: Tab) {
    const i = this.tabs.indexOf(tab);
    if (i < 0) return;
    this.tabs.splice(i, 1);
    tab.dispose();
    this.scheduleSave();
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
    if (this.active && (!tab || tab === this.active)) void Window.SetTitle(`${this.active.title} — wate`).catch(() => {});
  }

  // ---- panes ------------------------------------------------------------

  private makeTerminal(tab: Tab, opts: { cwd?: string; command?: string[]; replay?: string; visible?: boolean }): TerminalPane {
    const pane: TerminalPane = new TerminalPane({
      paneId: nextId("pane"),
      tabId: tab.id,
      cwd: opts.cwd ?? "",
      command: opts.command,
      replay: opts.replay,
      visible: opts.visible,
      terminal: this.config.terminal,
      theme: this.termTheme(),
      fontDelta: this.fontDelta,
      keyFilter: (e) => this.keymap.match(e) === null,
      onExit: () => this.removePane(tab, pane),
      onTitle: () => tab.onChange?.(),
      onOpenFile: (t) => this.openTarget(tab, t),
    });
    return pane;
  }

  private async addTerminal(tab: Tab, dir: Dir, opts: { cwd?: string; command?: string[] } = {}): Promise<TerminalPane> {
    const cwd = opts.cwd ?? (await tab.focused?.cwd().catch(() => "")) ?? "";
    const pane = this.makeTerminal(tab, { ...opts, cwd });
    tab.add(pane, dir);
    this.scheduleSave();
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
    const pane = this.makeEditor(tab, path, line, col);
    try {
      tab.add(pane, "row");
      this.scheduleSave();
      await pane.load();
      pane.focus();
    } catch (err) {
      console.warn("editor:", err);
      tab.remove(pane.id);
      tab.focusPane();
    }
  }

  private makeEditor(tab: Tab, path: string, line?: number, col?: number): EditorPane {
    const pane: EditorPane = new EditorPane({
      paneId: nextId("pane"),
      path,
      line,
      col,
      editor: this.config.editor,
      onTitle: () => tab.onChange?.(),
      onClose: () => void this.closePane(tab, pane),
      onLink: (href, fromDir) => this.openPreviewLink(tab, href, fromDir),
    });
    return pane;
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
    AgentService.Forget(pane.id).catch(() => {});
    tab.remove(pane.id);
    if (tab.isEmpty) this.closeTab(tab);
    else tab.focusPane();
    this.scheduleSave();
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
    if (action === "__debug") {
      this.debugDump();
      return;
    }
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
      case "swap_left":
      case "swap_right":
      case "swap_up":
      case "swap_down":
        tab?.swapDir(action.slice("swap_".length) as Direction);
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
      case "launch_claude":
        await this.launchClaude();
        break;
      case "toggle_sidebar":
        this.sidebar.toggle();
        break;
      case "open_settings":
        this.openSettings();
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

  /** Rendering/state summary for `wate ctl debug`; ends up in the Go log. */
  private debugDump() {
    const cs = getComputedStyle(document.body);
    const root = document.documentElement;
    const panes = this.allPanes().map((p) => {
      const r = p.element.getBoundingClientRect();
      const term = p instanceof TerminalPane ? p.term : null;
      return {
        id: p.id,
        kind: p.kind,
        rect: [Math.round(r.x), Math.round(r.y), Math.round(r.width), Math.round(r.height)],
        canvases: p.element.querySelectorAll("canvas").length,
        rowsWidth: p.element.querySelector(".xterm-rows")?.clientWidth,
        rowOverflow: Math.max(0, ...Array.from(p.element.querySelectorAll(".xterm-rows > div")).map((d) => d.scrollWidth - d.clientWidth)),
        cols: term?.cols,
        rows: term?.rows,
        buffer: term ? { type: term.buffer.active.type, baseY: term.buffer.active.baseY, cursorY: term.buffer.active.cursorY, length: term.buffer.active.length } : undefined,
        line0: term?.buffer.active.getLine(0)?.translateToString(true).slice(0, 60),
      };
    });
    console.warn("[debug]", {
      theme: this.theme?.id,
      bgMode: root.dataset.bgMode,
      bodyBg: cs.backgroundColor,
      vars: { bg: root.style.getPropertyValue("--bg"), surface: root.style.getPropertyValue("--surface") },
      size: [window.innerWidth, window.innerHeight],
      visibility: document.visibilityState,
      tabs: this.tabs.length,
      panes,
    });
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
