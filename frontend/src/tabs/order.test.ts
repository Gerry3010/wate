import { describe, expect, it } from "vitest";
import { dropIndex, insertionIndex, moveItem } from "./order";

describe("moveItem", () => {
  it("moves an item forwards and backwards", () => {
    const abc = ["a", "b", "c", "d"];
    expect(moveItem(abc, 0, 2)).toEqual(["b", "c", "a", "d"]);
    expect(moveItem(abc, 3, 1)).toEqual(["a", "d", "b", "c"]);
  });

  it("returns the same array when nothing would move", () => {
    const abc = ["a", "b", "c"];
    expect(moveItem(abc, 1, 1)).toBe(abc);
    expect(moveItem(abc, 7, 0)).toBe(abc);
    expect(moveItem(abc, -1, 0)).toBe(abc);
  });

  it("clamps a target past either end", () => {
    const abc = ["a", "b", "c"];
    expect(moveItem(abc, 0, 99)).toEqual(["b", "c", "a"]);
    expect(moveItem(abc, 2, -5)).toEqual(["c", "a", "b"]);
  });
});

describe("insertionIndex", () => {
  // Three 100px tabs: centres at 50, 150, 250.
  const centers = [50, 150, 250];

  it("counts the centres left of the pointer", () => {
    expect(insertionIndex(centers, 0)).toBe(0);
    expect(insertionIndex(centers, 49)).toBe(0);
    expect(insertionIndex(centers, 51)).toBe(1);
    expect(insertionIndex(centers, 200)).toBe(2);
    expect(insertionIndex(centers, 9999)).toBe(3);
  });

  it("has no gaps for an empty bar", () => {
    expect(insertionIndex([], 42)).toBe(0);
  });
});

describe("dropIndex", () => {
  it("accounts for the dragged tab leaving its own slot", () => {
    // Dragging tab 0 past the second centre: gap 2, but once "a" is lifted out it lands at 1.
    expect(dropIndex(0, 2)).toBe(1);
    // Dragging tab 2 to the front: gap 0 is already the answer.
    expect(dropIndex(2, 0)).toBe(0);
    // Staying put either side of its own centre.
    expect(dropIndex(1, 1)).toBe(1);
    expect(dropIndex(1, 2)).toBe(1);
  });
});
