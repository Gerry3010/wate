import { describe, expect, it } from "vitest";
import { COMMON_LIGATURES, buildLigatureTable, ligatureRanges } from "./ligatures";

/** What @xterm/addon-ligatures does in its fallback path, kept here as the reference. */
function referenceRanges(text: string, ligatures: readonly string[]): [number, number][] {
  const sorted = [...ligatures].sort((a, b) => b.length - a.length);
  const ranges: [number, number][] = [];
  for (let i = 0; i < text.length; i++) {
    for (const lig of sorted) {
      if (text.startsWith(lig, i)) {
        ranges.push([i, i + lig.length]);
        i += lig.length - 1;
        break;
      }
    }
  }
  return ranges;
}

describe("ligatureRanges", () => {
  it("joins a ligature and leaves the rest alone", () => {
    expect(ligatureRanges("a => b")).toEqual([[2, 4]]);
  });

  it("prefers the longest match", () => {
    expect(ligatureRanges("a <=> b")).toEqual([[2, 5]]);
    expect(ligatureRanges("!==")).toEqual([[0, 3]]);
  });

  it("returns non-overlapping ranges in order", () => {
    expect(ligatureRanges("=>=>")).toEqual([
      [0, 2],
      [2, 4],
    ]);
  });

  it("finds nothing in ordinary text", () => {
    expect(ligatureRanges("the quick brown fox jumps over the lazy dog")).toEqual([]);
    expect(ligatureRanges("╭─  ~/Sync-Projekte/GO-Projekte/wate   main ✔")).toEqual([]);
  });

  it("hands out fresh arrays (xterm rewrites them in place)", () => {
    const first = ligatureRanges("a => b");
    first[0][0] = 99;
    expect(ligatureRanges("a => b")).toEqual([[2, 4]]);
  });

  it("matches the reference implementation on a mixed corpus", () => {
    const corpus = [
      "if (a !== b) return a <= b;",
      "const f = (x) => x ?? 0; /* note */",
      "std::vector<int> v; v |> map |> sum",
      "x <--- y ~~> z <====> w",
      "no ligatures here at all, just words",
      "diff --git a/x b/x  @@ -1,3 +1,4 @@",
      "|> <| <*> +++ :: ::: =: :> /= ~= <>",
    ];
    for (const line of corpus) {
      expect(ligatureRanges(line)).toEqual(referenceRanges(line, COMMON_LIGATURES));
    }
  });

  it("takes a custom table", () => {
    const table = buildLigatureTable(["<3"]);
    expect(ligatureRanges("i <3 you => yes", table)).toEqual([[2, 4]]);
  });
});
