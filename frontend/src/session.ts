import type { LayoutNode } from "./layout/tree";

/** Serialized UI state (see StateService). Versioned so old files can be ignored safely. */
export interface SavedSession {
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
  | { id: string; kind: "terminal"; cwd: string; /** ANSI scrollback replayed before the shell starts. */ replay?: string }
  | { id: string; kind: "editor"; path: string };

export function parseSession(raw: string): SavedSession | null {
  if (!raw) return null;
  try {
    const s = JSON.parse(raw) as SavedSession;
    if (s.version !== 1 || !Array.isArray(s.tabs) || s.tabs.length === 0) return null;
    return s;
  } catch {
    return null;
  }
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
