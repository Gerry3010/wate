/**
 * A small modal for the one question that must not be answered by accident.
 *
 * Everything else in wate asks through a popup menu, which closes the moment you look away —
 * right for choosing, wrong for "this will end work that is still running". So: a centred
 * card, a backdrop that swallows clicks, Escape for the safe answer, and no default button
 * that a stray Enter could press — unless the question is one where pressing it costs nothing,
 * and then a choice can say so with `primary`.
 */

export interface ConfirmChoice {
  label: string;
  /** Marks the answer that destroys something. */
  danger?: boolean;
  /** The answer Enter gives. Never on a question where a stray Enter would cost anything. */
  primary?: boolean;
  onSelect: () => void;
}

let open: HTMLElement | null = null;
let onEnter: (() => void) | null = null;

export function closeConfirm(): void {
  open?.remove();
  open = null;
  onEnter = null;
  window.removeEventListener("keydown", onKey, true);
}

function onKey(e: KeyboardEvent) {
  if (e.key === "Enter" && onEnter) {
    const answer = onEnter;
    e.stopPropagation();
    e.preventDefault();
    closeConfirm();
    answer();
    return;
  }
  if (e.key !== "Escape") return;
  e.stopPropagation();
  e.preventDefault();
  closeConfirm();
}

export interface ConfirmOptions {
  /** Items the question is about, one line each (cut off with an ellipsis, whole in the
   *  tooltip). The card grows wider for them and the list scrolls once it runs long. */
  list?: string[];
}

/** Show the card. Escape and the backdrop both mean "do nothing". Returns the card's backdrop,
 *  so a caller can tell later whether its question is still the one on screen. */
export function showConfirm(title: string, detail: string, choices: ConfirmChoice[], opts: ConfirmOptions = {}): HTMLElement {
  closeConfirm();
  const back = document.createElement("div");
  back.className = "confirm-back";
  const card = document.createElement("div");
  card.className = "confirm-card" + (opts.list?.length ? " wide" : "");

  const h = document.createElement("div");
  h.className = "confirm-title";
  h.textContent = title;
  const d = document.createElement("div");
  d.className = "confirm-detail";
  d.textContent = detail;
  const row = document.createElement("div");
  row.className = "confirm-row";
  for (const c of choices) {
    const b = document.createElement("button");
    b.className = "confirm-button" + (c.danger ? " danger" : "") + (c.primary ? " primary" : "");
    b.textContent = c.label;
    b.addEventListener("click", () => {
      closeConfirm();
      c.onSelect();
    });
    row.appendChild(b);
  }
  card.append(h, d);
  if (opts.list?.length) {
    const ul = document.createElement("ul");
    ul.className = "confirm-list";
    for (const item of opts.list) {
      const li = document.createElement("li");
      li.textContent = item;
      li.title = item;
      ul.appendChild(li);
    }
    card.appendChild(ul);
  }
  card.appendChild(row);
  back.appendChild(card);
  // A click that misses the card is not an answer; it must not reach what is behind it either.
  back.addEventListener("pointerdown", (e) => {
    e.stopPropagation();
    if (e.target === back) closeConfirm();
  });
  document.body.appendChild(back);
  open = back;
  onEnter = choices.find((c) => c.primary)?.onSelect ?? null;
  window.addEventListener("keydown", onKey, true);
  return back;
}
