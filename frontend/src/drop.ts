/**
 * Files dropped onto the window.
 *
 * The paths do not come from the WebView: WebKitGTK reports a file drop with an empty
 * DataTransfer, so the OS drop is taken natively and arrives as the `window:drop` event with the
 * paths and the point they landed on (see internal/app/drop.go). What the point means is decided
 * here — the middle of a pane types the paths into it, the edges split it towards that side —
 * and the DOM handlers stay as a fallback for engines that do hand over the paths.
 *
 * Nothing in this module touches the DOM, so all of it is testable.
 */
import type { Dir } from "./layout/tree";

/** Paths carried by a drop: the uri-list (`file://` only), or plain text that looks like a path. */
export function droppedPaths(dt: DataTransfer | null): string[] {
  if (!dt) return [];
  const list = dt.getData("text/uri-list") || dt.getData("text/plain") || "";
  const paths: string[] = [];
  for (const raw of list.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue; // uri-list comments
    const m = /^file:\/\/[^/]*(\/.*)$/.exec(line);
    if (m) {
      try {
        paths.push(decodeURIComponent(m[1]));
      } catch {
        paths.push(m[1]); // a stray % in the name: better the raw path than nothing
      }
    } else if (line.startsWith("/") || line.startsWith("~/")) {
      paths.push(line);
    }
  }
  return paths;
}

/** Shell-quoted, so a name with spaces or quotes survives the paste: `'it'\''s here.png'`. */
export function quotePath(path: string): string {
  return /^[A-Za-z0-9_@%+=:,./-]+$/.test(path) ? path : `'${path.replace(/'/g, `'\\''`)}'`;
}

/** What the drop types into the shell: the quoted paths, with a trailing space to type on. */
export function dropText(paths: string[]): string {
  return paths.length === 0 ? "" : paths.map(quotePath).join(" ") + " ";
}

/** The middle of a pane, or the side a drop wants the pane split towards. */
export type DropZone = "center" | "left" | "right" | "up" | "down";

/** A pane's place on screen — the shape `Tab.rects()` hands out. */
export interface Box {
  x: number;
  y: number;
  w: number;
  h: number;
}

/**
 * How wide the edge band is: a quarter of the side, but never thinner than a comfortable target
 * and never so wide that a large pane is all edge and no middle.
 */
export const EDGE = { fraction: 0.25, min: 24, max: 140 } as const;

function band(side: number, edge: typeof EDGE): number {
  // Half the side is the hard ceiling: below ~100px the min would otherwise eat the middle.
  return Math.min(Math.max(side * edge.fraction, Math.min(edge.min, side / 4)), edge.max, side / 2);
}

/**
 * Where in a pane a drop landed. The point is measured against each of the four bands and the one
 * it reaches deepest into wins, so a corner resolves to a single, predictable side.
 */
export function dropZone(box: Box, x: number, y: number, edge = EDGE): DropZone {
  if (box.w <= 0 || box.h <= 0) return "center";
  const px = Math.min(Math.max(x, box.x), box.x + box.w);
  const py = Math.min(Math.max(y, box.y), box.y + box.h);
  const bx = band(box.w, edge);
  const by = band(box.h, edge);
  // 0 at the edge, 1 where the band ends; >= 1 everywhere in the middle.
  const into: [DropZone, number][] = [
    ["left", (px - box.x) / bx],
    ["right", (box.x + box.w - px) / bx],
    ["up", (py - box.y) / by],
    ["down", (box.y + box.h - py) / by],
  ];
  let best = into[0];
  for (const candidate of into) if (candidate[1] < best[1]) best = candidate;
  return best[1] >= 1 ? "center" : best[0];
}

/** The area a zone covers: the whole pane for the middle, the half it would split off otherwise. */
export function zoneRect(box: Box, zone: DropZone): Box {
  switch (zone) {
    case "left":
      return { ...box, w: box.w / 2 };
    case "right":
      return { ...box, x: box.x + box.w / 2, w: box.w / 2 };
    case "up":
      return { ...box, h: box.h / 2 };
    case "down":
      return { ...box, y: box.y + box.h / 2, h: box.h / 2 };
    default:
      return { ...box };
  }
}

/** How a zone splits its pane; `before` means the new pane takes the first slot (left/up). */
export function zoneSplit(zone: DropZone): { dir: Dir; before: boolean } | null {
  switch (zone) {
    case "left":
      return { dir: "row", before: true };
    case "right":
      return { dir: "row", before: false };
    case "up":
      return { dir: "col", before: true };
    case "down":
      return { dir: "col", before: false };
    default:
      return null;
  }
}

/** The first box containing the point — panes do not overlap, so the first hit is the one. */
export function boxAt<T extends Box>(boxes: T[], x: number, y: number): T | undefined {
  return boxes.find((b) => x >= b.x && x < b.x + b.w && y >= b.y && y < b.y + b.h);
}
