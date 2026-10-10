/**
 * The strip along the top of a pane, shown only while the pane shares its tab with another.
 *
 * A single pane needs no label: it *is* the tab. Two or more, and it stops being obvious which
 * one is running what, which one an agent opened, and which one has been opened up to it. That
 * last part is the reason the bar exists at all — a pane the user has allowed an agent to read
 * or type into has to say so without anyone opening a menu.
 */

import type { Access } from "../api";
import { DOTS } from "../ui/icons";
import { showMenu, type MenuEntry } from "../ui/menu";

/** Everything the bar draws, in the form it draws it. Pure, so it can be tested. */
export interface StatusView {
  /** Short word for where the pane came from, or "" for an ordinary one. */
  origin: string;
  /** Status word for the dot's colour class: running / waiting / done / "". */
  status: string;
  /** What the pane is, in a few characters: the command, or the shell. */
  label: string;
  /** Letters for the rights the user has handed out, in a fixed order. */
  chips: string[];
  /** Hover text spelling the chips out. */
  chipTitle: string;
}

export interface StatusInput {
  openedByAgent: boolean;
  agentStatus?: string;
  command: string;
  access: Access;
}

const RIGHTS: [keyof Access, string, string][] = [
  ["read", "R", "read this pane"],
  ["write", "W", "type into this pane"],
  ["manage", "M", "resize and close this pane"],
];

export function statusView(in_: StatusInput): StatusView {
  const chips: string[] = [];
  const spelled: string[] = [];
  for (const [key, letter, what] of RIGHTS) {
    if (in_.access?.[key]) {
      chips.push(letter);
      spelled.push(what);
    }
  }
  return {
    origin: in_.openedByAgent ? "agent" : "",
    status: in_.agentStatus ?? "",
    label: in_.command || "shell",
    chips,
    chipTitle: spelled.length ? "Agents in this tab may " + spelled.join(", ") : "",
  };
}

/**
 * What a grant looks like after one tick is flipped.
 *
 * Turning Manage on fills in the other two, because managing a pane one may neither see nor
 * type into is not a halfway house anybody would pick. Turning it off leaves them: taking
 * away the right to close a pane is not a reason to stop reading it, and "Revoke all" is
 * right there for when it is. Nothing else implies anything — least of all Read, which is
 * the one with a privacy cost.
 */
export function afterToggle(access: Access, key: keyof Access): Access {
  const now = { ...access, [key]: !access[key] };
  if (key === "manage" && now.manage) return { read: true, write: true, manage: true };
  return now;
}

/** What the bar needs from the app to do its job. */
export interface StatusHost {
  access(paneId: string): Access;
  setAccess(paneId: string, access: Access): void;
  closePane(paneId: string): void;
  /** Extra entries above the agent section (moving the pane about). Empty is fine. */
  paneEntries(paneId: string): MenuEntry[];
}

export class PaneStatusBar {
  readonly element: HTMLElement;
  private host?: StatusHost;
  /** Set by the pane: a drag starting on the bar moves the pane. */
  onDragStart?: (e: PointerEvent) => void;
  private sig = "";
  private originEl: HTMLElement;
  private dot: HTMLElement;
  private labelEl: HTMLElement;
  private chipsEl: HTMLElement;

  constructor(private paneId: string) {
    this.element = document.createElement("div");
    this.element.className = "pane-bar";
    this.element.hidden = true;
    // Clicking the bar must not pull the focus out of the terminal, and a drag that starts on
    // it is the bar's own (see the Alt+drag handler on .pane, which captures pointerdown).
    this.element.addEventListener("mousedown", (e) => e.preventDefault());
    this.element.addEventListener("pointerdown", (e) => {
      e.stopPropagation();
      // The bar is the pane's handle: dragging it moves the pane, without having to know
      // that Alt+drag does the same thing from anywhere on the pane. Not from the buttons
      // on it, though — those are for pressing.
      if (e.button !== 0) return;
      if ((e.target as HTMLElement | null)?.closest(".pane-bar-menu")) return;
      this.onDragStart?.(e);
    });

    this.originEl = document.createElement("span");
    this.originEl.className = "pane-bar-origin";
    this.dot = document.createElement("span");
    this.dot.className = "status-dot";
    this.labelEl = document.createElement("span");
    this.labelEl.className = "pane-bar-label";
    this.chipsEl = document.createElement("span");
    this.chipsEl.className = "pane-bar-chips";

    const menu = document.createElement("button");
    menu.className = "pane-bar-menu";
    menu.innerHTML = DOTS;
    menu.title = "Pane options";
    menu.addEventListener("click", (e) => {
      e.stopPropagation();
      this.openMenu(menu.getBoundingClientRect());
    });

    this.element.append(this.originEl, this.dot, this.labelEl, this.chipsEl, menu);
  }

  setHost(host: StatusHost) {
    this.host = host;
  }

  /** Show or hide the bar. Returns true when it changed, because the grid then has to refit. */
  setVisible(on: boolean): boolean {
    if (this.element.hidden !== on) return false;
    this.element.hidden = !on;
    return true;
  }

  get visible(): boolean {
    return !this.element.hidden;
  }

  /** Repaint, but only when something actually changed: this runs on every agent tick. */
  update(view: StatusView) {
    const sig = [view.origin, view.status, view.label, view.chips.join("")].join("\u0000");
    if (sig === this.sig) return;
    this.sig = sig;
    this.originEl.textContent = view.origin;
    this.originEl.hidden = !view.origin;
    this.dot.className = "status-dot";
    this.element.className = "pane-bar" + (view.status ? ` status-${view.status}` : "");
    this.dot.hidden = !view.status;
    this.labelEl.textContent = view.label;
    this.chipsEl.textContent = view.chips.join(" ");
    this.chipsEl.title = view.chipTitle;
    this.chipsEl.hidden = view.chips.length === 0;
  }

  private openMenu(at: DOMRect) {
    const host = this.host;
    if (!host) return;
    const access = host.access(this.paneId);
    const toggle = (key: keyof Access) => () => host.setAccess(this.paneId, afterToggle(access, key));
    const entries: MenuEntry[] = [
      ...host.paneEntries(this.paneId),
      "separator",
      { header: "Agent access" },
      { label: "Read", hint: "see the screen", checked: access.read, onSelect: toggle("read") },
      { label: "Write", hint: "type, no Enter", checked: access.write, onSelect: toggle("write") },
      { label: "Manage", hint: "resize, close, move", checked: access.manage, onSelect: toggle("manage") },
    ];
    if (access.read || access.write || access.manage) {
      entries.push({
        label: "Revoke all",
        onSelect: () => host.setAccess(this.paneId, { read: false, write: false, manage: false }),
      });
    }
    entries.push("separator", { label: "Close pane", danger: true, onSelect: () => host.closePane(this.paneId) });
    const el = showMenu(at.right, at.bottom + 2, entries);
    // Keep it inside the window: the button sits at the pane's right edge.
    const r = el.getBoundingClientRect();
    el.style.left = `${Math.max(4, at.right - r.width)}px`;
  }
}
