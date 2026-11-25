import type { Notice, NoticeList } from "../api";
import { ago, badgeText, BANNER_MS, KIND_LABEL, MAX_BANNERS } from "./format";

export interface BellHost {
  /** A notice was clicked: it is read, and its pane comes to the front. */
  open(id: string): void;
  markAllRead(): void;
  clear(): void;
}

const BELL =
  '<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
  '<path d="M6 8a6 6 0 1 1 12 0c0 7 3 9 3 9H3s3-2 3-9"/><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0"/>' +
  "</svg>";

/**
 * wate's own notification list: a bell in the tab bar with the unread count, a panel that
 * lists what came in, and banners in the corner for whatever arrives while the window has
 * the focus. The backend decides where a notice goes (banner here, or the desktop while wate
 * is in the background); this only shows it.
 */
export class NoticeBell {
  readonly button = document.createElement("button");
  /** The corner the banners stack up in; the app puts it in the page. */
  readonly banners = document.createElement("div");
  private badge = document.createElement("span");
  private panel: HTMLElement | null = null;
  private list: NoticeList = { notices: [], unread: 0 };

  constructor(private host: BellHost) {
    this.button.className = "tabbar-bell";
    this.button.title = "Notifications";
    this.button.innerHTML = BELL;
    this.badge.className = "tabbar-bell-badge";
    this.badge.hidden = true;
    this.button.appendChild(this.badge);
    this.button.addEventListener("click", (e) => {
      e.stopPropagation();
      if (this.panel) this.closePanel();
      else this.openPanel();
    });
    this.banners.className = "notice-banners";
  }

  update(list: NoticeList) {
    this.list = { notices: list.notices ?? [], unread: list.unread };
    const text = badgeText(list.unread);
    this.badge.textContent = text;
    this.badge.hidden = text === "";
    this.button.classList.toggle("unread", text !== "");
    this.setTooltip();
    if (this.panel) this.renderPanel();
  }

  /** A notice that just came in, shown in the corner for a few seconds. Only a window that has
   *  the focus shows it: every window hears of every notice. */
  banner(n: Notice) {
    if (!document.hasFocus()) return;
    while (this.banners.childElementCount >= MAX_BANNERS) this.banners.firstElementChild?.remove();
    const el = document.createElement("div");
    el.className = `notice-banner kind-${n.kind}`;
    el.setAttribute("role", "status");
    const close = document.createElement("button");
    close.className = "notice-banner-close";
    close.title = "Dismiss";
    close.textContent = "×";
    close.addEventListener("click", (e) => {
      e.stopPropagation();
      el.remove();
    });
    el.append(this.noticeBody(n), close);
    el.addEventListener("click", () => {
      el.remove();
      this.host.open(n.id);
    });
    // Hovering holds it, so it does not vanish from under the pointer.
    let timer = window.setTimeout(() => el.remove(), BANNER_MS);
    el.addEventListener("pointerenter", () => window.clearTimeout(timer));
    el.addEventListener("pointerleave", () => {
      timer = window.setTimeout(() => el.remove(), BANNER_MS / 2);
    });
    this.banners.appendChild(el);
  }

  /** The button's tooltip, left off while the panel is open: it would cover the panel's head. */
  private setTooltip() {
    const unread = this.list.unread;
    this.button.title = this.panel ? "" : unread ? `Notifications (${unread} unread)` : "Notifications";
  }

  openPanel() {
    if (this.panel) return;
    const el = document.createElement("div");
    el.className = "notice-panel";
    el.addEventListener("pointerdown", (e) => e.stopPropagation());
    document.body.appendChild(el);
    this.panel = el;
    this.setTooltip();
    this.renderPanel();
    const r = this.button.getBoundingClientRect();
    el.style.top = `${r.bottom + 4}px`;
    el.style.right = `${Math.max(4, window.innerWidth - r.right)}px`;
    window.addEventListener("pointerdown", this.onOutside, true);
    window.addEventListener("keydown", this.onKey, true);
    window.addEventListener("blur", this.onBlur);
  }

  closePanel() {
    this.panel?.remove();
    this.panel = null;
    this.setTooltip();
    window.removeEventListener("pointerdown", this.onOutside, true);
    window.removeEventListener("keydown", this.onKey, true);
    window.removeEventListener("blur", this.onBlur);
  }

  private onOutside = (e: PointerEvent) => {
    const t = e.target as Node;
    if (this.panel && !this.panel.contains(t) && !this.button.contains(t)) this.closePanel();
  };

  private onKey = (e: KeyboardEvent) => {
    if (e.key !== "Escape") return;
    e.stopPropagation();
    this.closePanel();
  };

  private onBlur = () => this.closePanel();

  private renderPanel() {
    const el = this.panel;
    if (!el) return;
    const head = document.createElement("div");
    head.className = "notice-panel-head";
    const title = document.createElement("span");
    title.className = "notice-panel-title";
    title.textContent = "Notifications";
    head.appendChild(title);
    const notices = this.list.notices ?? [];
    if (this.list.unread) head.appendChild(this.headButton("Mark all read", () => this.host.markAllRead()));
    if (notices.length) head.appendChild(this.headButton("Clear", () => this.host.clear()));

    const body = document.createElement("div");
    body.className = "notice-panel-list";
    if (notices.length === 0) {
      const empty = document.createElement("div");
      empty.className = "notice-panel-empty";
      empty.textContent = "Nothing yet. Sessions that finish or wait for you show up here.";
      body.appendChild(empty);
    }
    for (const n of notices) {
      const row = document.createElement("button");
      row.className = `notice-row kind-${n.kind}` + (n.read ? " read" : "");
      row.append(this.noticeBody(n));
      row.addEventListener("click", () => {
        this.closePanel();
        this.host.open(n.id);
      });
      body.appendChild(row);
    }
    el.replaceChildren(head, body);
  }

  private headButton(label: string, onClick: () => void): HTMLElement {
    const b = document.createElement("button");
    b.className = "notice-panel-action";
    b.textContent = label;
    b.addEventListener("click", onClick);
    return b;
  }

  /** Dot, title, body and when: the same in a row of the panel and in a banner. */
  private noticeBody(n: Notice): HTMLElement {
    const wrap = document.createElement("div");
    wrap.className = "notice-body";
    const dot = document.createElement("span");
    dot.className = "notice-dot";
    const text = document.createElement("div");
    text.className = "notice-text";
    const title = document.createElement("div");
    title.className = "notice-title";
    title.textContent = n.title;
    title.title = n.title;
    const meta = document.createElement("div");
    meta.className = "notice-meta";
    meta.textContent = [KIND_LABEL[n.kind] ?? n.kind, ago(n.at)].filter(Boolean).join(" · ");
    text.append(title);
    if (n.body) {
      const body = document.createElement("div");
      body.className = "notice-detail";
      body.textContent = n.body;
      body.title = n.body;
      text.append(body);
    }
    text.append(meta);
    wrap.append(dot, text);
    return wrap;
  }
}
