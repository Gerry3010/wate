/** Pure split-tree model for a tab's panes. No DOM here so it stays unit-testable. */

/** "row" = panes side by side, "col" = stacked. */
export type Dir = "row" | "col";

export interface Leaf {
  kind: "leaf";
  id: string;
}

export interface Split {
  kind: "split";
  id: string;
  dir: Dir;
  /** Share of the first child (0..1). */
  ratio: number;
  a: LayoutNode;
  b: LayoutNode;
}

export type LayoutNode = Leaf | Split;

export const leaf = (id: string): Leaf => ({ kind: "leaf", id });

export function leaves(n: LayoutNode): string[] {
  return n.kind === "leaf" ? [n.id] : [...leaves(n.a), ...leaves(n.b)];
}

/** Replace the leaf `targetId` by a split with the new leaf after it (right of / below). */
export function splitLeaf(root: LayoutNode, targetId: string, dir: Dir, newId: string, splitId = newId + ":split"): LayoutNode {
  if (root.kind === "leaf") {
    if (root.id !== targetId) return root;
    return { kind: "split", id: splitId, dir, ratio: 0.5, a: root, b: leaf(newId) };
  }
  return { ...root, a: splitLeaf(root.a, targetId, dir, newId, splitId), b: splitLeaf(root.b, targetId, dir, newId, splitId) };
}

/** Remove a leaf; its parent split collapses into the sibling. Returns null when the tree is empty. */
export function removeLeaf(root: LayoutNode, id: string): LayoutNode | null {
  if (root.kind === "leaf") return root.id === id ? null : root;
  const a = removeLeaf(root.a, id);
  const b = removeLeaf(root.b, id);
  if (a === null) return b;
  if (b === null) return a;
  return { ...root, a, b };
}

export function setRatio(root: LayoutNode, splitId: string, ratio: number): LayoutNode {
  if (root.kind === "leaf") return root;
  const r = Math.min(0.9, Math.max(0.1, ratio));
  if (root.id === splitId) return { ...root, ratio: r };
  return { ...root, a: setRatio(root.a, splitId, ratio), b: setRatio(root.b, splitId, ratio) };
}

/** The leaf that should receive focus when `id` is closed: the nearest one in its sibling subtree. */
export function focusAfterClose(root: LayoutNode, id: string): string | null {
  const parent = parentOf(root, id);
  if (!parent) {
    const rest = leaves(root).filter((l) => l !== id);
    return rest[0] ?? null;
  }
  const sibling = parent.a.kind === "leaf" && parent.a.id === id ? parent.b : parent.a;
  return leaves(sibling)[0] ?? null;
}

function parentOf(root: LayoutNode, id: string): Split | null {
  if (root.kind === "leaf") return null;
  if ((root.a.kind === "leaf" && root.a.id === id) || (root.b.kind === "leaf" && root.b.id === id)) return root;
  return parentOf(root.a, id) ?? parentOf(root.b, id);
}

export interface Rect {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

export type Direction = "left" | "right" | "up" | "down";

/**
 * Geometric focus navigation (tmux/i3 style): the closest pane whose edge touches
 * `from` in the given direction and overlaps it on the other axis; falls back to
 * the nearest pane in that direction by centre distance.
 */
export function neighbor(rects: Rect[], fromId: string, dir: Direction): string | null {
  const from = rects.find((r) => r.id === fromId);
  if (!from) return null;
  const horizontal = dir === "left" || dir === "right";
  const ahead = (r: Rect): number => {
    switch (dir) {
      case "right": return r.x - (from.x + from.w);
      case "left": return from.x - (r.x + r.w);
      case "down": return r.y - (from.y + from.h);
      case "up": return from.y - (r.y + r.h);
    }
  };
  const overlap = (r: Rect): number =>
    horizontal
      ? Math.min(from.y + from.h, r.y + r.h) - Math.max(from.y, r.y)
      : Math.min(from.x + from.w, r.x + r.w) - Math.max(from.x, r.x);

  const candidates = rects.filter((r) => r.id !== fromId && ahead(r) >= -1);
  if (candidates.length === 0) return null;
  const touching = candidates.filter((r) => overlap(r) > 0);
  const pool = touching.length ? touching : candidates;
  pool.sort((p, q) => {
    const d = ahead(p) - ahead(q);
    if (Math.abs(d) > 1) return d;
    return overlap(q) - overlap(p);
  });
  return pool[0].id;
}

/** Id of the nearest ancestor split of `leafId` with the given orientation (the divider to move). */
export function nearestSplit(root: LayoutNode, leafId: string, dir: Dir): string | null {
  const path = pathTo(root, leafId);
  if (!path) return null;
  for (let i = path.length - 1; i >= 0; i--) {
    const n = path[i];
    if (n.kind === "split" && n.dir === dir) return n.id;
  }
  return null;
}

function pathTo(n: LayoutNode, leafId: string): LayoutNode[] | null {
  if (n.kind === "leaf") return n.id === leafId ? [n] : null;
  const a = pathTo(n.a, leafId);
  if (a) return [n, ...a];
  const b = pathTo(n.b, leafId);
  return b ? [n, ...b] : null;
}

export function ratioOf(root: LayoutNode, splitId: string): number | null {
  if (root.kind === "leaf") return null;
  if (root.id === splitId) return root.ratio;
  return ratioOf(root.a, splitId) ?? ratioOf(root.b, splitId);
}

/**
 * Move the divider nearest to `leafId` one step in `direction`
 * (right/down grow the first child). Returns the unchanged tree at the edge.
 */
export function resizeTowards(root: LayoutNode, leafId: string, direction: Direction, step = 0.05): LayoutNode {
  const dir: Dir = direction === "left" || direction === "right" ? "row" : "col";
  const id = nearestSplit(root, leafId, dir);
  if (!id) return root;
  const r = ratioOf(root, id)!;
  const delta = direction === "right" || direction === "down" ? step : -step;
  return setRatio(root, id, r + delta);
}

/** Preferred divider positions for snapping. */
export const SNAP_POINTS = [0.25, 1 / 3, 0.5, 2 / 3, 0.75];

/** Snap `ratio` to the closest snap point within `threshold`, else return it unchanged. */
export function snapRatio(ratio: number, threshold = 0.02): { ratio: number; snapped: number | null } {
  for (const p of SNAP_POINTS) {
    if (Math.abs(ratio - p) <= threshold) return { ratio: p, snapped: p };
  }
  return { ratio, snapped: null };
}

/** Exchange two leaves (their positions in the tree); unknown ids leave the tree unchanged. */
export function swapLeaves(root: LayoutNode, a: string, b: string): LayoutNode {
  const ids = leaves(root);
  if (a === b || !ids.includes(a) || !ids.includes(b)) return root;
  const walk = (n: LayoutNode): LayoutNode => {
    if (n.kind === "leaf") {
      if (n.id === a) return leaf(b);
      if (n.id === b) return leaf(a);
      return n;
    }
    return { ...n, a: walk(n.a), b: walk(n.b) };
  };
  return walk(root);
}
