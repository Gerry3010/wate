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

describe("fromImported", () => {
  it("turns importer layouts into saved tabs with pane ids", async () => {
    const { fromImported } = await import("./session");
    const tabs = fromImported([
      {
        title: "Work",
        color: "blue",
        group: "Team",
        root: {
          kind: "split",
          dir: "row",
          ratio: 0.25,
          a: { kind: "leaf", cwd: "/a" },
          b: { kind: "split", dir: "col", ratio: 2, a: { kind: "leaf", cwd: "/b", focused: true }, b: { kind: "leaf", cwd: "/c" } },
        },
      },
      { title: "", color: "", root: null },
      { title: "", color: "", root: { kind: "leaf", cwd: "/d" } },
    ]);
    expect(tabs).toHaveLength(2);
    const [t1, t2] = tabs;
    expect(t1.title).toBe("Team › Work");
    expect(t1.color).toBe("blue");
    expect(t1.panes.map((p) => (p.kind === "terminal" ? p.cwd : ""))).toEqual(["/a", "/b", "/c"]);
    expect(t1.tree.kind).toBe("split");
    if (t1.tree.kind === "split") {
      expect(t1.tree.ratio).toBe(0.25);
      expect(t1.tree.b.kind === "split" && t1.tree.b.ratio).toBe(0.9); // clamped
    }
    expect(t1.focused).toBe(t1.panes[1].id);
    expect(t2.title).toBeUndefined();
    expect(t2.focused).toBe(t2.panes[0].id);
  });
});
