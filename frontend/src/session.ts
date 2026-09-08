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
}

export type SavedPane = { id: string; kind: "terminal"; cwd: string } | { id: string; kind: "editor"; path: string };

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
