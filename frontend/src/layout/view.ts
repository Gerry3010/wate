import { SNAP_POINTS, snapRatio, type Dir, type LayoutNode } from "./tree";

export interface LayoutViewHost {
  /** Element for a leaf id (the pane's root element). */
  elementFor(id: string): HTMLElement | undefined;
  /** Model update after a drag finished. */
  onRatio(splitId: string, ratio: number): void;
  /** A divider drag started: panes may suspend expensive relayout until onResized. */
  onDragStart(): void;
  /** Panes need to re-fit after their box changed. */
  onResized(): void;
  /** The divider lines were redrawn; here is the focused pane's rectangle (null when none). */
  onLines?(focused: DOMRect | null): void;
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
  private dividers: { el: HTMLElement; line: HTMLElement; dir: Dir }[] = [];
  private lineTimer?: ReturnType<typeof setTimeout>;

  constructor(readonly container: HTMLElement, private host: LayoutViewHost) {
    container.classList.add("layout");
    // Window resizes move the panes under the dividers.
    new ResizeObserver(() => this.scheduleLines()).observe(container);
  }

  render(tree: LayoutNode | null) {
    // Detach pane elements first so rebuilding the structure never destroys them.
    for (const el of Array.from(this.container.querySelectorAll<HTMLElement>(".pane"))) el.remove();
    this.container.replaceChildren();
    this.splits.clear();
    this.dividers = [];
    if (tree) this.container.appendChild(this.build(tree));
    this.scheduleLines();
  }

  /** Update one divider without rebuilding (drag, keyboard resize). */
  setRatio(splitId: string, ratio: number) {
    const s = this.splits.get(splitId);
    if (!s) return;
    s.a.style.flex = `${ratio} 1 0%`;
    s.b.style.flex = `${1 - ratio} 1 0%`;
    this.scheduleLines();
  }

  /** Redraw the divider lines on the next frame (focus moved, sizes changed). */
  scheduleLines() {
    clearTimeout(this.lineTimer);
    this.lineTimer = setTimeout(() => this.updateLines(), 0);
  }

  /**
   * Divider lines are drawn only between two inactive panes: wherever the focused pane borders
   * a divider, that stretch of the line is cut out (the divider itself stays there, and lights
   * up on hover for resizing). A divider along a sub-split keeps its line beside the other panes.
   */
  private updateLines() {
    const focused = this.container.querySelector<HTMLElement>(".pane.focused");
    const f = focused?.getBoundingClientRect();
    for (const { el, line, dir } of this.dividers) {
      let mask = "";
      if (f) {
        const d = el.getBoundingClientRect();
        const touch = 6;
        if (dir === "row") {
          const adjacent = Math.abs(f.right - d.left) <= touch || Math.abs(f.left - d.right) <= touch;
          const lo = Math.max(d.top, f.top) - d.top;
          const hi = Math.min(d.bottom, f.bottom) - d.top;
          if (adjacent && hi - lo > 1) mask = `linear-gradient(to bottom, #000 ${lo}px, transparent ${lo}px, transparent ${hi}px, #000 ${hi}px)`;
        } else {
          const adjacent = Math.abs(f.bottom - d.top) <= touch || Math.abs(f.top - d.bottom) <= touch;
          const lo = Math.max(d.left, f.left) - d.left;
          const hi = Math.min(d.right, f.right) - d.left;
          if (adjacent && hi - lo > 1) mask = `linear-gradient(to right, #000 ${lo}px, transparent ${lo}px, transparent ${hi}px, #000 ${hi}px)`;
        }
      }
      line.style.maskImage = mask;
      line.style.webkitMaskImage = mask;
    }
    this.host.onLines?.(f ?? null);
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
    const line = document.createElement("div");
    line.className = "divider-line";
    d.appendChild(line);
    this.dividers.push({ el: d, line, dir });
    d.addEventListener("pointerdown", (e) => {
      if (e.button !== 0) return;
      e.preventDefault();
      d.setPointerCapture(e.pointerId);
      d.classList.add("dragging");
      this.host.onDragStart();
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
