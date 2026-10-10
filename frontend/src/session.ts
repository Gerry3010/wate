import type { LayoutNode } from "./layout/tree";

/**
 * Serialized UI state (see StateService). Versioned so old files can be ignored safely.
 *
 * v2 added the window layer. v1 files are migrated on read rather than discarded — they hold
 * the user's tabs, and throwing them away on an upgrade is not an acceptable way to find out
 * the format changed.
 */
export interface SavedSession {
  version: 2;
  windows: SavedWindow[];
}

/** One window's tabs. This is also what a named session stores, minus the key. */
export interface SavedWindow {
  /** Stable across runs; matches the window's record in window.json. */
  key: string;
  /** Index into tabs. */
  active: number;
  tabs: SavedTab[];
}

/** The v1 shape, kept as documentation of what parseSession still accepts on input. */
export interface SavedSessionV1 {
  version: 1;
  active: number;
  tabs: SavedTab[];
}

export interface SavedTab {
  tree: LayoutNode;
  focused: string | null;
  panes: SavedPane[];
  /** User-given title and colour (tab bar), optional. */
  title?: string;
  color?: string;
}

export type SavedPane =
  | {
      id: string;
      kind: "terminal";
      cwd: string;
      /** ANSI scrollback replayed before the shell starts. */
      replay?: string;
      /** A Claude Code session that was running here (offered for resume on restore). */
      claude?: SavedClaude;
    }
  | { id: string; kind: "editor"; path: string };

/**
 * The resume offers panes were restored with, held until a live session takes over.
 *
 * A pane that came back out of the state file has no Claude running in it: the session it
 * points at died with the last wate, and the offer to resume it is the only trace left. The
 * save that follows a restart looks at the live sessions, finds none for that pane, and —
 * before this existed — wrote the pane out with no pointer at all. So the offer survived
 * exactly one restart and was gone by the second, which is precisely when you would want it.
 */
export class ResumeOffers {
  private byPane = new Map<string, SavedClaude>();

  /** Take note of what a restored pane was pointing at. */
  remember(pane: string, c: SavedClaude | undefined) {
    if (c) this.byPane.set(pane, c);
  }

  forget(pane: string) {
    this.byPane.delete(pane);
  }

  /**
   * What to write for a pane, given whatever is running in it now.
   *
   * A live session always wins, and it also retires the old offer: once Claude has run here,
   * the pointer from before the restart is a worse answer than the one in front of us, and
   * keeping it would mean offering a dead session again the next time this pane falls idle.
   */
  forSave(pane: string, live: SavedClaude | undefined): SavedClaude | undefined {
    if (live) {
      this.byPane.delete(pane);
      return live;
    }
    return this.byPane.get(pane);
  }
}

/** The Claude Code session a pane held when the state was saved. */
export interface SavedClaude {
  /** Session title: its transcript summary, or the first prompt. */
  title: string;
  /** Claude Code session id, for `claude --resume <id>`. */
  sessionId?: string;
  /** Where it ran, which model, how full its context was — for the block's second line. */
  cwd?: string;
  model?: string;
  contextPercent?: number;
}

/** One window's worth of saved tabs — what a window slot and a named session both hold. */
export function parseWindow(raw: string): SavedWindow | null {
  if (!raw) return null;
  try {
    return asWindow(JSON.parse(raw), "");
  } catch {
    return null;
  }
}

/** Accepts v1 and v2 on input; always answers in the v2 shape. */
export function parseSession(raw: string): SavedSession | null {
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  const s = parsed as { version?: number; windows?: unknown[] };
  let windows: SavedWindow[] = [];
  if (s.version === 2 && Array.isArray(s.windows)) {
    windows = s.windows
      .map((w, i) => asWindow(w, (w as SavedWindow | null)?.key || `w-${i + 1}`))
      .filter((w): w is SavedWindow => w !== null);
  } else if (s.version === 1) {
    const one = asWindow(parsed, "w-1");
    if (one) windows = [one];
  } else {
    return null;
  }
  return windows.length ? { version: 2, windows } : null;
}

/** A window is only worth restoring if it has tabs; an empty one would open a blank window. */
function asWindow(v: unknown, key: string): SavedWindow | null {
  const w = v as Partial<SavedWindow> | null;
  if (!w || !Array.isArray(w.tabs) || w.tabs.length === 0) return null;
  return { key: w.key || key, active: typeof w.active === "number" ? w.active : 0, tabs: w.tabs };
}

/** Rewrite leaf ids in a saved tree through `map` (old pane id → new pane id). */
export function remapTree(n: LayoutNode, map: Map<string, string>): LayoutNode | null {
  if (n.kind === "leaf") {
    const id = map.get(n.id);
    return id ? { kind: "leaf", id } : null;
  }
  const a = remapTree(n.a, map);
  const b = remapTree(n.b, map);
  if (!a) return b;
  if (!b) return a;
  return { ...n, a, b };
}

/** Layout as delivered by the importer (leaves carry a cwd instead of a pane id). */
export interface ImportedNode {
  kind: string;
  cwd?: string;
  dir?: string;
  ratio?: number;
  a?: ImportedNode;
  b?: ImportedNode;
  focused?: boolean;
  history?: string;
}

export interface ImportedTab {
  title: string;
  color: string;
  group?: string;
  root: ImportedNode | null;
}

/** Convert imported tabs into the saved-session shape so the normal restore path opens them. */
export function fromImported(tabs: ImportedTab[]): SavedTab[] {
  let seq = 0;
  const out: SavedTab[] = [];
  for (const t of tabs) {
    if (!t.root) continue;
    const panes: SavedPane[] = [];
    let focused: string | null = null;
    const walk = (n: ImportedNode): LayoutNode => {
      if (n.kind === "split" && n.a && n.b) {
        const id = `imp-split-${++seq}`;
        return { kind: "split", id, dir: n.dir === "col" ? "col" : "row", ratio: clampRatio(n.ratio), a: walk(n.a), b: walk(n.b) };
      }
      const id = `imp-pane-${++seq}`;
      panes.push({ id, kind: "terminal", cwd: n.cwd ?? "", replay: n.history || undefined });
      if (n.focused && !focused) focused = id;
      return { kind: "leaf", id };
    };
    const tree = walk(t.root);
    const title = t.group && t.title ? `${t.group} › ${t.title}` : t.title || t.group || "";
    out.push({ tree, focused: focused ?? panes[0]?.id ?? null, panes, title: title || undefined, color: t.color || undefined });
  }
  return out;
}

function clampRatio(r: number | undefined): number {
  if (typeof r !== "number" || !Number.isFinite(r)) return 0.5;
  return Math.min(0.9, Math.max(0.1, r));
}
