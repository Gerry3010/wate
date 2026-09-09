import type { Session } from "../api";
import type { AgentStore } from "../agent/store";

export interface SidebarHost {
  jumpTo(session: Session): void;
  launch(): void;
  launchLabel: string;
}

const STATUS_LABEL: Record<string, string> = { running: "working", waiting: "waiting for you", done: "finished" };

/** Right-hand panel listing Claude Code sessions across all tabs. */
export class Sidebar {
  readonly element = document.createElement("aside");
  private list = document.createElement("div");
  private timer?: ReturnType<typeof setInterval>;

  constructor(private store: AgentStore, private host: SidebarHost) {
    this.element.className = "sidebar";
    this.element.hidden = true;
    const head = document.createElement("div");
    head.className = "sidebar-head";
    head.textContent = "Claude Code";
    const launch = document.createElement("button");
    launch.className = "sidebar-launch";
    launch.textContent = "+ New session";
    launch.title = host.launchLabel;
    launch.addEventListener("click", () => host.launch());
    this.list.className = "sidebar-list";
    this.element.append(head, this.list, launch);
    store.subscribe(() => this.render());
    this.render();
  }

  toggle(force?: boolean) {
    const show = force ?? this.element.hidden;
    this.element.hidden = !show;
    clearInterval(this.timer);
    if (show) this.timer = setInterval(() => this.render(), 10_000);
  }

  get visible() {
    return !this.element.hidden;
  }

  render() {
    const sessions = this.store.list();
    if (sessions.length === 0) {
      const empty = document.createElement("div");
      empty.className = "sidebar-empty";
      empty.innerHTML = `No Claude Code sessions.<br><kbd>${this.host.launchLabel}</kbd> starts one in the current directory.`;
      this.list.replaceChildren(empty);
      return;
    }
    this.list.replaceChildren(
      ...sessions.map((s) => {
        const row = document.createElement("div");
        row.className = `sidebar-row status-${s.status}`;
        const dot = document.createElement("span");
        dot.className = "status-dot";
        const main = document.createElement("div");
        main.className = "sidebar-main";
        const title = document.createElement("div");
        title.className = "sidebar-title";
        title.textContent = shortPath(s.cwd) || "Claude";
        const sub = document.createElement("div");
        sub.className = "sidebar-sub";
        sub.textContent = [STATUS_LABEL[s.status] ?? s.status, elapsed(s.started_at)].filter(Boolean).join(" · ");
        main.append(title, sub);
        const ctx = contextRow(s);
        if (ctx) main.appendChild(ctx);
        if (s.message) {
          const msg = document.createElement("div");
          msg.className = "sidebar-msg";
          msg.textContent = s.message;
          main.appendChild(msg);
        }
        row.append(dot, main);
        row.addEventListener("click", () => this.host.jumpTo(s));
        return row;
      }),
    );
  }
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

function shortPath(p: string): string {
  if (!p) return "";
  const home = /^\/(?:home|Users)\/[^/]+/.exec(p)?.[0];
  const short = home ? "~" + p.slice(home.length) : p;
  // Keep the tail (project name) visible when the path is long.
  const parts = short.split("/");
  return parts.length > 4 ? `${parts[0]}/…/${parts.slice(-2).join("/")}` : short;
}

function elapsed(since: string): string {
  const ms = Date.now() - new Date(since).getTime();
  if (!(ms > 0)) return "";
  const m = Math.floor(ms / 60000);
  if (m < 1) return "just now";
  if (m < 60) return `${m} min`;
  return `${Math.floor(m / 60)} h ${m % 60} min`;
}
