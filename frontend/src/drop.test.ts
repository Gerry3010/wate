import { describe, expect, it } from "vitest";
import { boxAt, dropText, dropZone, droppedPaths, quotePath, zoneRect, zoneSplit } from "./drop";

/** The parts of DataTransfer a drop handler reads. */
function transfer(data: Record<string, string>): DataTransfer {
  return { getData: (type: string) => data[type] ?? "" } as DataTransfer;
}

describe("droppedPaths", () => {
  it("decodes file URIs from the uri-list", () => {
    const dt = transfer({ "text/uri-list": "file:///home/gerry/Bilder/mon%20chat.png" });
    expect(droppedPaths(dt)).toEqual(["/home/gerry/Bilder/mon chat.png"]);
  });

  it("takes every file of a multi-file drop and skips comments", () => {
    const dt = transfer({ "text/uri-list": "# comment\nfile:///a/one.png\r\nfile://localhost/b/two.png\r\n" });
    expect(droppedPaths(dt)).toEqual(["/a/one.png", "/b/two.png"]);
  });

  it("falls back to plain text that looks like a path", () => {
    expect(droppedPaths(transfer({ "text/plain": "/tmp/shot.png" }))).toEqual(["/tmp/shot.png"]);
    expect(droppedPaths(transfer({ "text/plain": "https://example.com" }))).toEqual([]);
  });

  it("has nothing to say about an empty drop", () => {
    expect(droppedPaths(null)).toEqual([]);
    expect(droppedPaths(transfer({}))).toEqual([]);
  });
});

describe("quotePath", () => {
  it("leaves a plain path alone", () => {
    expect(quotePath("/home/gerry/a-file_1.png")).toBe("/home/gerry/a-file_1.png");
  });

  it("quotes spaces and single quotes", () => {
    expect(quotePath("/tmp/mon chat.png")).toBe("'/tmp/mon chat.png'");
    expect(quotePath("/tmp/it's.png")).toBe(`'/tmp/it'\\''s.png'`);
  });
});

describe("dropText", () => {
  it("joins the paths and leaves the cursor a space to type on", () => {
    expect(dropText(["/a/one.png", "/b/two three.png"])).toBe("/a/one.png '/b/two three.png' ");
  });

  it("is empty when nothing was dropped", () => {
    expect(dropText([])).toBe("");
  });
});

describe("dropZone", () => {
  const pane = { x: 100, y: 100, w: 800, h: 400 }; // bands: 140px (capped) and 100px

  it("calls the middle the middle", () => {
    expect(dropZone(pane, 500, 300)).toBe("center");
    expect(dropZone(pane, 260, 210)).toBe("center"); // just inside both bands
  });

  it("names the side a drop is closest to", () => {
    expect(dropZone(pane, 110, 300)).toBe("left");
    expect(dropZone(pane, 890, 300)).toBe("right");
    expect(dropZone(pane, 500, 110)).toBe("up");
    expect(dropZone(pane, 500, 490)).toBe("down");
  });

  it("resolves a corner to whichever band it reaches deeper into", () => {
    // 10px into a 140px band horizontally (7%), 40px into a 100px band vertically (40%).
    expect(dropZone(pane, 110, 140)).toBe("left");
    expect(dropZone(pane, 200, 105)).toBe("up");
  });

  it("caps the band so a wide pane keeps a middle", () => {
    // A quarter of 800 would be 200px; the cap keeps it at 140.
    expect(dropZone(pane, 250, 300)).toBe("center");
    expect(dropZone(pane, 230, 300)).toBe("left");
  });

  it("leaves a middle even in a tiny pane", () => {
    const tiny = { x: 0, y: 0, w: 80, h: 60 };
    expect(dropZone(tiny, 40, 30)).toBe("center");
    expect(dropZone(tiny, 2, 30)).toBe("left");
  });

  it("clamps a point outside the pane instead of inventing a zone", () => {
    expect(dropZone(pane, -50, 300)).toBe("left");
    expect(dropZone(pane, 500, 5000)).toBe("down");
    expect(dropZone({ x: 0, y: 0, w: 0, h: 0 }, 0, 0)).toBe("center");
  });
});

describe("zoneRect", () => {
  const pane = { x: 100, y: 100, w: 800, h: 400 };

  it("covers the half that would be split off", () => {
    expect(zoneRect(pane, "left")).toEqual({ x: 100, y: 100, w: 400, h: 400 });
    expect(zoneRect(pane, "right")).toEqual({ x: 500, y: 100, w: 400, h: 400 });
    expect(zoneRect(pane, "up")).toEqual({ x: 100, y: 100, w: 800, h: 200 });
    expect(zoneRect(pane, "down")).toEqual({ x: 100, y: 300, w: 800, h: 200 });
  });

  it("covers the whole pane for the middle", () => {
    expect(zoneRect(pane, "center")).toEqual(pane);
  });
});

describe("zoneSplit", () => {
  it("puts left and up in the first slot", () => {
    expect(zoneSplit("left")).toEqual({ dir: "row", before: true });
    expect(zoneSplit("right")).toEqual({ dir: "row", before: false });
    expect(zoneSplit("up")).toEqual({ dir: "col", before: true });
    expect(zoneSplit("down")).toEqual({ dir: "col", before: false });
  });

  it("does not split for the middle", () => {
    expect(zoneSplit("center")).toBeNull();
  });
});

describe("boxAt", () => {
  const panes = [
    { id: "a", x: 0, y: 0, w: 100, h: 100 },
    { id: "b", x: 100, y: 0, w: 100, h: 100 },
  ];

  it("finds the pane under the point", () => {
    expect(boxAt(panes, 50, 50)?.id).toBe("a");
    expect(boxAt(panes, 150, 50)?.id).toBe("b");
    expect(boxAt(panes, 100, 50)?.id).toBe("b"); // the shared edge belongs to the second
  });

  it("has nothing to say about a point next to every pane", () => {
    expect(boxAt(panes, 50, 500)).toBeUndefined();
  });
});
