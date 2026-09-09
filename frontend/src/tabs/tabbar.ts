import type { Tab } from "./tab";

export interface TabBarHost {
  activate(tab: Tab): void;
  close(tab: Tab): void;
  newTab(): void;
  openSettings(): void;
  /** Claude status for the tab's badge ("idle" for none). */
  agentStatus(tab: Tab): string;
}

const GEAR =
  '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
  '<circle cx="12" cy="12" r="3.2"/>' +
  '<path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.87-.34 1.7 1.7 0 0 0-1.03 1.56V21a2 2 0 1 1-4 0v-.09a1.7 1.7 0 0 0-1.11-1.56 1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.7 1.7 0 0 0 4.6 15a1.7 1.7 0 0 0-1.56-1.03H3a2 2 0 1 1 0-4h.09A1.7 1.7 0 0 0 4.65 8.9a1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.7 1.7 0 0 0 9 4.6a1.7 1.7 0 0 0 1.03-1.56V3a2 2 0 1 1 4 0v.09a1.7 1.7 0 0 0 1.03 1.56 1.7 1.7 0 0 0 1.87-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.7 1.7 0 0 0 19.4 9a1.7 1.7 0 0 0 1.56 1.03H21a2 2 0 1 1 0 4h-.09A1.7 1.7 0 0 0 19.4 15z"/>' +
  "</svg>";

/** The strip at the top: one button per tab, a "+" and empty space that drags the window. */
export class TabBar {
  readonly element = document.createElement("div");
  private list = document.createElement("div");

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
    const settings = document.createElement("button");
    settings.className = "tabbar-settings";
    settings.title = "Settings";
    settings.innerHTML = GEAR;
    settings.addEventListener("click", () => host.openSettings());
    this.element.append(this.list, add, drag, settings);
  }

  render(tabs: Tab[], active: Tab | null) {
    this.list.replaceChildren(
      ...tabs.map((tab, i) => {
        const b = document.createElement("div");
        const status = this.host.agentStatus(tab);
        b.className = "tabbar-tab" + (tab === active ? " active" : "") + (status !== "idle" ? ` status-${status}` : "");
        b.dataset.tabId = tab.id;
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
        title.textContent = tab.title;
        const close = document.createElement("button");
        close.className = "tabbar-close";
        close.textContent = "×";
        close.title = "Close tab";
        close.addEventListener("click", (e) => {
          e.stopPropagation();
          this.host.close(tab);
        });
        b.append(idx, title, close);
        b.addEventListener("mousedown", (e) => {
          if (e.button === 1) {
            e.preventDefault();
            this.host.close(tab);
          } else if (e.button === 0) this.host.activate(tab);
        });
        return b;
      }),
    );
  }
}
