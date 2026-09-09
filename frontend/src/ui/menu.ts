/** Lightweight popup menu (context menus, the tab bar dropdown). One open at a time. */

export interface MenuItem {
  label: string;
  /** Secondary text on the right (e.g. "3 tabs"). */
  hint?: string;
  danger?: boolean;
  disabled?: boolean;
  /** Colour swatch shown before the label. */
  swatch?: string;
  checked?: boolean;
  onSelect?: () => void;
  /** Small inline action button at the right edge (e.g. delete a saved session). */
  action?: { label: string; title?: string; onSelect: () => void };
}

export type MenuEntry = MenuItem | "separator" | { header: string } | { input: InputEntry };

export interface InputEntry {
  placeholder: string;
  button: string;
  value?: string;
  onSubmit(value: string): void;
}

let open: HTMLElement | null = null;

export function closeMenu() {
  open?.remove();
  open = null;
  window.removeEventListener("pointerdown", onOutside, true);
  window.removeEventListener("keydown", onKey, true);
  window.removeEventListener("blur", closeMenu);
}

function onOutside(e: PointerEvent) {
  if (open && !open.contains(e.target as Node)) closeMenu();
}

function onKey(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.stopPropagation();
    closeMenu();
  }
}

/** Show a menu at viewport coordinates; it flips to stay on screen. */
export function showMenu(x: number, y: number, entries: MenuEntry[]): HTMLElement {
  closeMenu();
  const el = document.createElement("div");
  el.className = "popup-menu";
  el.setAttribute("role", "menu");
  for (const entry of entries) el.appendChild(renderEntry(entry));
  document.body.appendChild(el);
  open = el;
  // Position after layout so we know the size.
  const r = el.getBoundingClientRect();
  const left = Math.max(4, Math.min(x, window.innerWidth - r.width - 4));
  const top = Math.max(4, Math.min(y, window.innerHeight - r.height - 4));
  el.style.left = `${left}px`;
  el.style.top = `${top}px`;
  window.addEventListener("pointerdown", onOutside, true);
  window.addEventListener("keydown", onKey, true);
  window.addEventListener("blur", closeMenu);
  const firstInput = el.querySelector<HTMLInputElement>("input");
  if (firstInput) firstInput.focus();
  return el;
}

function renderEntry(entry: MenuEntry): HTMLElement {
  if (entry === "separator") {
    const s = document.createElement("div");
    s.className = "popup-sep";
    return s;
  }
  if ("header" in entry) {
    const h = document.createElement("div");
    h.className = "popup-header";
    h.textContent = entry.header;
    return h;
  }
  if ("input" in entry) {
    const row = document.createElement("form");
    row.className = "popup-input";
    const i = document.createElement("input");
    i.type = "text";
    i.placeholder = entry.input.placeholder;
    i.value = entry.input.value ?? "";
    i.addEventListener("keydown", (e) => e.stopPropagation());
    const b = document.createElement("button");
    b.type = "submit";
    b.textContent = entry.input.button;
    row.append(i, b);
    row.addEventListener("submit", (e) => {
      e.preventDefault();
      const v = i.value.trim();
      if (!v) return;
      closeMenu();
      entry.input.onSubmit(v);
    });
    return row;
  }
  const item = document.createElement("div");
  item.className = "popup-item" + (entry.danger ? " danger" : "") + (entry.disabled ? " disabled" : "");
  item.setAttribute("role", "menuitem");
  if (entry.swatch !== undefined) {
    const sw = document.createElement("span");
    sw.className = "popup-swatch";
    if (entry.swatch) sw.style.background = entry.swatch;
    else sw.classList.add("none");
    item.appendChild(sw);
  }
  const label = document.createElement("span");
  label.className = "popup-label";
  label.textContent = entry.label;
  item.appendChild(label);
  if (entry.checked) item.classList.add("checked");
  if (entry.hint) {
    const hint = document.createElement("span");
    hint.className = "popup-hint";
    hint.textContent = entry.hint;
    item.appendChild(hint);
  }
  if (entry.action) {
    const a = document.createElement("button");
    a.className = "popup-action";
    a.textContent = entry.action.label;
    a.title = entry.action.title ?? "";
    a.addEventListener("click", (e) => {
      e.stopPropagation();
      closeMenu();
      entry.action!.onSelect();
    });
    item.appendChild(a);
  }
  if (!entry.disabled && entry.onSelect) {
    item.addEventListener("click", () => {
      closeMenu();
      entry.onSelect!();
    });
  }
  return item;
}
