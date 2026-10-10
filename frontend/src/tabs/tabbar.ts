import type { Tab } from "./tab";
import { showMenu, closeMenu, type MenuEntry } from "../ui/menu";
import type { SessionInfo } from "../api";
import { perf } from "../perf";
import { dropIndex, insertionIndex } from "./order";

/** How far the pointer travels before a press on a tab becomes a drag rather than a click. */
const DRAG_THRESHOLD = 4;

/**
 * How far below the bar a tab has to be dragged before letting go tears it into its own
 * window — the same gesture browsers use.
 *
 * It is measured inside the window on purpose. Pointer capture does keep reporting once the
 * cursor leaves a lone window, but as soon as a second one exists the coordinates are clamped
 * at the edge, so "is the pointer outside the window" is not something the drag can know.
 */
const TEAR_OFF = 56;

export interface TabBarHost {
  activate(tab: Tab): void;
  close(tab: Tab): void;
  newTab(): void;
  openSettings(): void;
  /** Claude status for the tab's badge ("idle" for none). */
  agentStatus(tab: Tab): string;
  renameTab(tab: Tab, title: string): void;
  colorTab(tab: Tab, color: string): void;
  /** Repaint the bar from the app's current state (a drag held it back). */
  requestRender(): void;
  /** Reorder: put `tab` at index `to`. */
  moveTab(tab: Tab, to: number): void;
  /** Windows this tab could be moved to (excluding the one it is in). */
  otherWindows(): Promise<{ id: string; index: number }[]>;
  /** Move `tab` to window `to`; an empty id means a window of its own. */
  moveTabToWindow(tab: Tab, to: string): void;
  /** A tab was let go outside this window (point in this window's client coordinates). */
  dropTabOutside(tab: Tab, x: number, y: number): void;
  /** Ask the running agents whether wate may restart. */
  prepareRestart(): void;
  /** Saved sessions for the dropdown. */
  listSessions(): Promise<SessionInfo[]>;
  saveSession(name: string): void;
  openSession(id: string): void;
  deleteSession(id: string): void;
  openImport(): void;
}

/** Tab colours offered in the menu (Warp's names map onto these). */
export const TAB_COLORS: [string, string][] = [
  ["red", "#f38ba8"],
  ["orange", "#fab387"],
  ["yellow", "#f9e2af"],
  ["green", "#a6e3a1"],
  ["cyan", "#94e2d5"],
  ["blue", "#89b4fa"],
  ["purple", "#cba6f7"],
  ["pink", "#f5c2e7"],
  ["gray", "#9399b2"],
];

export function tabColorValue(color: string): string {
  if (!color) return "";
  const named = TAB_COLORS.find(([n]) => n === color.toLowerCase());
  if (named) return named[1];
  return /^#[0-9a-f]{6}$/i.test(color) ? color : "";
}

const GEAR =
  '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
  '<circle cx="12" cy="12" r="3.2"/>' +
  '<path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.87-.34 1.7 1.7 0 0 0-1.03 1.56V21a2 2 0 1 1-4 0v-.09a1.7 1.7 0 0 0-1.11-1.56 1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.7 1.7 0 0 0 4.6 15a1.7 1.7 0 0 0-1.56-1.03H3a2 2 0 1 1 0-4h.09A1.7 1.7 0 0 0 4.65 8.9a1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.7 1.7 0 0 0 9 4.6a1.7 1.7 0 0 0 1.03-1.56V3a2 2 0 1 1 4 0v.09a1.7 1.7 0 0 0 1.03 1.56 1.7 1.7 0 0 0 1.87-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.7 1.7 0 0 0 19.4 9a1.7 1.7 0 0 0 1.56 1.03H21a2 2 0 1 1 0 4h-.09A1.7 1.7 0 0 0 19.4 15z"/>' +
  "</svg>";

const CHEVRON =
  '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>';

/** The strip at the top: one button per tab, a "+", empty space that drags the window,
 *  a sessions dropdown and the settings gear. */
export class TabBar {
  readonly element = document.createElement("div");
  private list = document.createElement("div");
  private renaming: Tab | null = null;
  /** Set while a tab is being dragged, so a repaint cannot pull the DOM out from under it. */
  private dragging: Tab | null = null;
  private caret = document.createElement("div");
  /** Last position painted for the caret; an unchanged one writes no style (see DropHighlight). */
  private caretAt = "";
  /** Everything render() reads, as one string: unchanged means the DOM can stay as it is. */
  private sig = "";

  constructor(private host: TabBarHost) {
    this.element.className = "tabbar";
    this.list.className = "tabbar-tabs";
    this.caret.className = "tabbar-caret";
    document.body.appendChild(this.caret);
    const add = document.createElement("button");
    add.className = "tabbar-add";
    add.title = "New tab";
    add.textContent = "+";
    add.addEventListener("click", () => host.newTab());
    const drag = document.createElement("div");
    drag.className = "tabbar-drag";
    const sessions = document.createElement("button");
    sessions.className = "tabbar-sessions";
    sessions.title = "Sessions";
    sessions.innerHTML = CHEVRON;
    sessions.addEventListener("click", (e) => {
      const r = sessions.getBoundingClientRect();
      e.stopPropagation();
      void this.showSessionMenu(r.right, r.bottom + 2, true);
    });
    const settings = document.createElement("button");
    settings.className = "tabbar-settings";
    settings.title = "Settings";
    settings.innerHTML = GEAR;
    settings.addEventListener("click", () => host.openSettings());
    this.element.append(this.list, add, drag, sessions, settings);
    // Right-click on empty bar space: the sessions menu.
    for (const el of [drag, this.list, this.element]) {
      el.addEventListener("contextmenu", (e) => {
        if ((e.target as HTMLElement).closest(".tabbar-tab")) return;
        e.preventDefault();
        void this.showSessionMenu(e.clientX, e.clientY, false);
      });
    }
  }

  render(tabs: Tab[], active: Tab | null) {
    // Rebuilding the bar throws away every tab's DOM and listeners; while a Claude session
    // streams, the callers fire constantly with nothing actually changed.
    const sig = tabs
      .map((t) => [t.id, t.title, t.detail, t.color, this.host.agentStatus(t), t === active].join("\u001f"))
      .join("\u001e");
    // A drag holds references to the tab elements; replaceChildren would throw them away
    // mid-gesture (a terminal title changing is enough to get here).
    if (this.dragging) return;
    if (!this.renaming && sig === this.sig) return;
    this.sig = this.renaming ? "" : sig;
    perf.tabRenders++;
    this.list.replaceChildren(
      ...tabs.map((tab, i) => {
        const b = document.createElement("div");
        const status = this.host.agentStatus(tab);
        b.className = "tabbar-tab" + (tab === active ? " active" : "") + (status !== "idle" ? ` status-${status}` : "");
        b.dataset.tabId = tab.id;
        const color = tabColorValue(tab.color);
        if (color) {
          b.classList.add("colored");
          b.style.setProperty("--tab-color", color);
        }
        const idx = document.createElement("span");
        idx.className = "tabbar-index";
        idx.textContent = String(i + 1);
        if (status !== "idle") {
          idx.className = "status-dot tabbar-badge";
          idx.textContent = "";
          idx.title = `Claude Code: ${status}`;
        }
        const title = document.createElement("span");
        title.className = "tabbar-title";
        if (this.renaming === tab) {
          title.appendChild(this.renameField(tab));
        } else {
          title.textContent = tab.title;
          const detail = tab.detail || tab.autoTitle;
          title.title = tab.customTitle ? `${tab.customTitle}\n${detail}` : detail;
        }
        const close = document.createElement("button");
        close.className = "tabbar-close";
        close.textContent = "×";
        close.title = "Close tab";
        close.addEventListener("click", (e) => {
          e.stopPropagation();
          this.host.close(tab);
        });
        // Index/status dot and the close button share one fixed slot at the tab's end:
        // hovering swaps them in place, so revealing the × never reflows the bar.
        const slot = document.createElement("span");
        slot.className = "tabbar-slot";
        slot.append(idx, close);
        b.append(title, slot);
        b.addEventListener("mousedown", (e) => {
          if (this.renaming === tab) return;
          if (e.button === 1) {
            e.preventDefault();
            this.host.close(tab);
          } else if (e.button === 0) this.host.activate(tab);
        });
        b.addEventListener("pointerdown", (e) => {
          if (e.button !== 0 || this.renaming) return;
          if ((e.target as HTMLElement).closest(".tabbar-close")) return;
          this.startDrag(tab, b, e, tabs, active);
        });
        b.addEventListener("dblclick", (e) => {
          e.preventDefault();
          this.startRename(tab, tabs, active);
        });
        b.addEventListener("contextmenu", (e) => {
          e.preventDefault();
          void this.showTabMenu(tab, tabs, active, e.clientX, e.clientY);
        });
        return b;
      }),
    );
  }

  /**
   * Drag a tab to a new place in the bar. Built like the pane divider (layout/view.ts): the
   * geometry is measured once, the pointer is captured so the listeners can live on the tab
   * itself, moves are coalesced into one frame, and the model is only written on release —
   * everything before that is a preview the Escape key can throw away.
   */
  private startDrag(tab: Tab, el: HTMLElement, down: PointerEvent, tabs: Tab[], active: Tab | null) {
    const from = tabs.indexOf(tab);
    // A lone tab is still worth dragging: there is nowhere to reorder it to, but it can be
    // torn out into a window of its own.
    if (from < 0) return;
    const startX = down.clientX;
    const startY = down.clientY;
    // Held from the first press, not from the moment the drag passes its threshold: the
    // mousedown that follows activates the tab, and the repaint that causes would replace
    // the element this gesture is holding on to.
    this.dragging = tab;
    let started = false;
    let frame = 0;
    let point = startX;
    let to = from;
    let outside = false;
    let lastPoint: [number, number] = [0, 0];
    let rects: DOMRect[] = [];
    let centers: number[] = [];

    const begin = () => {
      started = true;
      // Measured once: asking per event would force a layout right after the last one was written.
      rects = (Array.from(this.list.children) as HTMLElement[]).map((c) => c.getBoundingClientRect());
      centers = rects.map((r) => r.left + r.width / 2);
      el.classList.add("dragging");
    };

    const apply = () => {
      frame = 0;
      // The gap is an index into the layout as drawn; the landing index is that gap once the
      // dragged tab has been lifted out of its own slot. The caret wants the former.
      const gap = insertionIndex(centers, point);
      to = dropIndex(from, gap);
      el.style.transform = outside ? `translate(${point - startX}px, ${lastPoint[1] - startY}px)` : `translateX(${point - startX}px)`;
      if (!outside) this.showCaret(rects, gap);
    };

    const move = (e: PointerEvent) => {
      point = e.clientX;
      // Client coordinates, not screenX/Y: the WebView reports those relative to the
      // window, so the backend does the conversion where the window's position is known.
      lastPoint = [e.clientX, e.clientY];
      if (!started) {
        if (Math.abs(point - startX) < DRAG_THRESHOLD) return;
        begin();
      }
      const barBottom = rects[0]?.bottom ?? 0;
      const out = e.clientY > barBottom + TEAR_OFF || e.clientY < -TEAR_OFF;
      if (out !== outside) {
        outside = out;
        el.classList.toggle("tearing", out);
        if (out) this.hideCaret();
      }
      if (!frame) frame = requestAnimationFrame(apply);
    };

    const finish = (commit: boolean) => {
      document.removeEventListener("keydown", onKey, true);
      el.removeEventListener("pointermove", move);
      el.removeEventListener("pointerup", up);
      el.removeEventListener("pointercancel", cancel);
      this.dragging = null;
      if (!started) {
        // A plain click: the activation that came with it still owes the bar a repaint.
        this.host.requestRender();
        return;
      }
      if (frame) {
        cancelAnimationFrame(frame); // the last move may still be waiting for its frame
        if (commit) apply();
        frame = 0;
      }
      el.classList.remove("dragging", "tearing");
      el.style.transform = "";
      this.hideCaret();
      if (commit && outside) this.host.dropTabOutside(tab, lastPoint[0], lastPoint[1]);
      else if (commit && to !== from) this.host.moveTab(tab, to);
      else this.host.requestRender();
    };

    const up = () => finish(true);
    const cancel = () => finish(false);
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.preventDefault();
      e.stopPropagation();
      finish(false);
    };

    el.setPointerCapture(down.pointerId);
    el.addEventListener("pointermove", move);
    el.addEventListener("pointerup", up);
    el.addEventListener("pointercancel", cancel);
    document.addEventListener("keydown", onKey, true);
  }

  /** Put the insertion caret in gap `gap` (0 = before the first tab, n = after the last). */
  private showCaret(rects: DOMRect[], gap: number) {
    const last = rects[rects.length - 1];
    const x = gap <= 0 ? rects[0].left : gap >= rects.length ? last.right : (rects[gap - 1].right + rects[gap].left) / 2;
    const key = `${x} ${rects[0].top} ${rects[0].height}`;
    if (key === this.caretAt) return;
    this.caretAt = key;
    const s = this.caret.style;
    s.transform = `translate(${Math.round(x) - 1}px, ${rects[0].top}px)`;
    s.height = `${rects[0].height}px`;
    this.caret.classList.add("visible");
  }

  private hideCaret() {
    this.caretAt = "";
    this.caret.classList.remove("visible");
  }

  private startRename(tab: Tab, tabs: Tab[], active: Tab | null) {
    this.renaming = tab;
    this.render(tabs, active);
  }

  private renameField(tab: Tab): HTMLInputElement {
    const i = document.createElement("input");
    i.className = "tabbar-rename";
    i.value = tab.customTitle;
    i.placeholder = tab.autoTitle;
    i.spellcheck = false;
    const finish = (commit: boolean) => {
      if (this.renaming !== tab) return;
      this.renaming = null;
      if (commit) this.host.renameTab(tab, i.value.trim());
      else tab.onChange?.();
    };
    i.addEventListener("keydown", (e) => {
      e.stopPropagation();
      if (e.key === "Enter") finish(true);
      else if (e.key === "Escape") finish(false);
    });
    i.addEventListener("blur", () => finish(true));
    i.addEventListener("mousedown", (e) => e.stopPropagation());
    queueMicrotask(() => {
      i.focus();
      i.select();
    });
    return i;
  }

  private async showTabMenu(tab: Tab, tabs: Tab[], active: Tab | null, x: number, y: number) {
    // Fetched before the menu is built so "move to window N" sits with its own kind rather
    // than being appended after the destructive entry at the bottom.
    const others = await this.host.otherWindows().catch(() => [] as { id: string; index: number }[]);
    const entries: MenuEntry[] = [
      { label: "Rename tab…", onSelect: () => this.startRename(tab, tabs, active) },
      { header: "Colour" },
      { label: "None", swatch: "", checked: !tab.color, onSelect: () => this.host.colorTab(tab, "") },
      ...TAB_COLORS.map(([name, value]) => ({
        label: name[0].toUpperCase() + name.slice(1),
        swatch: value,
        checked: tab.color === name,
        onSelect: () => this.host.colorTab(tab, name),
      })),
      "separator",
      ...others.map((w) => ({ label: `Move to window ${w.index}`, onSelect: () => this.host.moveTabToWindow(tab, w.id) })),
      { label: "Move to new window", onSelect: () => this.host.moveTabToWindow(tab, "") },
      "separator",
      { label: "Close tab", danger: true, onSelect: () => this.host.close(tab) },
    ];
    showMenu(x, y, entries);
  }

  /** The tab button under a point, for dropping a pane onto a tab. */
  tabIdAt(x: number, y: number): string | null {
    const el = document.elementFromPoint(x, y)?.closest<HTMLElement>(".tabbar-tab[data-tab-id]");
    return el?.dataset.tabId ?? null;
  }

  /**
   * Mark the tab a dragged pane would land in, and say so.
   *
   * A tab button is a small target and the thing being dragged is somewhere else entirely, so
   * without this the gesture is a guess: the pane vanishes from one tab and turns up in
   * another with nothing in between to confirm the aim.
   */
  showPaneTarget(tabId: string | null): void {
    for (const el of this.list.querySelectorAll<HTMLElement>(".tabbar-tab.pane-target")) {
      if (el.dataset.tabId !== tabId) el.classList.remove("pane-target");
    }
    const el = tabId ? this.list.querySelector<HTMLElement>(`.tabbar-tab[data-tab-id="${CSS.escape(tabId)}"]`) : null;
    if (!el) {
      this.paneHint?.remove();
      this.paneHint = undefined;
      return;
    }
    el.classList.add("pane-target");
    if (!this.paneHint) {
      this.paneHint = document.createElement("div");
      this.paneHint.className = "pane-drag-hint";
      this.paneHint.textContent = "Move to this tab";
      document.body.appendChild(this.paneHint);
    }
    const r = el.getBoundingClientRect();
    this.paneHint.style.transform = `translate(${Math.round(r.left)}px, ${Math.round(r.bottom + 4)}px)`;
  }

  private paneHint?: HTMLElement;

  private async showSessionMenu(x: number, y: number, alignRight: boolean) {
    const sessions = await this.host.listSessions().catch(() => [] as SessionInfo[]);
    const entries: MenuEntry[] = [
      { header: "Save current tabs as" },
      { input: { placeholder: "session name", button: "Save", onSubmit: (name) => this.host.saveSession(name) } },
    ];
    if (sessions.length) {
      entries.push("separator", { header: "Saved sessions" });
      for (const s of sessions) {
        entries.push({
          label: s.name,
          hint: `${s.tabs} tab${s.tabs === 1 ? "" : "s"}, ${s.panes} pane${s.panes === 1 ? "" : "s"}`,
          onSelect: () => this.host.openSession(s.id),
          action: { label: "×", title: "Delete session", onSelect: () => this.host.deleteSession(s.id) },
        });
      }
    }
    entries.push(
      "separator",
      { label: "Prepare restart…", hint: "ask the agents first", onSelect: () => this.host.prepareRestart() },
      { label: "Import tabs from another terminal…", onSelect: () => this.host.openImport() },
    );
    const el = showMenu(x, y, entries);
    if (alignRight) {
      const r = el.getBoundingClientRect();
      el.style.left = `${Math.max(4, x - r.width)}px`;
    }
  }

  /** Hide any open popup (e.g. when the window loses focus or a tab changes). */
  closeMenus() {
    closeMenu();
  }
}
