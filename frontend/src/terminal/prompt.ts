/**
 * Reading Claude Code's input box off a pane's screen, to tell whether anything has been typed
 * into it. Kept away from xterm.js so it can be tested, like snapshot.ts.
 */

/** One screen cell, as much of it as the question needs. */
export interface InputCell {
  ch: string;
  dim: boolean;
  inverse: boolean;
}

/** "empty", "typed" when there is text in the box, "none" when no input box has the cursor. */
export type ClaudeInput = "empty" | "typed" | "none";

const PROMPT = "❯";
const RULE = "─";

const first = (row: InputCell[] | undefined) => row?.[0]?.ch;
const blank = (c: InputCell) => c.ch.trim() === "";

/**
 * What Claude Code's input box holds. `rows` is the screen, `cursorRow` the cursor's row on it.
 *
 * The box is a "❯ " line between two rules, plus a line for each further line of input, and
 * the cursor sits in it. Earlier prompts further up start with "❯" too, which is why the
 * search goes out from the cursor and wants the rules around it: no box under the cursor (a
 * dialog has it, or bash mode, or this is not Claude Code) is "none".
 *
 * An empty box shows a placeholder or a suggested prompt. Both are dim, except that while the
 * terminal has the focus the first letter is inverted: that is the cursor. Anything else
 * written in the box was typed, and so is a single inverted letter with nothing dim after
 * it, which is a one-letter input with the cursor in front of it.
 */
export function claudeInput(rows: InputCell[][], cursorRow: number): ClaudeInput {
  let start = -1;
  for (let y = Math.min(cursorRow, rows.length - 1); y >= 0; y--) {
    const c = first(rows[y]);
    if (c === PROMPT) {
      start = y;
      break;
    }
    if (c === RULE) break;
  }
  if (start < 1 || first(rows[start - 1]) !== RULE) return "none";
  let end = start + 1;
  while (end < rows.length && first(rows[end]) !== RULE) end++;
  if (end >= rows.length || cursorRow >= end) return "none";

  const cells = [...rows[start].slice(2), ...rows.slice(start + 1, end).flat()];
  // The cursor at the start of the box, when it is drawn there.
  const at = cells.findIndex((c) => !blank(c) || c.inverse);
  const head = at >= 0 && cells[at].inverse ? at : -1;
  const rest = cells.filter((c, i) => i !== head && !blank(c));
  if (rest.some((c) => !c.dim)) return "typed";
  if (head >= 0 && !blank(cells[head]) && rest.length === 0) return "typed";
  return "empty";
}
