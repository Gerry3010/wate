import type { Dir, LayoutNode } from "./tree";

export interface LayoutViewHost {
  /** Element for a leaf id (the pane's root element). */
  elementFor(id: string): HTMLElement | undefined;
  onRatio(splitId: string, ratio: number): void;
}

/** Renders a split tree as nested flex containers with draggable dividers. */
export class LayoutView {
  constructor(readonly container: HTMLElement, private host: LayoutViewHost) {
    container.classList.add("layout");
  }

  render(tree: LayoutNode | null) {
    // Detach pane elements first so rebuilding the structure never destroys them.
    for (const el of Array.from(this.container.querySelectorAll<HTMLElement>(".pane"))) el.remove();
    this.container.replaceChildren();
    if (tree) this.container.appendChild(this.build(tree));
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
    a.style.flex = `${n.ratio} 1 0%`;
    b.style.flex = `${1 - n.ratio} 1 0%`;
    box.append(a, this.divider(n.id, n.dir, box), b);
    return box;
  }

  private divider(splitId: string, dir: Dir, box: HTMLElement): HTMLElement {
    const d = document.createElement("div");
    d.className = `divider divider-${dir}`;
    d.addEventListener("pointerdown", (e) => {
      e.preventDefault();
      d.setPointerCapture(e.pointerId);
      d.classList.add("dragging");
      const move = (ev: PointerEvent) => {
        const r = box.getBoundingClientRect();
        const ratio = dir === "row" ? (ev.clientX - r.left) / r.width : (ev.clientY - r.top) / r.height;
        this.host.onRatio(splitId, ratio);
      };
      const up = () => {
        d.classList.remove("dragging");
        d.removeEventListener("pointermove", move);
        d.removeEventListener("pointerup", up);
      };
      d.addEventListener("pointermove", move);
      d.addEventListener("pointerup", up);
    });
    return d;
  }
}
