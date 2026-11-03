import { describe, expect, it } from "vitest";
import { dockable, dragSizes, edgesFor, rootLayout, sizeKeys, type PanelSizes } from "./panel";

describe("rootLayout", () => {
  it("keeps a known layout and falls back to the drawer", () => {
    expect(rootLayout("top")).toBe("top");
    expect(rootLayout("overlay")).toBe("overlay");
    expect(rootLayout("sideways")).toBe("drawer");
    expect(rootLayout(undefined)).toBe("drawer");
  });
});

describe("docking and edges", () => {
  it("docks the drawer and the drop-down, never the overlay", () => {
    expect(dockable("drawer")).toBe(true);
    expect(dockable("top")).toBe(true);
    expect(dockable("overlay")).toBe(false);
  });

  it("drags the drawer by its left edge, the drop-down by its bottom, the overlay by any", () => {
    expect(edgesFor("drawer")).toEqual(["w"]);
    expect(edgesFor("top")).toEqual(["s"]);
    expect(edgesFor("overlay")).toHaveLength(8);
    expect(sizeKeys("drawer")).toEqual(["drawer_width"]);
    expect(sizeKeys("top")).toEqual(["top_height"]);
    expect(sizeKeys("overlay")).toEqual(["overlay_width", "overlay_height"]);
  });
});

describe("dragSizes", () => {
  const win = { width: 2000, height: 1000 };
  const sizes: PanelSizes = { drawer_width: 45, top_height: 50, overlay_width: 72, overlay_height: 78 };
  const drawer = { left: 1100, top: 38, right: 2000, bottom: 1000 };
  const top = { left: 0, top: 38, right: 2000, bottom: 538 };
  const overlay = { left: 280, top: 110, right: 1720, bottom: 890 }; // centred on (1000, 500)

  it("widens the drawer to wherever its left edge is let go, and touches nothing else", () => {
    expect(dragSizes("drawer", "w", drawer, 1400, 300, win, sizes)).toEqual({ ...sizes, drawer_width: 30 });
  });

  it("measures the drop-down from its top, below the tab bar", () => {
    expect(dragSizes("top", "s", top, 0, 638, win, sizes).top_height).toBe(60);
  });

  it("keeps the overlay centred: one edge dragged moves both", () => {
    // The right edge pulled 100 px further out makes it 200 px wider.
    const wider = dragSizes("overlay", "e", overlay, 1820, 500, win, sizes);
    expect(wider.overlay_width).toBe(82);
    expect(wider.overlay_height).toBe(78);
    // The left edge pulled in by as much makes it narrower by the same amount on both sides.
    expect(dragSizes("overlay", "w", overlay, 380, 500, win, sizes).overlay_width).toBe(62);
    // A corner changes both.
    expect(dragSizes("overlay", "se", overlay, 1600, 800, win, sizes)).toMatchObject({ overlay_width: 60, overlay_height: 60 });
    expect(dragSizes("overlay", "n", overlay, 0, 300, win, sizes)).toMatchObject({ overlay_width: 72, overlay_height: 40 });
  });

  it("keeps at least 320 × 160 px and at most 85 % of the window", () => {
    expect(dragSizes("drawer", "w", drawer, 1990, 0, win, sizes).drawer_width).toBe(16); // 320 of 2000
    expect(dragSizes("drawer", "w", drawer, 0, 0, win, sizes).drawer_width).toBe(85);
    expect(dragSizes("top", "s", top, 0, 40, win, sizes).top_height).toBe(16); // 160 of 1000
    expect(dragSizes("top", "s", top, 0, 5000, win, sizes).top_height).toBe(85);
    expect(dragSizes("overlay", "e", overlay, 1000, 500, win, sizes).overlay_width).toBe(16);
    expect(dragSizes("overlay", "e", overlay, 5000, 500, win, sizes).overlay_width).toBe(85);
  });

  it("gives way to the 85 % when the window is too small for the floor", () => {
    expect(dragSizes("drawer", "w", { ...drawer, right: 300 }, 290, 0, { width: 300, height: 1000 }, sizes).drawer_width).toBe(85);
  });

  it("rounds to a tenth of a percent", () => {
    expect(dragSizes("drawer", "w", drawer, 1234, 0, win, sizes).drawer_width).toBe(38.3);
  });
});
