import { Clipboard, Window } from "@wailsio/runtime";

import { AccessService, AgentService, ConfigService, OpenerService, PaneBridge, RestartService, PtyService, SessionService, StateService, ThemeService, WindowService, type Access, type AccessState, type Config, type Resolved, type Session, type Target } from "./api";
import { fromImported, parseWindow, remapTree, type ImportedTab, type SavedClaude, type SavedPane, type SavedTab, type SavedWindow } from "./session";
import { AgentStore } from "./agent/store";
import { updateBadge } from "./agent/badge";
import { closeMenu, type MenuEntry } from "./ui/menu";
import { showConfirm } from "./ui/confirm";
import { Sidebar, shortPath } from "./sidebar/sidebar";
import { applyTheme, xtermTheme } from "./theme/apply";
import { Keymap } from "./keymap/keymap";
import { perf, takePerf } from "./perf";
import { keys, logKey, takeKeys } from "./keys-debug";
import { boxAt, dropText, dropZone, droppedPaths, shellCommand, zoneRect, zoneSplit, type Box, type DropZone } from "./drop";
import { DropHighlight, installDragMotion } from "./drop-overlay";
import { neighbor, type Dir, type Direction } from "./layout/tree";
import { moveItem } from "./tabs/order";
import type { Pane } from "./pane";
import { Tab, nextId } from "./tabs/tab";
import { TabBar } from "./tabs/tabbar";
import { TerminalPane, type PaneNotice } from "./terminal/pane";
import { statusView, type StatusHost } from "./terminal/status";
import { EditorPane } from "./editor/pane";
import { SettingsPane } from "./settings/pane";

/** Is the event inside a CodeMirror editor? Then the editor owns the drop, not us. */
function inEditor(target: EventTarget | null): boolean {
  return !!(target as HTMLElement | null)?.closest?.(".cm-editor");
}

/** Where a new pane goes: which pane is split, in which direction, and on which side of it. */
export interface SplitAt {
  dir?: Dir;
  target?: string | null;
  before?: boolean;
  /** Share of the divider the *new* pane should get (0..1); the default is an even split. */
  ratio?: number;
}

/** Top-level UI state: tabs, panes, keybindings and the actions they trigger. */
/** Scrollback kept per pane (bytes of ANSI text): named sessions keep more than the auto-restore file. */
const SESSION_SCROLLBACK = 256 * 1024;

/** How long a dragged pane has to rest on a tab button before that tab comes forward. */
const SPRING_MS = 400;
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
      requestRender: () => this.refreshChrome(),
      moveTab: (t, to) => this.moveTab(t, to),
      otherWindows: async () => {
        const all = (await WindowService.Windows().catch(() => [])) ?? [];
        return all.filter((w) => w.id !== this.windowId).map((w) => ({ id: w.id, index: w.index }));
      },
      moveTabToWindow: (t, to) => void this.moveTabToWindow(t, to),
      dropTabOutside: (t, x, y) => void this.dropTabOutside(t, x, y),
      prepareRestart: () => void this.prepareRestart(),
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
      restartNow: () => void RestartService.Proceed(true),
      restartCancel: () => void RestartService.Cancel().catch((err) => console.warn("restart:", err)),
      launchLabel: this.keymap.label("launch_claude") || "Launch",
      toggleLabel: this.keymap.label("toggle_sidebar"),
      onVisibility: () => this.active?.render(),
      sendToPane: (paneId, text) => void PtyService.WriteToPane(paneId, text).catch((err) => console.warn("write to pane:", err)),
      resume: (sessionId, cwd) => void this.resumeClaude(sessionId, cwd),
      windowId: () => this.windowId,
    });
    this.content.className = "content";
    this.main.className = "main";
    this.main.append(this.content, this.sidebar.element);
    root.append(this.tabBar.element, this.main);
    window.addEventListener("keydown", (e) => this.onKey(e), { capture: true });
    // A dropped file would otherwise navigate the WebView to file:///… — the image then covers
    // every pane and only a restart brings them back. So wate takes the drop itself (see drop.ts).
    for (const type of ["dragenter", "dragover"] as const) {
      window.addEventListener(type, (e) => {
        if (inEditor(e.target)) return;
        e.preventDefault();
        if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
      }, { capture: true });
    }
    window.addEventListener("drop", (e) => this.onDrop(e), { capture: true });
    this.drops = new DropHighlight(root);
    installDragMotion({ move: (x, y) => this.previewDrop(x, y), leave: () => this.endDrag() });
    window.addEventListener("focus", () => this.reportFocus());
    window.addEventListener("blur", () => this.reportFocus());
    this.agents.subscribe(() => this.onAgentsChanged());
    AgentService.List().then((l) => this.agents.replaceAll(l ?? [])).catch(() => {});
    window.addEventListener("beforeunload", () => void this.saveSession(true));
  }

  // ---- session persistence ------------------------------------------------

  /** Debounced: called after every structural change. */
  /** Set for windows opened while another wate runs: they neither restore nor save the session. */
  /** This window's name, as Wails and the backend know it. */
  windowId = "";
  /** "session" restores this window's saved tabs; "empty" starts clean. */
  restoreMode = "session";
  /** Whether this window writes its tabs back — separate from whether it restored any. */
  persist = true;
  /** This window's slot in session.json and window.json; stable across runs. */
  stateKey = "w-1";

  private saveDeadline = 0;

  /** The drop preview, and when the native path last delivered one (the DOM fallback defers to it). */
  private drops?: DropHighlight;
  private lastNativeDrop = 0;
  /** Pane geometry for the drag in progress: measuring it per motion event would make the whole
   *  window (terminal included) lay out again several times a second. */
  private dragRects?: (Box & { id: string })[];
  /** What the last drop resolved to — `wate ctl debug` prints it. */
  private lastDrop?: { paths: string[]; x: number; y: number; pane: string | null; zone: DropZone };

  scheduleSave() {
    if (this.restoring || !this.persist || !this.config.general.restore_session) return;
    const now = Date.now();
    // Debounced, but with a ceiling: a busy pane would otherwise push the save out forever.
    if (this.saveTimer && now > this.saveDeadline) return;
    if (!this.saveTimer) this.saveDeadline = now + 5000;
    clearTimeout(this.saveTimer);
    this.saveTimer = setTimeout(() => {
      this.saveTimer = undefined;
      void this.saveSession();
    }, 800);
  }

  async saveSession(immediate = false): Promise<void> {
    if (this.restoring || !this.persist || !this.config.general.restore_session) return;
    perf.saves++;
    const state = await this.snapshot(immediate, RESTORE_SCROLLBACK);
    await StateService.Save(JSON.stringify(state)).catch((err) => console.warn("session save:", err));
  }

  /** Serialise this window's tabs (layout, cwds, open files, titles, colours, and — with
   *  scrollback — the terminal text). Each window saves only itself; the backend keeps the
   *  slots apart. */
  async snapshot(immediate = false, scrollback = 0): Promise<SavedWindow> {
    const tabs: SavedTab[] = [];
    for (const t of this.tabs) {
      if (!t.tree) continue;
      const panes: SavedPane[] = [];
      for (const p of t.panes.values()) {
        if (p instanceof EditorPane) panes.push({ id: p.id, kind: "editor", path: p.path });
        else if (p instanceof TerminalPane) {
          // lastKnownCwd is what OSC 7 reported; only ask the backend when the shell stayed quiet.
          const cwd = p.lastKnownCwd() || (immediate ? "" : await p.cwd().catch(() => ""));
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
    return { key: this.stateKey, active: this.active ? this.tabs.indexOf(this.active) : 0, tabs };
  }

  /** Store the current tabs under a name (tab bar dropdown). */
  async saveNamedSession(name: string): Promise<void> {
    try {
      const w = await this.snapshot(false, SESSION_SCROLLBACK);
      // Named sessions stay on the v1 shape: Go counts .tabs[].panes to show "3 tabs, 5 panes".
      await SessionService.Save(name, JSON.stringify({ version: 1, active: w.active, tabs: w.tabs }));
    } catch (err) {
      console.warn("save session:", err);
    }
  }

  /** Open a named session: its tabs are added after the current ones. */
  async openNamedSession(id: string): Promise<void> {
    try {
      const saved = parseWindow(await SessionService.Load(id));
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
    if (this.restoreMode !== "session" || !this.config.general.restore_session) return false;
    const saved = parseWindow(await StateService.Load().catch(() => ""));
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
    tab.onPaneDragMove = (paneId, x, y) => this.paneDragMove(tab, paneId, x, y);
    tab.onPaneDragDrop = (paneId, x, y) => void this.paneDragDrop(tab, paneId, x, y);
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
      // The hint is the short form; what actually runs carries the flags as well, and the
      // inline MCP definition among them is far too long to put in front of anybody.
      hint: c.sessionId ? `${cmd()} --resume ${c.sessionId}` : `${cmd()} --continue`,
      onActivate: () => {
        void (async () => {
          // Through ClaudeArgs, not assembled here: a session brought back this way has to
          // come back with the same flags one wate starts itself, or it returns without the
          // pane tools and cannot answer a restart with anything but the shell.
          const args = (await ConfigService.ClaudeArgs(c.sessionId ?? "").catch(() => null)) ?? [cmd()];
          pane.runCommand(shellCommand(c.sessionId ? args : [...args, "--continue"]));
          pane.focus();
        })();
      },
    };
  }

  /** Backend event: a session changed. */
  onAgentStatus(s: Session) {
    this.agents.apply(s);
  }

  /** Last status painted per pane, so an unchanged one costs no DOM work. */
  private paneStatus = new Map<string, string>();

  /** The tab button a dragged pane is resting on, and since when. */
  private springTab: { id: string; at: number } | null = null;

  /** What the backend says about each pane: who opened it, what it has been opened up to. */
  private access = new Map<string, AccessState>();

  /** Fold a pane's standing into its status bar. The bar itself skips an unchanged repaint. */
  private refreshBars() {
    const none: Access = { read: false, write: false, manage: false };
    for (const t of this.tabs) {
      for (const p of t.panes.values()) {
        if (!(p instanceof TerminalPane)) continue;
        const st = this.access.get(p.id);
        p.bar.update(
          statusView({
            openedByAgent: !!st?.owner,
            agentStatus: this.agents.forPane(p.id)?.status,
            command: p.running,
            access: st?.access ?? none,
          }),
        );
      }
    }
  }

  /**
   * The window was asked to close while agents were still working in it.
   *
   * Closing it would end them mid-thought, so the close was called off and this asks instead.
   * "Wait" hands over to the restart round, which is the same question put to the agents
   * rather than to the user.
   */
  onConfirmClose(req: { agents: number }) {
    const n = req.agents;
    showConfirm(
      `${n} ${n === 1 ? "agent is" : "agents are"} still running`,
      "Closing this window ends their sessions. You can ask them whether now is a good moment.",
      [
        { label: "Cancel", onSelect: () => {} },
        { label: "Wait", onSelect: () => void this.prepareRestart(false) },
        {
          label: "Quit",
          danger: true,
          onSelect: () => void RestartService.AllowClose().then(() => Window.Close()),
        },
      ],
    );
  }

  /** A restart round opened, changed or ended. */
  onRestartChanged(st: Parameters<typeof this.sidebar.setRestart>[0]) {
    // The panel is where the round is shown, so it has to be open to be of any use.
    if (st?.pending) this.sidebar.toggle(true);
    this.sidebar.setRestart(st);
  }

  /**
   * Ask every running session whether wate may restart.
   *
   * `relaunch` is what the user asked for in the first place: the Sessions menu means restart,
   * the close dialog's "Wait" means close. The countdown that runs once everyone has agreed
   * carries out whichever it was.
   */
  async prepareRestart(relaunch = true): Promise<void> {
    await RestartService.Announce("", this.config.claude.restart_deadline || 5, relaunch).catch((err) => console.warn("restart:", err));
  }

  /** A pane's standing changed in the backend (a grant, or an agent opening a pane). */
  onAccessChanged(state: AccessState) {
    this.access.set(state.pane, state);
    this.refreshBars();
  }

  private statusHost(): StatusHost {
    return {
      access: (id) => this.access.get(id)?.access ?? { read: false, write: false, manage: false },
      setAccess: (id, a) => void AccessService.Grant(id, a).catch((err) => console.warn("grant:", err)),
      closePane: (id) => {
        const tab = this.tabOfPane(id);
        const pane = this.paneById(id);
        if (tab && pane) this.closePane(tab, pane);
      },
      paneEntries: (id) => {
        const from = this.tabOfPane(id);
        const entries: MenuEntry[] = [];
        if (from && from.panes.size > 1) entries.push({ label: "Move to new tab", onSelect: () => void this.movePaneToNewTab(id) });
        this.tabs.forEach((t, i) => {
          if (t !== from) entries.push({ label: `Move to tab ${i + 1}`, hint: t.title, onSelect: () => void this.movePane(id, t) });
        });
        return entries;
      },
    };
  }

  private onAgentsChanged() {
    this.tabBar.render(this.tabs, this.active);
    for (const t of this.tabs) {
      for (const p of t.panes.values()) {
        const s = this.agents.forPane(p.id);
        const status = s ? `${s.status}|${s.context_percent}|${s.title}` : "";
        if (this.paneStatus.get(p.id) === status) continue;
        this.paneStatus.set(p.id, status);
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
    this.refreshBars();
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

  private lastFocusReport = "";

  private reportFocus() {
    const pane = this.active?.focusedId ?? "";
    const key = `${pane}|${document.hasFocus()}`;
    // The backend answers a focus report with an agent:status event, which repaints the
    // chrome — so only report when it actually says something new.
    if (key === this.lastFocusReport) return;
    this.lastFocusReport = key;
    AgentService.SetFocus(pane, document.hasFocus()).catch(() => {});
  }

  async launchClaude(): Promise<void> {
    const tab = this.active ?? (await this.newTab());
    await this.addTerminal(tab, "row", { command: (await ConfigService.ClaudeArgs("")) ?? undefined });
  }

  // ---- moving a tab between windows -------------------------------------

  /** Serialise one tab, keeping its pane ids: the receiving window re-attaches by them. */
  private async snapshotTab(tab: Tab): Promise<SavedTab | null> {
    const whole = await this.snapshot(false, SESSION_SCROLLBACK);
    return whole.tabs.find((t) => t.panes.some((p) => tab.panes.has(p.id))) ?? null;
  }

  /**
   * Hand a tab to another window (or to a new one, when `to` is empty). The tab is only let
   * go once the other side confirms it has it — see window:release-tab.
   */
  async moveTabToWindow(tab: Tab, to: string, index = -1): Promise<void> {
    const saved = await this.snapshotTab(tab);
    if (!saved) return;
    const live: Record<string, string> = {};
    for (const p of tab.panes.values()) {
      if (p instanceof TerminalPane && p.session) live[p.id] = p.session;
    }
    try {
      await WindowService.TransferTab({ to, index, tab_id: tab.id, tab: saved as unknown as Record<string, unknown>, live });
    } catch (err) {
      console.warn("move tab:", err);
    }
  }

  /**
   * A tab let go outside this window: onto another window, or onto nothing — which is how a
   * tab becomes its own window.
   */
  async dropTabOutside(tab: Tab, x: number, y: number): Promise<void> {
    let target = "";
    try {
      target = (await WindowService.WindowAt(Math.round(x), Math.round(y))) ?? "";
    } catch (err) {
      console.warn("window at:", err);
    }
    // Over another window: it takes the tab. Anywhere else — the desktop, or still over this
    // window, which is where the gesture ends when you simply pull a tab off the bar — the
    // tab gets a window of its own.
    await this.moveTabToWindow(tab, target === this.windowId ? "" : target);
  }

  /** The other window has the tab now: drop ours without killing the shells. */
  releaseTab(tabId: string) {
    const i = this.tabs.findIndex((t) => t.id === tabId);
    if (i < 0) return;
    const tab = this.tabs[i];
    this.tabs.splice(i, 1);
    tab.detach();
    if (this.active === tab) {
      this.active = null;
      const next = this.tabs[Math.min(i, this.tabs.length - 1)];
      if (next) this.activate(next);
      else void Window.Close();
    }
    this.refreshChrome();
    this.scheduleSave();
  }

  /** Take in a tab handed over by another window, re-attaching to its running shells. */
  async adoptTab(t: { tab: SavedTab; live: Record<string, string>; index: number; tab_id: string }): Promise<void> {
    const st = t.tab;
    const tab = new Tab();
    tab.customTitle = st.title ?? "";
    tab.color = st.color ?? "";
    tab.onChange = () => {
      this.refreshChrome(tab);
      this.scheduleSave();
    };
    tab.onLayout = (focused) => this.sidebar.cutEdge(focused);
    const panes = new Map<string, Pane>();
    for (const sp of st.panes) {
      if (sp.kind === "editor") {
        const ed = this.makeEditor(tab, sp.path);
        panes.set(sp.id, ed);
        continue;
      }
      // Same pane id, adopt: the shell is still running and keeps its own WATE_PANE_ID.
      // The replay carries the visible history across; the live shell then continues into it.
      // A line or two can fall in the seam — what the old pane received between the snapshot
      // and the handover — which is a far better trade than arriving with a blank screen.
      const adopt = !!t.live[sp.id];
      const pane = this.makeTerminal(tab, { id: sp.id, adopt, cwd: sp.cwd, replay: sp.replay, visible: true });
      panes.set(sp.id, pane);
    }
    const at = t.index < 0 || t.index > this.tabs.length ? this.tabs.length : t.index;
    this.tabs.splice(at, 0, tab);
    this.content.appendChild(tab.element);
    tab.restore(st.tree, [...panes.values()], st.focused);
    await Promise.allSettled([...panes.values()].map((p) => ("start" in p ? (p as { start(): Promise<void> }).start() : Promise.resolve())));
    for (const [id] of panes) PtyService.Retab(id, tab.id).catch(() => {});
    this.activate(tab);
    this.refreshChrome();
    this.scheduleSave();
    WindowService.TabAdopted(t.tab_id).catch((err) => console.warn("confirm adopt:", err));
  }

  /** Reopen an ended session where it left off: `claude --resume <id>` in its old directory. */
  async resumeClaude(sessionId: string, cwd: string): Promise<void> {
    const tab = this.active ?? (await this.newTab());
    await this.addTerminal(tab, "row", { cwd: cwd || undefined, command: (await ConfigService.ClaudeArgs(sessionId)) ?? undefined });
  }

  private jumpToSession(s: Session) {
    const tab = this.tabs.find((t) => t.id === s.tab_id) ?? this.tabs.find((t) => t.panes.has(s.pane_id));
    if (!tab) {
      // The sidebar lists every window's sessions, so this one may well live elsewhere.
      if (s.window_id && s.window_id !== this.windowId) {
        void WindowService.FocusPane(s.window_id, s.tab_id, s.pane_id).catch((err) => console.warn("focus pane:", err));
      }
      return;
    }
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

  /**
   * Answer a question the backend asked about one pane.
   *
   * Everything else the backend sends is a command and needs no reply; these are the few
   * things only the frontend knows, because the terminal's screen lives in xterm.js. The id
   * goes back with the answer so the waiting caller can be matched up again.
   */
  async handlePaneRequest(req: { id: string; kind: string; pane: string; data: string }): Promise<void> {
    let data = "";
    let err = "";
    try {
      data = await this.answerPaneRequest(req);
    } catch (e) {
      err = e instanceof Error ? e.message : String(e);
    }
    try {
      await PaneBridge.Reply(req.id, data, err);
    } catch (e) {
      // Nothing left to do: whoever asked is waiting on a timeout that will fire shortly.
      console.warn("pane reply:", e);
    }
  }

  private async answerPaneRequest(req: { kind: string; pane: string; data: string }): Promise<string> {
    const pane = this.paneById(req.pane);
    const tab = this.tabOfPane(req.pane);
    if (!pane || !tab) throw new Error(`no such pane ${req.pane}`);
    const args = JSON.parse(req.data || "{}") as { lines?: number; dir?: Dir; ratio?: number };
    switch (req.kind) {
      case "read": {
        if (!(pane instanceof TerminalPane)) throw new Error(`pane ${req.pane} is not a terminal`);
        return pane.snapshot(args.lines || undefined);
      }
      case "split": {
        const dir: Dir = args.dir === "col" ? "col" : "row";
        const made = await this.addTerminal(tab, dir, {}, { target: req.pane, ratio: args.ratio || undefined });
        return made.id;
      }
      case "ratio": {
        const dir: Dir = args.dir === "col" ? "col" : "row";
        if (!args.ratio) throw new Error("ratio must say how much");
        if (!tab.setSplitRatio(req.pane, dir, args.ratio)) throw new Error(`pane ${req.pane} has no ${dir} divider next to it`);
        return "";
      }
      case "move": {
        const to = String((args as { to?: string }).to ?? "new");
        if (to === "new" || to === "") {
          if (!(await this.movePaneToNewTab(req.pane))) throw new Error("a pane on its own is already a tab");
          return "";
        }
        const n = Number(to);
        const dest = this.tabs[n - 1];
        if (!dest) throw new Error(`there is no tab ${to}`);
        if (!(await this.movePane(req.pane, dest))) throw new Error("that pane is already there");
        return "";
      }
      case "close":
        this.closePane(tab, pane);
        return "";
      case "focus":
        this.activate(tab);
        tab.setFocus(req.pane);
        return "";
      case "list": {
        const rows = await Promise.all(
          [...tab.panes.values()].map(async (p) => ({
            id: p.id,
            kind: p.kind,
            focused: p.id === tab.focusedId,
            cwd: await p.cwd().catch(() => ""),
            command: p instanceof TerminalPane ? p.running : "",
          })),
        );
        return JSON.stringify(rows);
      }
      default:
        throw new Error(`unknown pane request ${req.kind}`);
    }
  }

  /**
   * Move a pane into another tab of this window, shell and scrollback intact.
   *
   * Nothing is rebuilt: a pane is a JavaScript object with a WebSocket and an xterm instance,
   * and only its parent element changes. The backend is told which tab it is in now, because
   * that is what decides who may reach it — the WATE_TAB_ID frozen into its shell does not
   * move with it and must not be believed (see PtyService.Retab).
   */
  async movePane(paneId: string, to: Tab, at?: SplitAt): Promise<boolean> {
    const from = this.tabOfPane(paneId);
    if (!from || from === to) return false;
    const pane = from.detachPane(paneId);
    if (!pane) return false;
    if (from.panes.size === 0) this.closeTab(from);
    to.add(pane, at?.dir ?? "row", at?.target ?? to.focusedId, at?.before ?? false);
    if (at?.ratio !== undefined) to.setSplitRatio(pane.id, at.dir ?? "row", at.ratio);
    await PtyService.Retab(pane.id, to.id).catch((err) => console.warn("retab:", err));
    this.activate(to);
    to.setFocus(pane.id);
    pane.relayout();
    this.refreshChrome();
    this.scheduleSave();
    return true;
  }

  /** Move a pane out into a tab of its own. */
  async movePaneToNewTab(paneId: string): Promise<boolean> {
    const from = this.tabOfPane(paneId);
    if (!from || from.panes.size < 2) return false;
    return this.movePane(paneId, this.createTab());
  }

  /**
   * A pane is being dragged: does the app want this point rather than the tab it came from?
   *
   * Yes when the pointer is over a tab button (which is a destination), and yes once another
   * tab is in front, because then the panes under the pointer are not the source tab's any
   * more. Resting on a tab button brings that tab forward, so a pane can be aimed at a
   * particular spot in it rather than only dumped in.
   */
  private paneDragMove(from: Tab, paneId: string, x: number, y: number): boolean {
    const overId = this.tabBar.tabIdAt(x, y);
    if (overId) {
      this.tabBar.showPaneTarget(overId === from.id && this.active === from ? null : overId);
      this.drops?.hide();
      const over = this.tabs.find((t) => t.id === overId);
      if (over && over !== this.active) {
        const now = performance.now();
        if (this.springTab?.id !== overId) this.springTab = { id: overId, at: now };
        else if (now - this.springTab.at >= SPRING_MS) {
          this.springTab = null;
          this.activate(over);
        }
      } else {
        this.springTab = null;
      }
      return true;
    }
    this.springTab = null;
    this.tabBar.showPaneTarget(null);
    if (this.active === from) {
      this.drops?.hide();
      return false;
    }
    // Over another tab's panes: show where it would land there, since the source tab's own
    // preview cannot speak for a layout it is not part of.
    const dest = this.active;
    const box = dest ? boxAt(dest.rects(), x, y) : undefined;
    if (box && box.id !== paneId) this.drops?.show(zoneRect(box, dropZone(box, x, y)), dropZone(box, x, y));
    else this.drops?.hide();
    return true;
  }

  /** The pane was let go somewhere the source tab does not own. */
  private async paneDragDrop(from: Tab, paneId: string, x: number, y: number): Promise<void> {
    this.springTab = null;
    this.tabBar.showPaneTarget(null);
    this.drops?.hide();
    const overId = this.tabBar.tabIdAt(x, y);
    const onButton = overId ? this.tabs.find((t) => t.id === overId) : undefined;
    const dest = onButton && onButton !== from ? onButton : this.active !== from ? this.active : null;
    if (!dest) return;
    let at: SplitAt | undefined;
    const box = boxAt(dest.rects(), x, y);
    if (box && box.id !== paneId) {
      const zone = dropZone(box, x, y);
      const split = zoneSplit(zone);
      if (split) at = { target: box.id, dir: split.dir, before: split.before };
    }
    await this.movePane(paneId, dest, at);
  }

  /** The tab of this window that holds this pane, if any. */
  private tabOfPane(id: string): Tab | undefined {
    return this.tabs.find((t) => t.panes.has(id));
  }

  /** The pane with this id, in whichever tab of this window holds it. */
  private paneById(id: string): Pane | undefined {
    for (const t of this.tabs) {
      const p = t.panes.get(id);
      if (p) return p;
    }
    return undefined;
  }

  /** Put `tab` at index `to`. The saved session stores tabs in bar order, so the reorder has
   *  to land before the next snapshot — hence refresh first, save second. */
  moveTab(tab: Tab, to: number) {
    const from = this.tabs.indexOf(tab);
    const next = moveItem(this.tabs, from, to);
    if (next === this.tabs) {
      this.refreshChrome();
      return;
    }
    this.tabs.splice(0, this.tabs.length, ...next);
    this.refreshChrome();
    this.scheduleSave();
  }

  /** The focused pane's directory, so a new window opens where this one is looking. */
  private async activeCwd(): Promise<string> {
    return (await this.active?.focused?.cwd().catch(() => "")) ?? "";
  }

  /** Shift the active tab one place along the bar (move_tab_left / move_tab_right). */
  private nudgeTab(delta: number) {
    const tab = this.active;
    if (!tab) return;
    this.moveTab(tab, this.tabs.indexOf(tab) + delta);
  }

  private lastWindowTitle = "";
  private lastTabReport = "";

  /**
   * Tell the backend which tabs and panes this window holds. It is the only place that knows,
   * and the backend needs it to route a ctl request or a hook to the right window.
   */
  private reportTabs() {
    const tabs = this.tabs.map((t) => t.id);
    const panes = this.tabs.flatMap((t) => [...t.panes.keys()]);
    const sig = tabs.join(",") + "|" + panes.join(",");
    if (sig === this.lastTabReport) return;
    this.lastTabReport = sig;
    void WindowService.SetTabs({ tabs, panes }).catch((err) => console.warn("report tabs:", err));
  }

  /** Bring a tab (and optionally one of its panes) to the front — see window:activate-tab. */
  activateTab(tabId: string, paneId?: string) {
    const tab = this.tabs.find((t) => t.id === tabId);
    if (!tab) return;
    this.activate(tab);
    if (paneId && tab.panes.has(paneId)) {
      tab.setFocus(paneId);
      tab.focusPane();
    }
  }

  private refreshChrome(tab?: Tab) {
    this.tabBar.render(this.tabs, this.active);
    this.reportTabs();
    // Panes come and go here, and a pane with no agent never reaches the agent tick.
    this.refreshBars();
    if (!this.active || (tab && tab !== this.active)) return;
    const title = `${this.active.title} — wate`;
    if (title === this.lastWindowTitle) return;
    this.lastWindowTitle = title;
    void Window.SetTitle(title).catch(() => {});
  }

  // ---- panes ------------------------------------------------------------

  private makeTerminal(tab: Tab, opts: { cwd?: string; command?: string[]; replay?: string; visible?: boolean; id?: string; adopt?: boolean }): TerminalPane {
    const pane: TerminalPane = new TerminalPane({
      // An adopted pane keeps its id: that is how the backend finds the shell it already has,
      // and it is what the shell's own WATE_PANE_ID says.
      paneId: opts.id ?? nextId("pane"),
      adopt: opts.adopt,
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
      onBarDrag: (e) => tab.startPaneDrag(pane.id, e),
    });
    pane.bar.setHost(this.statusHost());
    return pane;
  }

  private async addTerminal(tab: Tab, dir: Dir, opts: { cwd?: string; command?: string[] } = {}, at?: SplitAt): Promise<TerminalPane> {
    // A new split starts where the pane it was asked for is looking — which is the focused one
    // from the keyboard, but the *asking* one when a request named it.
    const beside = (at?.target && tab.panes.get(at.target)) || tab.focused;
    const cwd = opts.cwd ?? (await beside?.cwd().catch(() => "")) ?? "";
    const pane = this.makeTerminal(tab, { ...opts, cwd });
    tab.add(pane, dir, at?.target ?? tab.focusedId, at?.before ?? false);
    // splitLeaf always halves; a requested size is applied to the pane that was just added.
    if (at?.ratio !== undefined) tab.setSplitRatio(pane.id, dir, at.ratio);
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

  /** Open (or focus) an editor for path; by default split to the right of the focused pane. */
  async openEditor(tab: Tab, path: string, line?: number, col?: number, at?: SplitAt): Promise<void> {
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
      tab.add(pane, at?.dir ?? "row", at?.target ?? tab.focusedId, at?.before ?? false);
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
    AccessService.Forget(pane.id).catch(() => {});
    this.access.delete(pane.id);
    this.paneStatus.delete(pane.id);
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

  /** The OS dropped files on the window (see internal/app/drop.go for why this comes from Go). */
  onFilesDropped(req: { paths: string[] | null; x: number; y: number }) {
    this.lastNativeDrop = Date.now();
    void this.handleDrop(req.paths ?? [], req.x, req.y);
  }

  /**
   * A drop that did reach the DOM. Only engines that hand over the paths get here (WebKitGTK does
   * not), so this is a fallback — and it stays quiet right after a native drop rather than acting
   * on the same files twice.
   */
  private onDrop(e: DragEvent) {
    // CodeMirror reads a dropped file into the buffer itself; everywhere else the default is
    // a navigation away from the app, so it never gets to run.
    if (inEditor(e.target)) return;
    e.preventDefault();
    const paths = droppedPaths(e.dataTransfer);
    if (paths.length === 0 || Date.now() - this.lastNativeDrop < 400) return;
    void this.handleDrop(paths, e.clientX, e.clientY);
  }

  /** Paint what the drop under the cursor would do. */
  private previewDrop(x: number, y: number) {
    this.dragRects ??= this.active?.rects();
    const hit = this.dragRects ? boxAt(this.dragRects, x, y) : undefined;
    if (!hit) {
      this.drops?.hide();
      return;
    }
    const zone = dropZone(hit, x, y);
    this.drops?.show(zoneRect(hit, zone), zone);
  }

  /** The drag is over (or gone): forget the measurements and take the preview down. */
  private endDrag() {
    this.dragRects = undefined;
    this.drops?.hide();
  }

  /**
   * One entry for every drop, wherever it came from: dropped in the middle of a pane the paths are
   * typed into it, dropped near an edge the pane splits towards that edge and the file opens there.
   */
  async handleDrop(paths: string[], x: number, y: number): Promise<void> {
    this.endDrag();
    const tab = this.active;
    if (!tab || paths.length === 0) return;
    const hit = boxAt<Box & { id: string }>(tab.rects(), x, y);
    const zone = hit ? dropZone(hit, x, y) : "center";
    this.lastDrop = { paths, x, y, pane: hit?.id ?? null, zone };
    // Next to every pane (the tab bar, the sidebar): treat it as a drop into the focused pane
    // rather than losing the files.
    const pane = (hit ? tab.panes.get(hit.id) : undefined) ?? tab.focused;
    if (!pane) return;
    if (pane.id !== tab.focusedId) {
      tab.setFocus(pane.id);
      tab.focusPane();
    }
    const split = hit ? zoneSplit(zone) : null;
    if (!split) {
      if (pane instanceof TerminalPane) pane.term.paste(dropText(paths));
      else if (pane instanceof EditorPane) pane.insertText(dropText(paths).trimEnd());
      pane.focus();
      return;
    }
    await this.openBeside(tab, pane.id, split, paths);
  }

  /**
   * The edge case: a new pane on the given side of `target`. A single text file opens in an editor,
   * a single directory becomes a shell that starts there, and everything else — an image, a PDF,
   * several files at once — becomes a shell with the paths already typed in.
   */
  private async openBeside(tab: Tab, target: string, split: { dir: Dir; before: boolean }, paths: string[]): Promise<void> {
    const at = { target, before: split.before };
    if (paths.length === 1) {
      const [t] = (await OpenerService.Resolve("", paths).catch(() => [])) ?? [];
      if (t?.kind === "dir") {
        await this.addTerminal(tab, split.dir, { cwd: t.path }, at);
        return;
      }
      if (t?.kind === "file" && t.text) {
        await this.openEditor(tab, t.path, t.line, t.col, { dir: split.dir, ...at });
        return;
      }
    }
    const pane = await this.addTerminal(tab, split.dir, {}, at);
    pane.pasteSoon(dropText(paths));
  }

  private onKey(e: KeyboardEvent) {
    logKey(e);
    const action = this.keymap.match(e);
    if (!action) return;
    e.preventDefault();
    e.stopPropagation();
    void this.run(action);
  }

  /**
   * Run a keybind action. `from` is the pane a control-socket request came from.
   *
   * Without it, an action sent by something sitting in a background tab would land on whichever
   * tab happens to be in front — so `wate ctl action split_right` from a pane the user is not
   * looking at used to split the wrong tab. A keystroke has no origin pane and keeps using the
   * active tab, which is the same thing from the keyboard's point of view.
   */
  async run(action: string, from?: string): Promise<void> {
    const origin = from ? this.tabOfPane(from) : undefined;
    const tab = origin ?? this.active;
    // Split beside the pane that asked, not beside whatever was last focused over there.
    const at = origin ? { target: from } : undefined;
    if (action === "__debug") {
      this.debugDump();
      return;
    }
    if (action === "__keys") {
      keys.recording = !keys.recording;
      takeKeys();
      console.warn("[keys] recording", keys.recording);
      return;
    }
    if (action.startsWith("__drop")) {
      await this.debugDrop(action.slice("__drop".length).trim());
      return;
    }
    if (action === "__reattach") {
      await this.debugReattach();
      return;
    }
    if (action === "__perf") {
      // Timing costs a little on the hot path, so it is off until someone asks for it.
      perf.timing = !perf.timing;
      takePerf();
      console.warn("[perf] timing", perf.timing);
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
        if (tab) await this.addTerminal(tab, "row", {}, at);
        break;
      case "split_down":
        if (tab) await this.addTerminal(tab, "col", {}, at);
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
      case "close_tab":
        if (tab) this.closeTab(tab);
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
      case "move_tab_to_new_window":
        if (this.active) await this.moveTabToWindow(this.active, "");
        break;
      case "new_window":
        await WindowService.NewWindow(await this.activeCwd());
        break;
      case "move_tab_left":
        this.nudgeTab(-1);
        break;
      case "move_tab_right":
        this.nudgeTab(1);
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

  /**
   * Hidden: act out an OS file drop, so drag and drop can be exercised over the control socket
   * without a mouse — `wate ctl action '__drop {"x":700,"y":420,"paths":["/etc/hosts"]}'`, or the
   * short form `wate ctl action __drop 700 420 /etc/hosts`. With `"motion": true` it only paints
   * the preview for two seconds.
   */
  private async debugDrop(arg: string) {
    let spec: { x: number; y: number; paths?: string[]; motion?: boolean };
    try {
      if (arg.startsWith("{")) {
        spec = JSON.parse(arg);
      } else {
        const [sx, sy, ...rest] = arg.split(/\s+/).filter(Boolean);
        spec = { x: Number(sx), y: Number(sy), paths: rest.length ? [rest.join(" ")] : [] };
      }
    } catch (err) {
      console.warn("[drop] bad argument", arg, err);
      return;
    }
    if (spec.motion) {
      this.previewDrop(spec.x, spec.y);
      setTimeout(() => this.endDrag(), 2000);
    } else {
      await this.handleDrop(spec.paths ?? [], spec.x, spec.y);
    }
    console.warn("[drop]", this.lastDrop ?? spec);
  }

  /** Rendering/state summary for `wate ctl debug`; ends up in the Go log. */
  /**
   * Prove the re-attach path without involving a second window: drop the focused pane and
   * build a new one over the very same shell. If the shell dies or output goes missing here,
   * moving a tab between windows would do the same.
   */
  private async debugReattach() {
    const tab = this.active;
    const old = tab?.focused;
    if (!tab || !(old instanceof TerminalPane)) {
      console.warn("[reattach] no terminal pane focused");
      return;
    }
    const paneId = old.id;
    const replay = old.serialize(RESTORE_SCROLLBACK);
    const where = tab.tree;
    old.detach();
    tab.panes.delete(paneId);
    const pane = this.makeTerminal(tab, { id: paneId, adopt: true, replay, visible: true });
    tab.panes.set(paneId, pane);
    tab.tree = where; // the layout still names this pane; only the object behind it changed
    tab.render();
    await pane.start();
    tab.setFocus(paneId);
    tab.focusPane();
    console.warn("[reattach] pane", paneId, "re-attached");
  }

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
        geometry: p instanceof TerminalPane ? p.geometry() : undefined,
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
      perf: takePerf(),
      lastDrop: this.lastDrop,
      keys: keys.recording ? takeKeys() : undefined,
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
