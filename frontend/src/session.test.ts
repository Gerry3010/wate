import { describe, expect, it } from "vitest";
import { leaf, leaves, splitLeaf } from "./layout/tree";
import { parseSession, remapTree } from "./session";

describe("session", () => {
  it("rejects garbage and old versions", () => {
    expect(parseSession("")).toBeNull();
    expect(parseSession("{nope")).toBeNull();
    expect(parseSession(JSON.stringify({ version: 0, tabs: [{}] }))).toBeNull();
    expect(parseSession(JSON.stringify({ version: 1, tabs: [] }))).toBeNull();
  });
  it("remaps ids and drops panes that could not be recreated", () => {
    const t = splitLeaf(splitLeaf(leaf("a"), "a", "row", "b"), "b", "col", "c");
    const map = new Map([["a", "A"], ["c", "C"]]);
    const out = remapTree(t, map)!;
    expect(leaves(out)).toEqual(["A", "C"]);
    expect(remapTree(leaf("x"), map)).toBeNull();
  });
});
