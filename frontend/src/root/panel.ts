import type { TerminalPane } from "../terminal/pane";

/** How the root agent's panel opens over the tabs ([root] layout). */
export type RootLayout = "drawer" | "overlay" | "top";

export const ROOT_LAYOUTS: [RootLayout, string][] = [
  ["drawer", "Drawer from the right"],
  ["overlay", "Centred overlay"],
  ["top", "Drop down from the top"],
];

/** The layout to use for a configured value; anything unknown is a drawer. */
export function rootLayout(value: string | undefined): RootLayout {
  return ROOT_LAYOUTS.some(([l]) => l === value) ? (value as RootLayout) : "drawer";
}

/** Whether a layout can be docked: the centred overlay always floats. */
export function dockable(layout: RootLayout): boolean {
  return layout !== "overlay";
}

/** The panel's size in percent of the window, one per layout ([root] drawer_width and so on). */
export interface PanelSizes {
  drawer_width: number;
  top_height: number;
  overlay_width: number;
  overlay_height: number;
}

/** The smallest panel in pixels, and the largest in percent of the window. */
export const PANEL_MIN_WIDTH = 320;
export const PANEL_MIN_HEIGHT = 160;
export const PANEL_MAX_PERCENT = 85;

/** An edge or corner of the panel, by compass point. */
export type Edge = "n" | "s" | "e" | "w" | "ne" | "nw" | "se" | "sw";

/**
 * The edges a layout can be dragged by. The drawer hangs on the right and the drop-down under
 * the tab bar, so each has one free edge; the overlay can be taken by any.
 */
export function edgesFor(layout: RootLayout): Edge[] {
  switch (layout) {
    case "drawer":
      return ["w"];
    case "top":
      return ["s"];
    case "overlay":
      return ["n", "s", "e", "w", "ne", "nw", "se", "sw"];
  }
}

/** The sizes a layout uses, and so the ones dragging it changes. */
export function sizeKeys(layout: RootLayout): (keyof PanelSizes)[] {
  switch (layout) {
    case "drawer":
      return ["drawer_width"];
    case "top":
      return ["top_height"];
    case "overlay":
      return ["overlay_width", "overlay_height"];
  }
}

/** px along an axis of the given extent, kept within the limits, in percent to one decimal. */
function percentOf(px: number, extent: number, minPx: number): number {
  const max = (extent * PANEL_MAX_PERCENT) / 100;
  const clamped = Math.min(Math.max(px, Math.min(minPx, max)), max);
  return Math.round((clamped / extent) * 1000) / 10;
}

export interface Rect {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/**
 * The sizes after dragging edge to the pointer at (x, y). rect is where the panel was when the
 * drag began. The overlay stays centred, so it grows by as much on the far side as on the near.
 */
export function dragSizes(
  layout: RootLayout,
  edge: Edge,
  rect: Rect,
  x: number,
  y: number,
  win: { width: number; height: number },
  sizes: PanelSizes,
): PanelSizes {
  const out = { ...sizes };
  switch (layout) {
    case "drawer":
      out.drawer_width = percentOf(rect.right - x, win.width, PANEL_MIN_WIDTH);
      break;
    case "top":
      out.top_height = percentOf(y - rect.top, win.height, PANEL_MIN_HEIGHT);
      break;
    case "overlay":
      if (edge.includes("e") || edge.includes("w")) {
        out.overlay_width = percentOf(2 * Math.abs(x - (rect.left + rect.right) / 2), win.width, PANEL_MIN_WIDTH);
      }
      if (edge.includes("n") || edge.includes("s")) {
        out.overlay_height = percentOf(2 * Math.abs(y - (rect.top + rect.bottom) / 2), win.height, PANEL_MIN_HEIGHT);
      }
      break;
  }
  return out;
}

/**
 * The root agent's panel. It lies over the tabs rather than in one: the root agent belongs to no
 * project, and a tab of its own was one more thing to scroll past. A small button in the corner
 * (and the Claude sidebar, and a key) brings it up; it holds the one pane the agent runs in.
 * Docked, it sits beside the terminals instead, and they make room for it while it is open.
 */
export class RootPanel {
  readonly element = document.createElement("div");
  readonly fab = document.createElement("button");
  private body = document.createElement("div");
  private dot = document.createElement("span");
  private handles: HTMLElement[] = [];
  private layout: RootLayout = "drawer";
  private sizes: PanelSizes = { drawer_width: 45, top_height: 50, overlay_width: 72, overlay_height: 78 };
  pane?: TerminalPane;
  /** Called after it was shown or hidden, so the focus can go where it belongs. */
  onVisibility?: (open: boolean) => void;
  /** Called when a drag has ended, with the sizes it changed (to be remembered). */
  onResize?: (sizes: Partial<PanelSizes>) => void;

  constructor(label: string) {
    this.element.className = "root-panel root-drawer";
    this.element.hidden = true;
    this.body.className = "root-panel-body";
    this.element.append(this.body);
    for (const edge of edgesFor("overlay")) {
      const h = document.createElement("div");
      h.className = "root-resize";
      h.dataset.edge = edge;
      h.addEventListener("pointerdown", (e) => this.drag(e, h, edge));
      this.handles.push(h);
    }
    this.element.append(...this.handles);
    this.applySizes();

    this.fab.className = "root-fab";
    this.fab.hidden = true;
    this.fab.title = label ? `Root agent (${label})` : "Root agent";
    this.dot.className = "status-dot";
    const name = document.createElement("span");
    name.textContent = "Root";
    this.fab.append(this.dot, name);
    // The focus stays where it was until the panel takes it.
    this.fab.addEventListener("mousedown", (e) => e.preventDefault());
    this.fab.addEventListener("click", () => this.toggle());
  }

  get open(): boolean {
    return !this.element.hidden;
  }

  mount(pane: TerminalPane) {
    this.pane = pane;
    this.body.replaceChildren(pane.element);
    this.fab.hidden = false;
  }

  /** The agent ended: nothing left to show. */
  unmount() {
    this.hide();
    this.pane = undefined;
    this.body.replaceChildren();
    this.fab.hidden = true;
  }

  show() {
    if (!this.pane) return;
    const was = this.open;
    this.element.hidden = false;
    this.fab.classList.add("open");
    this.pane.setVisible(true);
    this.pane.relayout();
    this.pane.focus();
    if (!was) this.onVisibility?.(true);
  }

  hide() {
    if (!this.open) return;
    this.element.hidden = true;
    this.fab.classList.remove("open");
    this.pane?.setVisible(false);
    this.onVisibility?.(false);
  }

  toggle() {
    if (this.open) this.hide();
    else this.show();
  }

  /** Whether it takes part in the layout rather than lying over it. */
  get docked(): boolean {
    return this.element.classList.contains("docked");
  }

  /** Where it docks: "side" (right of the terminals), "top" (above them), or "" when it floats. */
  get dockedAt(): "side" | "top" | "" {
    if (!this.docked) return "";
    return this.layout === "top" ? "top" : "side";
  }

  setLayout(layout: RootLayout, docked = false) {
    this.layout = layout;
    for (const [l] of ROOT_LAYOUTS) this.element.classList.toggle(`root-${l}`, l === layout);
    this.element.classList.toggle("docked", docked && dockable(layout));
    const edges = edgesFor(layout);
    for (const h of this.handles) h.hidden = !edges.includes(h.dataset.edge as Edge);
    if (this.open) this.pane?.relayout();
  }

  setSizes(sizes: PanelSizes) {
    const { drawer_width, top_height, overlay_width, overlay_height } = sizes;
    this.sizes = { drawer_width, top_height, overlay_width, overlay_height };
    this.applySizes();
  }

  private applySizes() {
    const s = this.element.style;
    s.setProperty("--root-drawer-w", `${this.sizes.drawer_width}vw`);
    s.setProperty("--root-top-h", `${this.sizes.top_height}vh`);
    s.setProperty("--root-overlay-w", `${this.sizes.overlay_width}vw`);
    s.setProperty("--root-overlay-h", `${this.sizes.overlay_height}vh`);
  }

  /** Follow the pointer from an edge until the button comes up; the terminal refits as it goes. */
  private drag(e: PointerEvent, handle: HTMLElement, edge: Edge) {
    if (e.button !== 0) return;
    e.preventDefault();
    handle.setPointerCapture(e.pointerId);
    const r = this.element.getBoundingClientRect();
    const rect = { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
    const layout = this.layout;
    const before = this.sizes;
    this.element.classList.add("resizing");
    handle.classList.add("active");
    const move = (ev: PointerEvent) => {
      this.sizes = dragSizes(layout, edge, rect, ev.clientX, ev.clientY, { width: window.innerWidth, height: window.innerHeight }, this.sizes);
      this.applySizes();
    };
    const end = () => {
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", end);
      handle.removeEventListener("pointercancel", end);
      this.element.classList.remove("resizing");
      handle.classList.remove("active");
      // A click on the edge is no reason to write the config.
      const keys = sizeKeys(layout).filter((k) => this.sizes[k] !== before[k]);
      if (keys.length) this.onResize?.(Object.fromEntries(keys.map((k) => [k, this.sizes[k]])));
      this.pane?.focus();
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", end);
    handle.addEventListener("pointercancel", end);
  }

  /** The agent's state for the button's dot: running / waiting / done / "". */
  setStatus(status: string) {
    this.fab.className = "root-fab" + (status ? ` status-${status}` : "") + (this.open ? " open" : "");
    this.dot.hidden = !status;
  }
}
