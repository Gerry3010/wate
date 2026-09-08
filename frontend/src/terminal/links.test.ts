import { describe, expect, it } from "vitest";
import { findPaths } from "./links";

describe("findPaths", () => {
  it("finds absolute, relative, home and dotted paths", () => {
    const t = findPaths("see /etc/hosts and ./src/main.go:12:3, also ~/notes.md or README.md.").map((p) => p.text);
    expect(t).toEqual(["/etc/hosts", "./src/main.go:12:3", "~/notes.md", "README.md"]);
  });
  it("skips URLs (handled by the web-links addon)", () => {
    expect(findPaths("open https://example.com/x.html now").map((p) => p.text)).toEqual([]);
  });
  it("keeps short dotted words as candidates; existence is checked later", () => {
    expect(findPaths("e.g. a.c").map((p) => p.text)).toEqual(["e.g", "a.c"]);
  });
  it("reports column ranges", () => {
    const [p] = findPaths("cat foo/bar.txt");
    expect(p).toEqual({ text: "foo/bar.txt", start: 4, end: 15 });
  });
  it("handles go test output", () => {
    expect(findPaths("    config_test.go:14: unexpected").map((p) => p.text)).toEqual(["config_test.go:14"]);
  });
});
