import type { Tab } from "./tab";

export interface TabBarHost {
  activate(tab: Tab): void;
  close(tab: Tab): void;
  newTab(): void;
  /** Claude status for the tab's badge ("idle" for none). */
  agentStatus(tab: Tab): string;
}

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
    this.element.append(this.list, add, drag);
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
