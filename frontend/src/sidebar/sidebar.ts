import { AgentStateService, type SavedAgentSession, type Session } from "../api";
import type { AgentStore } from "../agent/store";
import { showMenu, type MenuEntry } from "../ui/menu";
import { groupSessions, rowCwd, rowTitle, type Row } from "./groups";

export interface SidebarHost {
  jumpTo(session: Session): void;
  launch(): void;
  launchLabel: string;
  /** Key that toggles the panel, for tooltips. */
  toggleLabel?: string;
  /** Called after the panel was shown or hidden (panes need a re-fit). */
  onVisibility?(): void;
  /** Type into a live session's pane, as if the keys had been pressed. */
  sendToPane(paneId: string, text: string): void;
  /** Open a pane running `claude --resume <id>` in `cwd`. */
  resume(sessionId: string, cwd: string): void;
  /** This window's id, so rows belonging to another window can say so. */
  windowId(): string;
}

export const STATUS_LABEL: Record<string, string> = { running: "working", waiting: "waiting for you", done: "finished" };

const DOTS =
  '<svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor" aria-hidden="true">' +
  '<circle cx="12" cy="5" r="1.7"/><circle cx="12" cy="12" r="1.7"/><circle cx="12" cy="19" r="1.7"/>' +
  "</svg>";

const CHEVRON =
  '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>';

/** Right-hand panel listing Claude Code sessions across all tabs. */
export class Sidebar {
  readonly element = document.createElement("aside");
  private list = document.createElement("div");
  private edge = document.createElement("div");
  private timer?: ReturnType<typeof setInterval>;
  /** What wate remembers about sessions; mirrored from disk, written through on every change. */
  private saved: SavedAgentSession[] = [];
  private collapsed: Record<string, boolean | undefined> = {};
  /** session id → the cwd/title last written back, so a live favourite syncs once, not per frame. */
  private synced = new Map<string, string>();

  constructor(private store: AgentStore, private host: SidebarHost) {
    this.element.className = "sidebar";
    this.element.hidden = true;
    const head = document.createElement("div");
    head.className = "sidebar-head";
    head.textContent = "Claude Code";
    const close = document.createElement("button");
    close.className = "sidebar-close";
    close.textContent = "×";
    close.title = host.toggleLabel ? `Hide (${host.toggleLabel})` : "Hide";
    close.addEventListener("click", () => this.toggle(false));
    head.appendChild(close);
    const launch = document.createElement("button");
    launch.className = "sidebar-launch";
    launch.textContent = "+ New session";
    launch.title = host.launchLabel;
    launch.addEventListener("click", () => host.launch());
    this.list.className = "sidebar-list";
    this.edge.className = "sidebar-edge";
    this.element.append(this.edge, head, this.list, launch);
    store.subscribe(() => this.render());
    void this.reload();
  }

  /** Re-read what is remembered about sessions (favourites, names, folded sections). */
  private async reload() {
    try {
      const f = await AgentStateService.List();
      this.saved = f?.sessions ?? [];
      this.collapsed = f?.collapsed ?? {};
    } catch (err) {
      console.warn("sidebar state:", err);
    }
    this.render();
  }

  toggle(force?: boolean) {
    const show = force ?? this.element.hidden;
    this.element.hidden = !show;
    clearInterval(this.timer);
    if (show) {
      this.render();
      this.timer = setInterval(() => this.render(), 10_000);
    }
    this.host.onVisibility?.();
  }

  /** Like the pane dividers, the panel's left hairline skips the stretch beside the focused pane. */
  cutEdge(focused: DOMRect | null) {
    let mask = "";
    if (focused && this.visible) {
      const r = this.element.getBoundingClientRect();
      const lo = Math.max(r.top, focused.top) - r.top;
      const hi = Math.min(r.bottom, focused.bottom) - r.top;
      if (Math.abs(focused.right - r.left) <= 12 && hi - lo > 1) mask = `linear-gradient(to bottom, #000 ${lo}px, transparent ${lo}px, transparent ${hi}px, #000 ${hi}px)`;
    }
    this.edge.style.maskImage = mask;
    this.edge.style.webkitMaskImage = mask;
  }

  get visible() {
    return !this.element.hidden;
  }

  render() {
    // The store fires on every status tick; a hidden panel has nothing to show for it.
    if (!this.visible) return;
    const { favorites, others } = groupSessions(this.store.list(), this.saved);
    if (favorites.length === 0 && others.length === 0) {
      const empty = document.createElement("div");
      empty.className = "sidebar-empty";
      empty.innerHTML = `No Claude Code sessions.<br><kbd>${this.host.launchLabel}</kbd> starts one in the current directory.`;
      this.list.replaceChildren(empty);
      return;
    }
    for (const r of favorites) this.syncSaved(r);
    const blocks: HTMLElement[] = [];
    if (favorites.length) blocks.push(this.section("favorites", "Favourites", favorites));
    blocks.push(this.section("sessions", "Sessions", others));
    this.list.replaceChildren(...blocks);
  }

  /**
   * Keep a favourite's remembered directory and title current while its session is alive —
   * once it ends they are all the Resume row has left to show.
   */
  private syncSaved(r: Row) {
    if (!r.live || !r.id) return;
    const stamp = `${r.live.cwd}\u0000${r.live.title}`;
    if (this.synced.get(r.id) === stamp) return;
    this.synced.set(r.id, stamp);
    if (r.saved && r.saved.cwd === r.live.cwd && r.saved.title === r.live.title) return;
    void this.remember({ ...(r.saved ?? blank(r.id)), cwd: r.live.cwd, title: r.live.title });
  }

  private section(key: string, label: string, rows: Row[]): HTMLElement {
    const wrap = document.createElement("div");
    wrap.className = "sidebar-section";
    const collapsed = !!this.collapsed[key];

    const head = document.createElement("button");
    head.className = "sidebar-sectionhead" + (collapsed ? " collapsed" : "");
    const chev = document.createElement("span");
    chev.className = "sidebar-chevron";
    chev.innerHTML = CHEVRON;
    const name = document.createElement("span");
    name.className = "sidebar-sectionname";
    name.textContent = label;
    const count = document.createElement("span");
    count.className = "sidebar-sectioncount";
    count.textContent = String(rows.length);
    head.append(chev, name, count);
    head.addEventListener("click", () => {
      this.collapsed[key] = !this.collapsed[key];
      this.render();
      AgentStateService.SetCollapsed(key, this.collapsed[key]).catch((err) => console.warn("collapse:", err));
    });

    const body = document.createElement("div");
    body.className = "sidebar-sectionbody";
    body.hidden = collapsed;
    if (rows.length === 0) {
      const hint = document.createElement("div");
      hint.className = "sidebar-sectionempty";
      hint.textContent = key === "favorites" ? "Nothing pinned yet." : "Everything is pinned above.";
      body.appendChild(hint);
    } else {
      body.append(...rows.map((r) => this.row(r)));
    }
    wrap.append(head, body);
    return wrap;
  }

  private row(r: Row): HTMLElement {
    const s = r.live;
    const status = r.ended ? "ended" : (s?.status ?? "idle");
    const row = document.createElement("div");
    row.className = `sidebar-row status-${status}` + (r.ended ? " ended" : "");

    const dot = document.createElement("span");
    dot.className = "status-dot";

    const main = document.createElement("div");
    main.className = "sidebar-main";
    const cwd = rowCwd(r);
    const title = document.createElement("div");
    title.className = "sidebar-title";
    title.textContent = rowTitle(r, shortPath(cwd) || "Claude");
    title.title = [rowTitle(r, ""), cwd].filter(Boolean).join("\n");
    // The panel lists every window's sessions; say so when one is not in this window.
    const elsewhere = !!s?.window_id && !!this.host.windowId() && s.window_id !== this.host.windowId();
    const sub = document.createElement("div");
    sub.className = "sidebar-sub";
    sub.textContent = [
      r.saved?.label || !s ? shortPath(cwd) : "",
      r.ended ? "ended" : (STATUS_LABEL[status] ?? status),
      s ? elapsed(s.started_at) : "",
      elsewhere ? "other window" : "",
    ]
      .filter(Boolean)
      .join(" · ");
    if (elsewhere) row.classList.add("elsewhere");
    main.append(title, sub);

    if (r.ended) {
      const resume = document.createElement("a");
      resume.className = "sidebar-resume settings-link";
      resume.href = "#";
      resume.textContent = "Resume →";
      resume.addEventListener("click", (e) => {
        e.preventDefault();
        e.stopPropagation();
        this.host.resume(r.id, cwd);
      });
      main.appendChild(resume);
    } else if (s) {
      const ctx = contextRow(s);
      if (ctx) main.appendChild(ctx);
      if (s.message) {
        const msg = document.createElement("div");
        msg.className = "sidebar-msg";
        msg.textContent = s.message;
        main.appendChild(msg);
      }
    }

    const menu = document.createElement("button");
    menu.className = "sidebar-dots";
    menu.innerHTML = DOTS;
    menu.title = "More";
    menu.addEventListener("click", (e) => {
      e.stopPropagation();
      this.openRowMenu(r, menu.getBoundingClientRect());
    });

    row.append(dot, main, menu);
    if (s) row.addEventListener("click", () => this.host.jumpTo(s));
    return row;
  }

  // ---- row actions ------------------------------------------------------

  /** Write one entry through to disk and repaint from the answer. */
  private async remember(e: SavedAgentSession) {
    try {
      await AgentStateService.Set(e);
    } catch (err) {
      console.warn("save session entry:", err);
    }
    await this.reload();
  }

  private openRowMenu(r: Row, anchor: DOMRect) {
    const live = r.live;
    // Favourites and names are keyed on the session id, which only hooks can supply.
    const keyed = !!r.id;
    const entries: MenuEntry[] = [];

    if (keyed) {
      entries.push({
        label: r.favorite ? "Unfavourite" : "Favourite",
        checked: r.favorite,
        onSelect: () => void this.remember({ ...(r.saved ?? blank(r.id)), cwd: rowCwd(r), title: r.live?.title ?? r.saved?.title ?? "", favorite: !r.favorite }),
      });
      entries.push({
        input: {
          placeholder: "name this session",
          button: "Rename",
          value: r.saved?.label ?? "",
          onSubmit: (name) => this.rename(r, name.trim()),
        },
      });
    } else {
      entries.push({ label: "No session id", hint: "needs hooks", disabled: true });
    }

    if (live) {
      entries.push({
        label: "Remote control…",
        hint: "/remote-control",
        onSelect: () => this.host.sendToPane(live.pane_id, "/remote-control\r"),
      });
    }
    if (r.ended) {
      entries.push({ label: "Resume", onSelect: () => this.host.resume(r.id, rowCwd(r)) });
    }

    entries.push("separator");
    if (live) {
      entries.push({
        label: "End session",
        hint: "Ctrl-C twice",
        onSelect: () => this.endSession(live.pane_id),
      });
    }
    if (keyed) {
      entries.push({
        label: "Delete…",
        danger: true,
        onSelect: () => this.confirmDelete(r, anchor),
      });
    }
    showMenuLeftOf(anchor, entries);
  }

  private rename(r: Row, name: string) {
    void this.remember({ ...(r.saved ?? blank(r.id)), cwd: rowCwd(r), title: r.live?.title ?? r.saved?.title ?? "", label: name });
    // Claude keeps its own name for the conversation; /rename is how it is told.
    if (r.live && name) this.host.sendToPane(r.live.pane_id, `/rename ${name}\r`);
  }

  /** Claude exits on a second Ctrl-C; the shell underneath stays, and so does the pane. */
  private endSession(paneId: string) {
    this.host.sendToPane(paneId, "\x03");
    setTimeout(() => this.host.sendToPane(paneId, "\x03"), 120);
  }

  /** Deleting a transcript cannot be undone and reaches outside wate, so it is asked twice. */
  private confirmDelete(r: Row, anchor: DOMRect) {
    showMenuLeftOf(anchor, [
      { header: "Delete this session?" },
      { label: "Its transcript is removed from ~/.claude/projects.", disabled: true },
      "separator",
      {
        label: "Delete transcript",
        danger: true,
        onSelect: () => void this.deleteSession(r),
      },
    ]);
  }

  private async deleteSession(r: Row) {
    try {
      await AgentStateService.DeleteTranscript(r.id);
    } catch (err) {
      // A session that never wrote a transcript is not an error worth shouting about.
      console.warn("delete transcript:", err);
    }
    try {
      await AgentStateService.Remove(r.id);
    } catch (err) {
      console.warn("forget session:", err);
    }
    this.synced.delete(r.id);
    await this.reload();
  }
}

function blank(id: string): SavedAgentSession {
  return { session_id: id, label: "", cwd: "", title: "", favorite: false, seen_at: "" } as SavedAgentSession;
}

/** Drop a menu below its button and pull it back inside the panel's right edge. */
function showMenuLeftOf(anchor: DOMRect, entries: MenuEntry[]) {
  const el = showMenu(anchor.right, anchor.bottom + 2, entries);
  const r = el.getBoundingClientRect();
  el.style.left = `${Math.max(4, anchor.right - r.width)}px`;
}

/** Context-window usage bar: "292k / 1M · 29 %" — how close the session is to a compact. */
function contextRow(s: Session): HTMLElement | null {
  const tokens = s.context?.tokens ?? 0;
  const window = s.context?.window ?? 0;
  if (!tokens || !window) return null;
  const pct = s.context_percent || Math.round((tokens / window) * 100);
  const row = document.createElement("div");
  row.className = "sidebar-ctx" + (pct >= 80 ? " hot" : pct >= 60 ? " warn" : "");
  row.title = `${tokens.toLocaleString()} of ${window.toLocaleString()} context tokens in use` + (s.context?.model ? ` (${s.context.model})` : "") + ". Auto-compact kicks in near the limit; /compact frees it earlier.";
  const bar = document.createElement("div");
  bar.className = "sidebar-ctx-bar";
  const fill = document.createElement("div");
  fill.className = "sidebar-ctx-fill";
  fill.style.width = `${Math.min(100, pct)}%`;
  bar.appendChild(fill);
  const text = document.createElement("span");
  text.className = "sidebar-ctx-text";
  text.textContent = `${fmtTokens(tokens)} / ${fmtTokens(window)} · ${pct} %`;
  row.append(bar, text);
  return row;
}

export function fmtTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n % 1_000_000 === 0 ? 0 : 1)}M`;
  if (n >= 1000) return `${Math.round(n / 1000)}k`;
  return String(n);
}

export function shortPath(p: string): string {
  if (!p) return "";
  const home = /^\/(?:home|Users)\/[^/]+/.exec(p)?.[0];
  const short = home ? "~" + p.slice(home.length) : p;
  // Keep the tail (project name) visible when the path is long.
  const parts = short.split("/");
  return parts.length > 4 ? `${parts[0]}/…/${parts.slice(-2).join("/")}` : short;
}

export function elapsed(since: string): string {
  const ms = Date.now() - new Date(since).getTime();
  if (!(ms > 0)) return "";
  const m = Math.floor(ms / 60000);
  if (m < 1) return "just now";
  if (m < 60) return `${m} min`;
  return `${Math.floor(m / 60)} h ${m % 60} min`;
}
