/**
 * What a dropped file would do, shown while it hovers: the pane it would land in, or the half it
 * would split off. The DOM half of the drop feature — the geometry lives in drop.ts.
 */
import type { Box, DropZone } from "./drop";

/** Callbacks a drag feeds while it moves over the window. */
export interface DragMotion {
  move(x: number, y: number): void;
  leave(): void;
}

/**
 * The highlight itself: one box that is moved, never recreated, and never touched unless something
 * actually changed — a drag reports motion far more often than it changes zones, and every style
 * write costs a layout pass the terminal underneath has to share.
 */
export class DropHighlight {
  private el = document.createElement("div");
  private at = "";

  constructor(parent: HTMLElement) {
    this.el.className = "drop-zone";
    parent.appendChild(this.el);
  }

  show(rect: Box, zone: DropZone): void {
    const key = `${zone} ${rect.x} ${rect.y} ${rect.w} ${rect.h}`;
    if (key === this.at) return;
    const first = this.at === "";
    this.at = key;
    const s = this.el.style;
    // The first frame has nowhere to travel from, so it is placed with the transition off —
    // otherwise the box would fly in from the corner of the screen before fading up.
    if (first) s.transition = "none";
    // Position through a transform (composited), size through layout (rare, and instant).
    s.transform = `translate(${rect.x}px, ${rect.y}px)`;
    s.width = `${rect.w}px`;
    s.height = `${rect.h}px`;
    this.el.dataset.zone = zone;
    if (first) {
      void this.el.offsetWidth; // flush the placement, so what follows animates from here
      s.transition = "";
    }
    this.el.classList.add("visible");
  }

  hide(): void {
    if (this.at === "") return;
    this.at = "";
    this.el.classList.remove("visible");
  }

  dispose(): void {
    this.el.remove();
  }
}

interface WailsDragHooks {
  handleDragOver?: (x: number, y: number) => void;
  handleDragLeave?: () => void;
}

/**
 * Follow a native file drag across the window.
 *
 * The OS drag never reaches the DOM here (the native drop target claims it), and the backend gets
 * no motion event either — Wails drives the hover state by calling `handleDragOver` on its own
 * runtime object (@wailsio/runtime/dist/window.js). So that call is wrapped rather than replaced:
 * if a future version drops it, the preview quietly stops and the drop itself still works.
 *
 * A drag that starts *inside* the app (text out of the editor) is ignored: the GTK motion
 * controller reports every drag, not just files, and a highlight over one's own text drag is noise.
 */
export function installDragMotion(motion: DragMotion): () => void {
  const hooks = ((window as unknown as { _wails?: WailsDragHooks })._wails ??= {});
  const originalOver = hooks.handleDragOver;
  const originalLeave = hooks.handleDragLeave;
  let internal = false;
  let idle: ReturnType<typeof setTimeout> | undefined;
  // A drag reports motion faster than the screen updates; one frame, one preview.
  let frame = 0;
  let pending: [number, number] | null = null;

  const done = () => {
    clearTimeout(idle);
    idle = undefined;
    if (frame) cancelAnimationFrame(frame);
    frame = 0;
    pending = null;
    try {
      motion.leave();
    } catch (err) {
      console.warn("drop preview:", err);
    }
  };
  const paint = () => {
    frame = 0;
    const point = pending;
    pending = null;
    if (!point) return;
    try {
      motion.move(point[0], point[1]);
    } catch (err) {
      console.warn("drop preview:", err);
    }
  };
  const moved = (x: number, y: number) => {
    if (internal) return;
    clearTimeout(idle);
    // A drag that ends outside the window may never report a leave, so the preview times itself out.
    idle = setTimeout(done, 4000);
    pending = [x, y];
    if (!frame) frame = requestAnimationFrame(paint);
  };

  hooks.handleDragOver = (x: number, y: number) => {
    originalOver?.(x, y);
    moved(x, y);
  };
  hooks.handleDragLeave = () => {
    originalLeave?.();
    done();
  };

  const onStart = () => {
    internal = true;
  };
  const onEnd = () => {
    internal = false;
    done();
  };
  // Fallback for engines that do deliver the drag to the DOM (and the guard for our own drags).
  const onOver = (e: DragEvent) => moved(e.clientX, e.clientY);
  window.addEventListener("dragstart", onStart, true);
  window.addEventListener("dragend", onEnd, true);
  window.addEventListener("drop", onEnd, true);
  window.addEventListener("dragover", onOver, true);
  window.addEventListener("blur", done);

  return () => {
    hooks.handleDragOver = originalOver;
    hooks.handleDragLeave = originalLeave;
    window.removeEventListener("dragstart", onStart, true);
    window.removeEventListener("dragend", onEnd, true);
    window.removeEventListener("drop", onEnd, true);
    window.removeEventListener("dragover", onOver, true);
    window.removeEventListener("blur", done);
    clearTimeout(idle);
    if (frame) cancelAnimationFrame(frame);
  };
}
