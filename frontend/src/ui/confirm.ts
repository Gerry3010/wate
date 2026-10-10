/**
 * A small modal for the one question that must not be answered by accident.
 *
 * Everything else in wate asks through a popup menu, which closes the moment you look away —
 * right for choosing, wrong for "this will end work that is still running". So: a centred
 * card, a backdrop that swallows clicks, Escape for the safe answer, and no default button
 * that a stray Enter could press.
 */

export interface ConfirmChoice {
  label: string;
  /** Marks the answer that destroys something. */
  danger?: boolean;
  onSelect: () => void;
}

let open: HTMLElement | null = null;

export function closeConfirm(): void {
  open?.remove();
  open = null;
  window.removeEventListener("keydown", onKey, true);
}

function onKey(e: KeyboardEvent) {
  if (e.key !== "Escape") return;
  e.stopPropagation();
  e.preventDefault();
  closeConfirm();
}

/** Show the card. Escape and the backdrop both mean "do nothing". */
export function showConfirm(title: string, detail: string, choices: ConfirmChoice[]): void {
  closeConfirm();
  const back = document.createElement("div");
  back.className = "confirm-back";
  const card = document.createElement("div");
  card.className = "confirm-card";

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
    b.className = "confirm-button" + (c.danger ? " danger" : "");
    b.textContent = c.label;
    b.addEventListener("click", () => {
      closeConfirm();
      c.onSelect();
    });
    row.appendChild(b);
  }
  card.append(h, d, row);
  back.appendChild(card);
  // A click that misses the card is not an answer; it must not reach what is behind it either.
  back.addEventListener("pointerdown", (e) => {
    e.stopPropagation();
    if (e.target === back) closeConfirm();
  });
  document.body.appendChild(back);
  open = back;
  window.addEventListener("keydown", onKey, true);
}
