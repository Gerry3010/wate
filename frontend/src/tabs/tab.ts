import type { Pane } from "../pane";
import { LayoutView } from "../layout/view";
import {
  focusAfterClose,
  leaves,
  nearestSplit,
  ratioOf,
  removeLeaf,
  resizeTowards,
  setRatio,
  splitLeaf,
  swapLeaves,
  neighbor,
  type Dir,
  type Direction,
  type LayoutNode,
  type Rect,
} from "../layout/tree";

let seq = 0;
export const nextId = (prefix: string) => `${prefix}-${++seq}-${Math.random().toString(36).slice(2, 7)}`;

/** A tab owns a split tree of panes. */
export class Tab {
  readonly id = nextId("tab");
  readonly element = document.createElement("div");
  readonly panes = new Map<string, Pane>();
  tree: LayoutNode | null = null;
  focusedId: string | null = null;
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

  get focused(): Pane | undefined {
    return this.focusedId ? this.panes.get(this.focusedId) : undefined;
  }

  /** Add a pane; splits the focused leaf or becomes the root. */
  add(pane: Pane, dir: Dir = "row", target = this.focusedId): void {
    this.attach(pane);
    if (!this.tree || !target) {
      this.tree = { kind: "leaf", id: pane.id };
    } else {
      this.tree = splitLeaf(this.tree, target, dir, pane.id);
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
    pane.element.addEventListener("focusin", () => this.setFocus(pane.id));
    pane.element.addEventListener(
      "pointerdown",
      (e) => {
        this.setFocus(pane.id);
        // Alt + drag moves the pane: drop it on another pane to swap the two.
        if (e.altKey && e.button === 0 && !e.ctrlKey && !e.metaKey) this.startSwapDrag(pane.id, e);
      },
      { capture: true },
    );
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

  render(): void {
    this.view.render(this.tree);
    requestAnimationFrame(() => this.relayoutPanes());
  }

  dispose(): void {
    for (const p of this.panes.values()) p.dispose();
    this.panes.clear();
    this.element.remove();
  }
}
