import { describe, expect, it } from "vitest";
import { SNAPSHOT_LINES, SNAPSHOT_MAX_LINES, clampTail, snapshotRange, trimTrailingBlanks } from "./snapshot";

describe("snapshotRange", () => {
  it("takes the last lines, inclusive", () => {
    expect(snapshotRange(100, 10)).toEqual({ start: 90, end: 99 });
  });

  it("stops at the top of a short buffer", () => {
    expect(snapshotRange(5, 10)).toEqual({ start: 0, end: 4 });
  });

  it("reads nothing from an empty buffer", () => {
    const { start, end } = snapshotRange(0, 10);
    expect(end).toBeLessThan(start);
  });

  it("falls back to the default when asked for nothing sensible", () => {
    expect(snapshotRange(1000, 0).start).toBe(1000 - SNAPSHOT_LINES);
    expect(snapshotRange(1000, -5).start).toBe(1000 - SNAPSHOT_LINES);
    expect(snapshotRange(1000, Number.NaN).start).toBe(1000 - SNAPSHOT_LINES);
  });

  it("counts back from the cursor, not the bottom of the grid", () => {
    // 30-row grid, output ends on row 11: the blank padding below must not eat the budget.
    expect(snapshotRange(30, 3, 11)).toEqual({ start: 9, end: 11 });
  });

  it("keeps the cursor row inside the buffer", () => {
    expect(snapshotRange(10, 5, 99)).toEqual({ start: 5, end: 9 });
    expect(snapshotRange(10, 5, -3)).toEqual({ start: 0, end: 0 });
  });

  it("never hands out more than the ceiling", () => {
    expect(snapshotRange(100_000, 50_000).start).toBe(100_000 - SNAPSHOT_MAX_LINES);
  });
});

describe("clampTail", () => {
  it("leaves text that already fits", () => {
    expect(clampTail("a\nb\nc", 100)).toBe("a\nb\nc");
  });

  it("keeps the end and cuts at a line boundary", () => {
    // "aaaa\nbbbb\ncccc" is 14 chars; a budget of 6 lands inside "bbbb" and must drop it whole.
    expect(clampTail("aaaa\nbbbb\ncccc", 6)).toBe("cccc");
  });

  it("cuts mid-line when one line is longer than the budget", () => {
    expect(clampTail("x".repeat(20), 5)).toBe("xxxxx");
  });

  it("returns nothing for a budget of nothing", () => {
    expect(clampTail("anything", 0)).toBe("");
  });
});

describe("trimTrailingBlanks", () => {
  it("drops the empty rows under the last output", () => {
    expect(trimTrailingBlanks(["a", "b", "", "   ", ""])).toEqual(["a", "b"]);
  });

  it("keeps blank lines that sit between content", () => {
    expect(trimTrailingBlanks(["a", "", "b"])).toEqual(["a", "", "b"]);
  });

  it("copes with nothing but blanks", () => {
    expect(trimTrailingBlanks(["", "  "])).toEqual([]);
  });
});
