// @vitest-environment jsdom
import { beforeAll, describe, expect, it } from "vitest";
import type { Pane } from "../pane";
import { Tab, nextId, noteId } from "./tab";

beforeAll(() => {
  // jsdom has no layout. The view only uses this to redraw its dividers, Alt+drag only to find
  // the pane under the pointer.
  document.elementFromPoint ??= () => null;
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
});

/** A pane with a header holding a button, like an editor's toolbar with its Save. */
function pane(id: string, kind: Pane["kind"]): Pane & { handle: HTMLElement; button: HTMLButtonElement } {
  const element = document.createElement("div");
  const handle = document.createElement("div");
  const button = document.createElement("button");
  handle.appendChild(button);
  element.appendChild(handle);
  return { id, kind, element, handle, button, title: id, focus() {}, cwd: async () => "", relayout() {}, dispose() {} };
}

const press = (on: HTMLElement, init: MouseEventInit = {}) =>
  on.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true, button: 0, clientX: 10, clientY: 10, ...init }));
const moveTo = (x: number, y: number) => window.dispatchEvent(new MouseEvent("pointermove", { clientX: x, clientY: y }));
const release = () => window.dispatchEvent(new MouseEvent("pointerup", { clientX: 200, clientY: 10 }));

describe("a pane's handle", () => {
  // Editors and the settings pane used to have no handle at all: only a terminal's bar
  // could be dragged, so a file opened with `wate open` stayed wherever it had landed.
  for (const kind of ["editor", "settings", "terminal"] as const) {
    it(`moves ${kind === "editor" ? "an" : "a"} ${kind} pane when dragged`, () => {
      const tab = new Tab();
      const p = pane("p1", kind);
      tab.add(p);
      tab.add(pane("p2", "terminal"), "row", "p1");
      press(p.handle);
      moveTo(200, 10);
      expect(p.element.classList.contains("swap-source")).toBe(true);
      release();
      expect(p.element.classList.contains("swap-source")).toBe(false);
    });
  }

  it("leaves its buttons to be pressed", () => {
    const tab = new Tab();
    const p = pane("p1", "editor");
    tab.add(p);
    press(p.button);
    moveTo(200, 10);
    expect(p.element.classList.contains("swap-source")).toBe(false);
    release();
  });

  it("does not start a second drag on top of Alt+drag", () => {
    const tab = new Tab();
    const p = pane("p1", "editor");
    tab.add(p);
    tab.add(pane("p2", "terminal"), "row", "p1");
    let handleDrag = false;
    tab.onPaneDragMove = () => ((handleDrag = true), false);
    press(p.handle, { altKey: true });
    moveTo(200, 10);
    release();
    // Alt+drag is the swap gesture; the handle's move must stay out of it, or one release
    // would run two drops.
    expect(handleDrag).toBe(false);
  });

  it("answers to the tab it is in now, not the one it came from", () => {
    const from = new Tab();
    const to = new Tab();
    const p = pane("p1", "editor");
    from.add(p);
    from.detachPane("p1");
    to.add(p);
    to.add(pane("p2", "terminal"), "row", "p1");
    let movedBy = "";
    from.onPaneDragMove = () => ((movedBy = "from"), false);
    to.onPaneDragMove = () => ((movedBy = "to"), false);
    press(p.handle);
    moveTo(200, 10);
    release();
    expect(movedBy).toBe("to");
  });
});

describe("the pane bar", () => {
  it("shows on a pane alone in its tab as well", () => {
    // It carries the number agents name the pane by.
    const tab = new Tab();
    const shown: boolean[] = [];
    tab.add({ ...pane("p1", "terminal"), setStatusVisible: (on: boolean) => void shown.push(on) });
    tab.render();
    expect(shown.length).toBeGreaterThan(0);
    expect(shown.every(Boolean)).toBe(true);
  });
});

describe("pane ids", () => {
  it("count on past an id the window took over", () => {
    noteId("pane-500-abcde");
    expect(nextId("pane")).toMatch(/^pane-501-/);
    // A lower one, or one without a number, changes nothing.
    noteId("pane-3-zzzzz");
    noteId("imp-pane-x");
    expect(nextId("tab")).toMatch(/^tab-502-/);
  });
});
