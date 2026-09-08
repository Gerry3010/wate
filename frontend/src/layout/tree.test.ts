import { describe, expect, it } from "vitest";
import { focusAfterClose, leaf, leaves, neighbor, removeLeaf, setRatio, splitLeaf, type Rect } from "./tree";

describe("split tree", () => {
  it("splits and lists leaves in order", () => {
    let t = splitLeaf(leaf("a"), "a", "row", "b");
    t = splitLeaf(t, "a", "col", "c");
    expect(leaves(t)).toEqual(["a", "c", "b"]);
    expect(t.kind).toBe("split");
  });

  it("removes leaves and collapses splits", () => {
    let t = splitLeaf(leaf("a"), "a", "row", "b");
    t = splitLeaf(t, "b", "col", "c");
    expect(leaves(removeLeaf(t, "c")!)).toEqual(["a", "b"]);
    expect(removeLeaf(t, "a")!.kind).toBe("split");
    expect(removeLeaf(leaf("a"), "a")).toBeNull();
  });

  it("clamps ratios", () => {
    const t = splitLeaf(leaf("a"), "a", "row", "b", "s");
    expect((setRatio(t, "s", 5) as any).ratio).toBe(0.9);
    expect((setRatio(t, "s", -1) as any).ratio).toBe(0.1);
  });

  it("picks the sibling subtree after close", () => {
    let t = splitLeaf(leaf("a"), "a", "row", "b");
    t = splitLeaf(t, "b", "col", "c");
    expect(focusAfterClose(t, "c")).toBe("b");
    expect(focusAfterClose(t, "a")).toBe("b");
    expect(focusAfterClose(leaf("a"), "a")).toBeNull();
  });
});

describe("neighbor", () => {
  // +-----+-----+
  // |  a  |  b  |
  // |     +-----+
  // |     |  c  |
  // +-----+-----+
  const rects: Rect[] = [
    { id: "a", x: 0, y: 0, w: 100, h: 100 },
    { id: "b", x: 100, y: 0, w: 100, h: 50 },
    { id: "c", x: 100, y: 50, w: 100, h: 50 },
  ];
  it("moves along touching edges", () => {
    expect(neighbor(rects, "a", "right")).toBe("b");
    expect(neighbor(rects, "b", "down")).toBe("c");
    expect(neighbor(rects, "c", "up")).toBe("b");
    expect(neighbor(rects, "c", "left")).toBe("a");
  });
  it("returns null at the edge", () => {
    expect(neighbor(rects, "a", "left")).toBeNull();
    expect(neighbor(rects, "b", "up")).toBeNull();
    expect(neighbor(rects, "zzz", "up")).toBeNull();
  });
  it("prefers the larger overlap among equally distant panes", () => {
    const r: Rect[] = [
      { id: "top", x: 0, y: 0, w: 100, h: 30 },
      { id: "bottom", x: 0, y: 30, w: 100, h: 70 },
      { id: "x", x: 100, y: 0, w: 100, h: 100 },
    ];
    expect(neighbor(r, "x", "left")).toBe("bottom");
  });
});
