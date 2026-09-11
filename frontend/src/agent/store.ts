import type { Session } from "../api";

export type AgentStatus = "idle" | "running" | "waiting" | "done";

/** Client-side mirror of the backend's session table. */
export class AgentStore {
  readonly sessions = new Map<string, Session>();
  private listeners = new Set<() => void>();

  apply(s: Session) {
    if (s.status === "idle") {
      if (!this.sessions.delete(s.pane_id)) return;
    } else {
      const known = this.sessions.get(s.pane_id);
      // The poller re-sends a session whenever anything in it moves; an identical one would
      // only cost the UI a full repaint.
      if (known && JSON.stringify(known) === JSON.stringify(s)) return;
      this.sessions.set(s.pane_id, s);
    }
    this.emit();
  }

  replaceAll(list: Session[]) {
    this.sessions.clear();
    for (const s of list) if (s.status !== "idle") this.sessions.set(s.pane_id, s);
    this.emit();
  }

  forPane(paneId: string): Session | undefined {
    return this.sessions.get(paneId);
  }

  /** Most urgent status among a tab's panes: waiting > done > running. */
  forTab(tabId: string): AgentStatus {
    let best: AgentStatus = "idle";
    const rank: Record<AgentStatus, number> = { idle: 0, running: 1, done: 2, waiting: 3 };
    for (const s of this.sessions.values()) {
      if (s.tab_id === tabId && rank[s.status as AgentStatus] > rank[best]) best = s.status as AgentStatus;
    }
    return best;
  }

  list(): Session[] {
    return Array.from(this.sessions.values());
  }

  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private emit() {
    for (const fn of this.listeners) fn();
  }
}
