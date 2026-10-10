import { describe, expect, it } from "vitest";
import { leaf, leaves, splitLeaf } from "./layout/tree";
import { parseSession, parseWindow, remapTree, ResumeOffers } from "./session";

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

describe("parseSession across versions", () => {
  const tab = { tree: { kind: "leaf", id: "p1" }, focused: "p1", panes: [{ id: "p1", kind: "terminal", cwd: "/w" }] };

  it("migrates a v1 file into one window rather than discarding it", () => {
    const got = parseSession(JSON.stringify({ version: 1, active: 0, tabs: [tab] }));
    expect(got?.version).toBe(2);
    expect(got?.windows).toHaveLength(1);
    expect(got?.windows[0].tabs).toEqual([tab]);
    expect(got?.windows[0].key).toBe("w-1");
  });

  it("keeps the active index when migrating", () => {
    const got = parseSession(JSON.stringify({ version: 1, active: 2, tabs: [tab, tab, tab] }));
    expect(got?.windows[0].active).toBe(2);
  });

  it("reads a v2 file with several windows", () => {
    const raw = JSON.stringify({ version: 2, windows: [{ key: "a", active: 0, tabs: [tab] }, { key: "b", active: 0, tabs: [tab, tab] }] });
    const got = parseSession(raw);
    expect(got?.windows.map((w) => [w.key, w.tabs.length])).toEqual([["a", 1], ["b", 2]]);
  });

  it("drops windows with no tabs, and the whole thing when none are left", () => {
    const raw = JSON.stringify({ version: 2, windows: [{ key: "a", active: 0, tabs: [] }, { key: "b", active: 0, tabs: [tab] }] });
    expect(parseSession(raw)?.windows.map((w) => w.key)).toEqual(["b"]);
    expect(parseSession(JSON.stringify({ version: 2, windows: [{ key: "a", active: 0, tabs: [] }] }))).toBeNull();
  });

  it("refuses a future version and junk, as before", () => {
    expect(parseSession(JSON.stringify({ version: 3, windows: [] }))).toBeNull();
    expect(parseSession("not json")).toBeNull();
    expect(parseSession("")).toBeNull();
  });

  it("parseWindow reads a single window, keyed or not", () => {
    expect(parseWindow(JSON.stringify({ active: 1, tabs: [tab, tab] }))?.active).toBe(1);
    expect(parseWindow(JSON.stringify({ key: "k", active: 0, tabs: [tab] }))?.key).toBe("k");
    expect(parseWindow(JSON.stringify({ active: 0, tabs: [] }))).toBeNull();
  });
});

describe("ResumeOffers", () => {
  const offer = { title: "wate restart work", sessionId: "s-1" };

  it("keeps a restored pane's offer when nothing is running in it", () => {
    // The bug this is here for: after a restart the pane has no live session, the save wrote
    // nothing, and the offer was gone by the second restart.
    const o = new ResumeOffers();
    o.remember("pane-1", offer);
    expect(o.forSave("pane-1", undefined)).toEqual(offer);
    // Still there on the save after that, and the one after that.
    expect(o.forSave("pane-1", undefined)).toEqual(offer);
  });

  it("lets a live session take the pane over for good", () => {
    const o = new ResumeOffers();
    o.remember("pane-1", offer);
    const live = { title: "what is running now", sessionId: "s-2" };
    expect(o.forSave("pane-1", live)).toEqual(live);
    // The old pointer is retired: offering a session from before the restart once this pane
    // falls idle again would be worse than offering nothing.
    expect(o.forSave("pane-1", undefined)).toBeUndefined();
  });

  it("knows nothing about a pane that was not restored", () => {
    const o = new ResumeOffers();
    expect(o.forSave("pane-9", undefined)).toBeUndefined();
    o.remember("pane-9", undefined);
    expect(o.forSave("pane-9", undefined)).toBeUndefined();
  });

  it("forgets a pane that is gone", () => {
    const o = new ResumeOffers();
    o.remember("pane-1", offer);
    o.forget("pane-1");
    expect(o.forSave("pane-1", undefined)).toBeUndefined();
  });
});
