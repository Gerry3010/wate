import type { Session } from "../api";
import { showMenuAbove } from "../ui/menu";
import { STATUS_LABEL, elapsed, fmtTokens, shortPath } from "../sidebar/sidebar";
import { CLAUDE_LOGO } from "../ui/icons";

export interface BadgeHost {
  /** Latest state of the pane's session (undefined once it ended). */
  session(): Session | undefined;
  /** "All sessions →": open the sidebar. */
  allSessions(): void;
}

/**
 * Keep the pane's Claude badge (bottom-right corner) in sync with its session: created when
 * Claude is detected, restyled on status changes, removed when the session ends.
 */
export function updateBadge(pane: HTMLElement, s: Session | undefined, host: BadgeHost) {
  let badge = pane.querySelector<HTMLButtonElement>(":scope > .pane-claude");
  if (!s) {
    badge?.remove();
    return;
  }
  if (!badge) {
    badge = document.createElement("button");
    badge.className = "pane-claude";
    badge.innerHTML = CLAUDE_LOGO;
    // Never take the focus away from the terminal.
    badge.addEventListener("mousedown", (e) => {
      e.preventDefault();
      e.stopPropagation();
    });
    badge.addEventListener("click", (e) => {
      e.stopPropagation();
      const cur = host.session();
      if (cur) showAgentPopover(cur, badge!.getBoundingClientRect(), host);
    });
    pane.appendChild(badge);
  }
  badge.className = `pane-claude status-${s.status}`;
  const pct = s.context_percent ? ` · context ${s.context_percent} %` : "";
  badge.title = `Claude Code: ${STATUS_LABEL[s.status] ?? s.status}${pct}`;
}

/** The popup behind the badge: what the session is, how it is doing, how full its context is. */
export function showAgentPopover(s: Session, anchor: DOMRect, host: BadgeHost) {
  const box = document.createElement("div");
  box.className = "agent-pop";

  const head = document.createElement("div");
  head.className = "agent-pop-head";
  const logo = document.createElement("span");
  logo.className = `agent-pop-logo status-${s.status}`;
  logo.innerHTML = CLAUDE_LOGO;
  const title = document.createElement("div");
  title.className = "agent-pop-title";
  title.textContent = s.title || "Claude Code";
  title.title = s.title;
  const state = document.createElement("div");
  state.className = `agent-pop-state status-${s.status}`;
  state.textContent = [STATUS_LABEL[s.status] ?? s.status, elapsed(s.started_at)].filter(Boolean).join(" · ");
  const headText = document.createElement("div");
  headText.className = "agent-pop-headtext";
  headText.append(title, state);
  head.append(logo, headText);
  box.appendChild(head);

  const facts = document.createElement("dl");
  facts.className = "agent-pop-facts";
  const fact = (k: string, v: string, mono = false) => {
    if (!v) return;
    const dt = document.createElement("dt");
    dt.textContent = k;
    const dd = document.createElement("dd");
    dd.textContent = v;
    dd.title = v;
    if (mono) dd.classList.add("mono");
    facts.append(dt, dd);
  };
  fact("Directory", shortPath(s.cwd), true);
  fact("Model", s.context?.model ?? "", true);
  fact("Session", s.session_id ? s.session_id.slice(0, 8) + "…" : "", true);
  if (s.message) fact("Last", s.message);
  box.appendChild(facts);

  const ctx = s.context;
  if (ctx?.tokens && ctx.window) {
    const pct = s.context_percent || Math.round((ctx.tokens / ctx.window) * 100);
    const sec = document.createElement("div");
    sec.className = "agent-pop-ctx" + (pct >= 80 ? " hot" : pct >= 60 ? " warn" : "");
    const label = document.createElement("div");
    label.className = "agent-pop-ctxlabel";
    label.innerHTML = `<span>Context</span><span>${fmtTokens(ctx.tokens)} / ${fmtTokens(ctx.window)} · <b>${pct} %</b></span>`;
    const bar = document.createElement("div");
    bar.className = "sidebar-ctx-bar";
    const fill = document.createElement("div");
    fill.className = "sidebar-ctx-fill";
    fill.style.width = `${Math.min(100, pct)}%`;
    bar.appendChild(fill);
    sec.append(label, bar);
    // Where the tokens come from: the cache split says how cheap the next turn will be.
    const parts: [string, number][] = [
      ["cached", ctx.cache_read],
      ["cache write", ctx.cache_creation],
      ["fresh input", ctx.input],
      ["output", ctx.output],
    ];
    const split = document.createElement("div");
    split.className = "agent-pop-split";
    for (const [name, n] of parts) {
      if (!n) continue;
      const cell = document.createElement("span");
      cell.innerHTML = `<b>${fmtTokens(n)}</b> ${name}`;
      split.appendChild(cell);
    }
    if (split.childElementCount) sec.appendChild(split);
    const hint = document.createElement("div");
    hint.className = "agent-pop-hint";
    hint.textContent = pct >= 80 ? "Close to the limit — /compact soon, or let auto-compact handle it." : pct >= 60 ? "Getting full; /compact when a task is done keeps answers sharp." : "Plenty of room.";
    sec.appendChild(hint);
    box.appendChild(sec);
  }

  const all = document.createElement("a");
  all.className = "agent-pop-all settings-link";
  all.href = "#";
  all.textContent = "All sessions →";
  all.addEventListener("click", (e) => {
    e.preventDefault();
    host.allSessions();
  });
  box.appendChild(all);

  showMenuAbove(anchor, [{ element: box }]);
}
