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

/**
 * The root agent's panel. It lies over the tabs rather than in one: the root agent belongs to no
 * project, and a tab of its own was one more thing to scroll past. A small button in the corner
 * (and the Claude sidebar, and a key) brings it up; it holds the one pane the agent runs in.
 */
export class RootPanel {
  readonly element = document.createElement("div");
  readonly fab = document.createElement("button");
  private body = document.createElement("div");
  private dot = document.createElement("span");
  pane?: TerminalPane;
  /** Called after it was shown or hidden, so the focus can go where it belongs. */
  onVisibility?: (open: boolean) => void;

  constructor(label: string) {
    this.element.className = "root-panel root-drawer";
    this.element.hidden = true;
    this.body.className = "root-panel-body";
    this.element.append(this.body);

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

  setLayout(layout: RootLayout) {
    for (const [l] of ROOT_LAYOUTS) this.element.classList.toggle(`root-${l}`, l === layout);
    if (this.open) this.pane?.relayout();
  }

  /** The agent's state for the button's dot: running / waiting / done / "". */
  setStatus(status: string) {
    this.fab.className = "root-fab" + (status ? ` status-${status}` : "") + (this.open ? " open" : "");
    this.dot.hidden = !status;
  }
}
