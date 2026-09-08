import { SNAP_POINTS, snapRatio, type Dir, type LayoutNode } from "./tree";

export interface LayoutViewHost {
  /** Element for a leaf id (the pane's root element). */
  elementFor(id: string): HTMLElement | undefined;
  /** Model update after a drag finished. */
  onRatio(splitId: string, ratio: number): void;
  /** Panes need to re-fit after their box changed. */
  onResized(): void;
}

interface SplitEls {
  box: HTMLElement;
  a: HTMLElement;
  b: HTMLElement;
  dir: Dir;
}

/** Renders a split tree as nested flex containers with draggable, snapping dividers. */
export class LayoutView {
  private splits = new Map<string, SplitEls>();

  constructor(readonly container: HTMLElement, private host: LayoutViewHost) {
    container.classList.add("layout");
  }

  render(tree: LayoutNode | null) {
    // Detach pane elements first so rebuilding the structure never destroys them.
    for (const el of Array.from(this.container.querySelectorAll<HTMLElement>(".pane"))) el.remove();
    this.container.replaceChildren();
    this.splits.clear();
    if (tree) this.container.appendChild(this.build(tree));
  }

  /** Update one divider without rebuilding (drag, keyboard resize). */
  setRatio(splitId: string, ratio: number) {
    const s = this.splits.get(splitId);
    if (!s) return;
    s.a.style.flex = `${ratio} 1 0%`;
    s.b.style.flex = `${1 - ratio} 1 0%`;
  }

  private build(n: LayoutNode): HTMLElement {
    if (n.kind === "leaf") {
      const el = this.host.elementFor(n.id);
      if (!el) {
        const ph = document.createElement("div");
        ph.className = "pane pane-missing";
        return ph;
      }
      el.style.flex = "";
      return el;
    }
    const box = document.createElement("div");
    box.className = `split split-${n.dir}`;
    const a = this.build(n.a);
    const b = this.build(n.b);
    box.append(a, this.divider(n.id, n.dir, box), b);
    this.splits.set(n.id, { box, a, b, dir: n.dir });
    this.setRatio(n.id, n.ratio);
    return box;
  }

  private divider(splitId: string, dir: Dir, box: HTMLElement): HTMLElement {
    const d = document.createElement("div");
    d.className = `divider divider-${dir}`;
    d.addEventListener("pointerdown", (e) => {
      if (e.button !== 0) return;
      e.preventDefault();
      d.setPointerCapture(e.pointerId);
      d.classList.add("dragging");
      const guides = this.showGuides(box, dir);
      let current: number | null = null;
      const move = (ev: PointerEvent) => {
        const r = box.getBoundingClientRect();
        const raw = dir === "row" ? (ev.clientX - r.left) / r.width : (ev.clientY - r.top) / r.height;
        const { ratio, snapped } = snapRatio(Math.min(0.9, Math.max(0.1, raw)));
        current = ratio;
        this.setRatio(splitId, ratio);
        d.classList.toggle("snapped", snapped !== null);
        for (const g of guides.children) (g as HTMLElement).classList.toggle("active", Number((g as HTMLElement).dataset.p) === snapped);
      };
      const up = () => {
        d.classList.remove("dragging", "snapped");
        d.removeEventListener("pointermove", move);
        d.removeEventListener("pointerup", up);
        d.removeEventListener("pointercancel", up);
        guides.remove();
        if (current !== null) this.host.onRatio(splitId, current);
        this.host.onResized();
      };
      d.addEventListener("pointermove", move);
      d.addEventListener("pointerup", up);
      d.addEventListener("pointercancel", up);
    });
    return d;
  }

  /** Snap markers along the split axis, shown while dragging. */
  private showGuides(box: HTMLElement, dir: Dir): HTMLElement {
    const g = document.createElement("div");
    g.className = `snap-guides snap-guides-${dir}`;
    for (const p of SNAP_POINTS) {
      const m = document.createElement("div");
      m.className = "snap-guide";
      m.dataset.p = String(p);
      m.style[dir === "row" ? "left" : "top"] = `${p * 100}%`;
      g.appendChild(m);
    }
    box.appendChild(g);
    return g;
  }
}
