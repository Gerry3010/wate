import type { Pane } from "../pane";
import { LayoutView } from "../layout/view";
import { boxAt, dropZone, zoneRect, zoneSplit, type DropZone } from "../drop";
import { DropHighlight } from "../drop-overlay";
import {
  focusAfterClose,
  leaves,
  nearestSplit,
  ratioForShare,
  ratioOf,
  removeLeaf,
  resizeTowards,
  setRatio,
  shareOf,
  splitLeaf,
  swapLeaves,
  neighbor,
  type Dir,
  type Direction,
  type LayoutNode,
  type Rect,
} from "../layout/tree";

/** Pointer travel before a press on the bar counts as a drag rather than a click. */
const DRAG_THRESHOLD = 4;

let seq = 0;
export const nextId = (prefix: string) => `${prefix}-${++seq}-${Math.random().toString(36).slice(2, 7)}`;

/** A tab owns a split tree of panes. */
export class Tab {
  readonly id = nextId("tab");
  readonly element = document.createElement("div");
  readonly panes = new Map<string, Pane>();
  /** One per attached pane, so moving a pane between tabs does not leave its old wiring on. */
  private wiring = new Map<string, AbortController>();
  tree: LayoutNode | null = null;
  focusedId: string | null = null;
  /** Fired after the layout settled, with the focused pane's rectangle (the sidebar edge follows it). */
  onLayout?: (focused: DOMRect | null) => void;
  /** User-given title (overrides the focused pane's title) and tab colour. */
  customTitle = "";
  color = "";
  private view: LayoutView;
  onChange?: () => void;

  constructor() {
    this.element.className = "tab-content";
    this.view = new LayoutView(this.element, {
      elementFor: (id) => this.panes.get(id)?.element,
      onRatio: (splitId, ratio) => {
        if (this.tree) this.tree = setRatio(this.tree, splitId, ratio);
      },
      onDragStart: () => {
        for (const p of this.panes.values()) p.setFitSuspended?.(true);
      },
      onResized: () => {
        for (const p of this.panes.values()) p.setFitSuspended?.(false);
        this.relayoutPanes();
      },
      onLines: (r) => this.onLayout?.(r),
    });
  }

  /** Keyboard resize: move the divider nearest to the focused pane. */
  resize(direction: Direction): void {
    if (!this.tree || !this.focusedId) return;
    const dir: Dir = direction === "left" || direction === "right" ? "row" : "col";
    const id = nearestSplit(this.tree, this.focusedId, dir);
    if (!id) return;
    this.tree = resizeTowards(this.tree, this.focusedId, direction);
    this.view.setRatio(id, ratioOf(this.tree, id)!);
    this.relayoutPanes();
  }

  /**
   * Give `paneId` exactly `share` (0..1) of the divider nearest to it along `dir`.
   *
   * The keyboard's resize() nudges by a step; this sets a value outright, which is what a
   * request for "a third of the width" means. `setRatio` keeps it inside 0.1..0.9, and
   * `shareOf` works out whether the pane is the half the ratio describes.
   */
  setSplitRatio(paneId: string, dir: Dir, share: number): boolean {
    if (!this.tree) return false;
    const s = shareOf(this.tree, paneId, dir);
    if (!s) return false;
    this.tree = setRatio(this.tree, s.splitId, ratioForShare(share, s.isFirst));
    this.view.setRatio(s.splitId, ratioOf(this.tree, s.splitId)!);
    this.relayoutPanes();
    return true;
  }

  private relayoutPanes(): void {
    for (const p of this.panes.values()) p.relayout();
  }

  get title(): string {
    if (this.customTitle) return this.customTitle;
    return this.autoTitle;
  }

  /** Title derived from the focused pane (shown as fallback / in the rename field). */
  get autoTitle(): string {
    const p = this.focusedId ? this.panes.get(this.focusedId) : undefined;
    return p?.title || "wate";
  }

  /** Tooltip text from the focused pane (the shell's own title and its full path). */
  get detail(): string {
    const p = this.focusedId ? this.panes.get(this.focusedId) : undefined;
    return p?.detail ?? "";
  }

  get focused(): Pane | undefined {
    return this.focusedId ? this.panes.get(this.focusedId) : undefined;
  }

  /**
   * Add a pane; splits the focused leaf (or `target`) or becomes the root. A split always appends,
   * so `before` — the new pane left of / above the old one — swaps the two leaves afterwards,
   * while the tree is still unrendered and nobody has seen the intermediate arrangement.
   */
  add(pane: Pane, dir: Dir = "row", target = this.focusedId, before = false): void {
    this.attach(pane);
    if (!this.tree || !target) {
      this.tree = { kind: "leaf", id: pane.id };
    } else {
      this.tree = splitLeaf(this.tree, target, dir, pane.id);
      if (before) this.tree = swapLeaves(this.tree, pane.id, target);
    }
    this.render();
    this.setFocus(pane.id);
  }

  /** Session restore: install a whole tree at once. */
  restore(tree: LayoutNode, panes: Pane[], focused: string | null): void {
    for (const p of panes) this.attach(p);
    this.tree = tree;
    this.render();
    const first = leaves(tree)[0];
    this.setFocus(focused && this.panes.has(focused) ? focused : first);
  }

  private attach(pane: Pane) {
    this.panes.set(pane.id, pane);
    pane.element.dataset.paneId = pane.id;
    // A pane can be attached twice — it moves between tabs without being rebuilt — so the old
    // tab's listeners have to come off first, or a moved pane would answer to both.
    this.wiring.get(pane.id)?.abort();
    const wiring = new AbortController();
    this.wiring.set(pane.id, wiring);
    const opts = { signal: wiring.signal };
    pane.element.addEventListener("focusin", () => this.setFocus(pane.id), opts);
    pane.element.addEventListener(
      "pointerdown",
      (e) => {
        this.setFocus(pane.id);
        // Alt + drag moves the pane: drop it on another pane to swap the two.
        if (e.altKey && e.button === 0 && !e.ctrlKey && !e.metaKey) this.startSwapDrag(pane.id, e);
      },
      { capture: true, signal: wiring.signal },
    );
  }

  /** Take a pane out without killing it: it is moving to another tab, not closing. */
  detachPane(id: string): Pane | undefined {
    const pane = this.panes.get(id);
    if (!pane) return undefined;
    const next = this.tree ? focusAfterClose(this.tree, id) : null;
    this.panes.delete(id);
    this.wiring.get(id)?.abort();
    this.wiring.delete(id);
    this.tree = this.tree ? removeLeaf(this.tree, id) : null;
    pane.element.remove();
    this.render();
    if (next) this.setFocus(next);
    else this.focusedId = null;
    this.onChange?.();
    return pane;
  }

  /** Exchange two panes' positions. */
  swap(a: string, b: string): void {
    if (!this.tree || a === b || !this.panes.has(a) || !this.panes.has(b)) return;
    this.tree = swapLeaves(this.tree, a, b);
    this.render();
    this.setFocus(a);
    this.onChange?.();
  }

  /** Swap the focused pane with its neighbour in a direction (keyboard). */
  swapDir(direction: Direction): void {
    if (!this.focusedId) return;
    const other = neighbor(this.rects(), this.focusedId, direction);
    if (other) this.swap(this.focusedId, other);
  }

  /** During a pane drag: let the app take over (a tab button, or another tab now in front). */
  onPaneDragMove?: (paneId: string, x: number, y: number) => boolean;
  /** The pane was let go while the app had taken over. */
  onPaneDragDrop?: (paneId: string, x: number, y: number) => void;

  /**
   * Drag a pane by its status bar: onto the middle of another to swap the two, onto an edge
   * to land there, onto another tab's button to move it over there.
   *
   * The preview is the highlight box and nothing else — the same idea as the file drop. The
   * first version rearranged the real layout on every zone change, which was accurate and
   * looked awful: the panes slid about under the pointer at every twitch.
   *
   * Nothing happens until the pointer has actually travelled, because this gesture starts on
   * the same bar that holds the menu button, and a click that quietly rebuilt the layout
   * would take the button out from under itself before it could be clicked.
   */
  startPaneDrag(id: string, start: PointerEvent): void {
    const source = this.panes.get(id)?.element;
    if (!source || !this.tree) return;
    // Measured once: nothing moves during the drag, so nothing needs measuring again.
    const boxes = this.rects();
    let armed = false;
    let highlight: DropHighlight | null = null;
    let landed: { target: string; zone: DropZone } | null = null;
    let elsewhere = false;
    let shown = "";

    const arm = () => {
      armed = true;
      highlight = new DropHighlight(document.body);
      source.classList.add("swap-source");
    };
    const preview = (x: number, y: number) => {
      elsewhere = this.onPaneDragMove?.(id, x, y) ?? false;
      if (elsewhere) {
        landed = null;
        shown = "away";
        highlight?.hide();
        return;
      }
      const box = boxAt(boxes, x, y);
      const h = box && box.id !== id ? { box, zone: dropZone(box, x, y) } : null;
      const key = h ? `${h.box.id}:${h.zone}` : "";
      if (key === shown) return;
      shown = key;
      landed = h ? { target: h.box.id, zone: h.zone } : null;
      if (h) highlight?.show(zoneRect(h.box, h.zone), h.zone);
      else highlight?.hide();
    };
    const cleanup = () => {
      window.removeEventListener("pointermove", move, true);
      window.removeEventListener("pointerup", finish, true);
      window.removeEventListener("pointercancel", cancel, true);
      window.removeEventListener("keydown", onKey, true);
      highlight?.dispose();
      source.classList.remove("swap-source");
    };
    const move = (e: PointerEvent) => {
      if (!armed) {
        if (Math.abs(e.clientX - start.clientX) < DRAG_THRESHOLD && Math.abs(e.clientY - start.clientY) < DRAG_THRESHOLD) return;
        arm();
      }
      e.preventDefault();
      preview(e.clientX, e.clientY);
    };
    const finish = (e: PointerEvent) => {
      // Never travelled: this was a click on the bar, and the bar's own buttons want it.
      if (!armed) {
        cleanup();
        return;
      }
      preview(e.clientX, e.clientY);
      const done = landed;
      const away = elsewhere;
      cleanup();
      if (away) this.onPaneDragDrop?.(id, e.clientX, e.clientY);
      else if (done) this.dropPane(id, done.target, done.zone);
    };
    const cancel = () => cleanup();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") cancel();
    };
    window.addEventListener("pointermove", move, true);
    window.addEventListener("pointerup", finish, true);
    window.addEventListener("pointercancel", cancel, true);
    window.addEventListener("keydown", onKey, true);
  }

  /** Put `id` where the drop said, in one move. */
  private dropPane(id: string, target: string, zone: DropZone): void {
    if (!this.tree || target === id) return;
    if (zone === "center") {
      this.tree = swapLeaves(this.tree, id, target);
    } else {
      const split = zoneSplit(zone);
      const without = removeLeaf(this.tree, id);
      if (!split || !without) return;
      const t = splitLeaf(without, target, split.dir, id);
      this.tree = split.before ? swapLeaves(t, id, target) : t;
    }
    this.render();
    this.setFocus(id);
    this.onChange?.();
  }

  private startSwapDrag(id: string, start: PointerEvent): void {
    start.preventDefault();
    start.stopPropagation();
    const source = this.panes.get(id)?.element;
    if (!source || !this.tree) return;
    const original = this.tree;
    source.classList.add("swap-source");
    for (const p of this.panes.values()) p.setFitSuspended?.(true);
    let target: string | null = null;
    const paneAt = (x: number, y: number): string | null => {
      const el = document.elementFromPoint(x, y)?.closest<HTMLElement>(".pane[data-pane-id]") ?? null;
      const pid = el?.dataset.paneId ?? "";
      return pid && pid !== id && this.panes.has(pid) ? pid : null;
    };
    // Live preview: the layout shows the swapped arrangement while hovering a target.
    const preview = (t: string | null) => {
      if (t === target) return;
      this.panes.get(target ?? "")?.element.classList.remove("swap-target");
      target = t;
      this.tree = t ? swapLeaves(original, id, t) : original;
      this.view.render(this.tree);
      this.panes.get(t ?? "")?.element.classList.add("swap-target");
    };
    const cleanup = () => {
      window.removeEventListener("pointermove", move, true);
      window.removeEventListener("pointerup", finish, true);
      window.removeEventListener("pointercancel", cancel, true);
      window.removeEventListener("keydown", onKey, true);
      source.classList.remove("swap-source");
      this.panes.get(target ?? "")?.element.classList.remove("swap-target");
      for (const p of this.panes.values()) p.setFitSuspended?.(false);
    };
    const move = (e: PointerEvent) => preview(paneAt(e.clientX, e.clientY));
    const finish = (e: PointerEvent) => {
      preview(paneAt(e.clientX, e.clientY));
      const dropped = target;
      cleanup();
      if (dropped) {
        this.render();
        this.setFocus(id);
        this.onChange?.();
      } else {
        this.tree = original;
        this.render();
      }
    };
    const cancel = () => {
      cleanup();
      this.tree = original;
      this.render();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") cancel();
    };
    window.addEventListener("pointermove", move, true);
    window.addEventListener("pointerup", finish, true);
    window.addEventListener("pointercancel", cancel, true);
    window.addEventListener("keydown", onKey, true);
  }

  remove(id: string): void {
    const pane = this.panes.get(id);
    if (!pane) return;
    const next = this.tree ? focusAfterClose(this.tree, id) : null;
    this.panes.delete(id);
    this.wiring.get(id)?.abort();
    this.wiring.delete(id);
    this.tree = this.tree ? removeLeaf(this.tree, id) : null;
    pane.dispose();
    this.render();
    if (next) this.setFocus(next);
    else this.focusedId = null;
    this.onChange?.();
  }

  setFocus(id: string): void {
    if (!this.panes.has(id)) return;
    const changed = this.focusedId !== id;
    this.focusedId = id;
    for (const [pid, p] of this.panes) {
      p.element.classList.toggle("focused", pid === id);
      p.setActive?.(pid === id);
    }
    this.view.scheduleLines();
    if (changed) this.onChange?.();
  }

  focusPane(): void {
    this.focused?.focus();
  }

  rects(): Rect[] {
    return Array.from(this.panes.values()).map((p) => {
      const r = p.element.getBoundingClientRect();
      return { id: p.id, x: r.left, y: r.top, w: r.width, h: r.height };
    });
  }

  get isEmpty(): boolean {
    return !this.tree || leaves(this.tree).length === 0;
  }

  /** A pane that is alone in its tab needs no label: it is the tab. */
  private updateBars(): void {
    const many = this.panes.size > 1;
    for (const p of this.panes.values()) p.setStatusVisible?.(many);
  }

  render(): void {
    this.updateBars();
    this.view.render(this.tree);
    requestAnimationFrame(() => this.relayoutPanes());
  }

  /**
   * Hand this tab's panes over: drop the DOM and the sockets, leave the shells running. The
   * window that adopts the tab re-attaches to the very same sessions, so detaching must not
   * do what dispose() does — that kills them.
   */
  detach(): void {
    for (const p of this.panes.values()) {
      const t = p as { detach?: () => void };
      if (typeof t.detach === "function") t.detach();
      else p.dispose();
    }
    this.panes.clear();
    this.element.remove();
  }

  dispose(): void {
    for (const p of this.panes.values()) p.dispose();
    this.panes.clear();
    this.element.remove();
  }
}
