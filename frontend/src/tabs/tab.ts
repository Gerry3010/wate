import type { Pane } from "../pane";
import { LayoutView } from "../layout/view";
import { focusAfterClose, leaves, removeLeaf, setRatio, splitLeaf, type Dir, type LayoutNode, type Rect } from "../layout/tree";

let seq = 0;
export const nextId = (prefix: string) => `${prefix}-${++seq}-${Math.random().toString(36).slice(2, 7)}`;

/** A tab owns a split tree of panes. */
export class Tab {
  readonly id = nextId("tab");
  readonly element = document.createElement("div");
  readonly panes = new Map<string, Pane>();
  tree: LayoutNode | null = null;
  focusedId: string | null = null;
  private view: LayoutView;
  onChange?: () => void;

  constructor() {
    this.element.className = "tab-content";
    this.view = new LayoutView(this.element, {
      elementFor: (id) => this.panes.get(id)?.element,
      onRatio: (splitId, ratio) => {
        if (!this.tree) return;
        this.tree = setRatio(this.tree, splitId, ratio);
        this.render();
      },
    });
  }

  get title(): string {
    const p = this.focusedId ? this.panes.get(this.focusedId) : undefined;
    return p?.title || "yate";
  }

  get focused(): Pane | undefined {
    return this.focusedId ? this.panes.get(this.focusedId) : undefined;
  }

  /** Add a pane; splits the focused leaf or becomes the root. */
  add(pane: Pane, dir: Dir = "row", target = this.focusedId): void {
    this.panes.set(pane.id, pane);
    if (!this.tree || !target) {
      this.tree = { kind: "leaf", id: pane.id };
    } else {
      this.tree = splitLeaf(this.tree, target, dir, pane.id);
    }
    pane.element.addEventListener("focusin", () => this.setFocus(pane.id));
    pane.element.addEventListener("pointerdown", () => this.setFocus(pane.id), { capture: true });
    this.render();
    this.setFocus(pane.id);
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
    for (const [pid, p] of this.panes) p.element.classList.toggle("focused", pid === id);
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
    requestAnimationFrame(() => {
      for (const p of this.panes.values()) p.relayout();
    });
  }

  dispose(): void {
    for (const p of this.panes.values()) p.dispose();
    this.panes.clear();
    this.element.remove();
  }
}
