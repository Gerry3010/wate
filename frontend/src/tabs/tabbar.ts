import type { Tab } from "./tab";
import { showMenu, closeMenu, type MenuEntry } from "../ui/menu";
import type { SessionInfo } from "../api";

export interface TabBarHost {
  activate(tab: Tab): void;
  close(tab: Tab): void;
  newTab(): void;
  openSettings(): void;
  /** Claude status for the tab's badge ("idle" for none). */
  agentStatus(tab: Tab): string;
  renameTab(tab: Tab, title: string): void;
  colorTab(tab: Tab, color: string): void;
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
  /** Everything render() reads, as one string: unchanged means the DOM can stay as it is. */
  private sig = "";

  constructor(private host: TabBarHost) {
    this.element.className = "tabbar";
    this.list.className = "tabbar-tabs";
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
    if (!this.renaming && sig === this.sig) return;
    this.sig = this.renaming ? "" : sig;
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
        b.addEventListener("dblclick", (e) => {
          e.preventDefault();
          this.startRename(tab, tabs, active);
        });
        b.addEventListener("contextmenu", (e) => {
          e.preventDefault();
          this.showTabMenu(tab, tabs, active, e.clientX, e.clientY);
        });
        return b;
      }),
    );
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

  private showTabMenu(tab: Tab, tabs: Tab[], active: Tab | null, x: number, y: number) {
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
      { label: "Close tab", danger: true, onSelect: () => this.host.close(tab) },
    ];
    showMenu(x, y, entries);
  }

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
    entries.push("separator", { label: "Import tabs from another terminal…", onSelect: () => this.host.openImport() });
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
