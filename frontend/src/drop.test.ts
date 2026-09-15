import { describe, expect, it } from "vitest";
import { dropText, droppedPaths, quotePath } from "./drop";

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
