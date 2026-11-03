import { describe, expect, it } from "vitest";
import { claudeInput, type InputCell } from "./prompt";

/** A screen row: plain text, then `dim` text, with the cell at `inverse` (an index) inverted. */
function row(plain: string, dim = "", inverse = -1): InputCell[] {
  const cells: InputCell[] = [];
  for (const ch of plain) cells.push({ ch, dim: false, inverse: false });
  for (const ch of dim) cells.push({ ch, dim: true, inverse: false });
  if (inverse >= 0) {
    while (cells.length <= inverse) cells.push({ ch: " ", dim: false, inverse: false });
    cells[inverse] = { ...cells[inverse], inverse: true };
  }
  return cells;
}

const rule = row("─".repeat(40));
const pending = row("❯ Reply with only the word ok."); // an earlier prompt in the scrollback

/** Claude Code's screen with `input` as the box's rows; the cursor on the box's first row. */
function screen(...input: InputCell[][]) {
  const rows = [pending, row("● ok"), row(""), rule, ...input, rule, row("  ctx 8% | yate")];
  return { rows, cursor: 4 };
}

describe("claudeInput", () => {
  it("takes an empty box for empty", () => {
    const { rows, cursor } = screen(row("❯ "));
    expect(claudeInput(rows, cursor)).toBe("empty");
  });

  it("takes a placeholder or a suggestion for empty", () => {
    const { rows, cursor } = screen(row("❯ ", 'Try "how does base.css work?"'));
    expect(claudeInput(rows, cursor)).toBe("empty");
  });

  it("takes the inverted first letter of a placeholder for the cursor, not for typing", () => {
    // What a focused terminal shows: the first letter inverted and not dim.
    const cells = row("❯ ", 'Try "how does base.css work?"');
    cells[2] = { ch: "T", dim: false, inverse: true };
    const { rows, cursor } = screen(cells);
    expect(claudeInput(rows, cursor)).toBe("empty");
    // And an empty box with the cursor drawn as an inverted blank.
    const bare = screen(row("❯ ", "", 2));
    expect(claudeInput(bare.rows, bare.cursor)).toBe("empty");
  });

  it("sees typed text, wherever the cursor is in it", () => {
    for (const cells of [row("❯ hello"), row("❯ hello", "", 7), row("❯ hello", "", 2)]) {
      const { rows, cursor } = screen(cells);
      expect(claudeInput(rows, cursor)).toBe("typed");
    }
  });

  it("sees a single letter with the cursor in front of it", () => {
    const { rows, cursor } = screen(row("❯ h", "", 2));
    expect(claudeInput(rows, cursor)).toBe("typed");
    const unfocused = screen(row("❯ h"));
    expect(claudeInput(unfocused.rows, unfocused.cursor)).toBe("typed");
  });

  it("sees text on a further line of the box", () => {
    const { rows } = screen(row("❯ "), row("  second line"));
    expect(claudeInput(rows, 5)).toBe("typed");
  });

  it("finds nothing when the cursor is not in the box", () => {
    const { rows } = screen(row("❯ "));
    // On the status line below, or on an earlier prompt that has no rules around it.
    expect(claudeInput(rows, 6)).toBe("none");
    expect(claudeInput(rows, 0)).toBe("none");
  });

  it("finds nothing in a dialog or in bash mode", () => {
    const dialog = [row("   Remote Control"), row("   ❯ Continue"), row("   Enter to select")];
    expect(claudeInput(dialog, 1)).toBe("none");
    const { rows, cursor } = screen(row("! ls"));
    expect(claudeInput(rows, cursor)).toBe("none");
  });

  it("wants the rule under the box", () => {
    const rows = [rule, row("❯ ")];
    expect(claudeInput(rows, 1)).toBe("none");
  });
});
