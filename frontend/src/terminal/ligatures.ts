/**
 * Programming ligatures without @xterm/addon-ligatures.
 *
 * The addon wants to read the font's GSUB table, which a WebView without `queryLocalFonts`
 * (WebKitGTK) can never do — it then falls back to a fixed list and, for every character of
 * every row, tests it against all 62 entries. The renderer asks per attribute run, per row,
 * per frame, so a colourful TUI pays for that list hundreds of thousands of times a second.
 *
 * Same list, same result, but a 128-entry lookup decides in one array load whether a
 * character can start a ligature at all — and almost none can.
 */

import { perf } from "../perf";

/** The ligature set the addon falls back to (Iosevka's `calt`), longest first so `<==>` beats `<=`. */
export const COMMON_LIGATURES: readonly string[] = [
  "<--", "<---", "<<-", "<-", "->", "->>", "-->", "--->",
  "<==", "<===", "<<=", "<=", "=>", "=>>", "==>", "===>", ">=", ">>=",
  "<->", "<-->", "<--->", "<---->", "<=>", "<==>", "<===>", "<====>", "::", ":::",
  "<~~", "</", "</>", "/>", "~~>", "==", "!=", "/=", "~=", "<>", "===", "!==", "!===",
  "<:", ":=", "*=", "*+", "<*", "<*>", "*>", "<|", "<|>", "|>", "+*", "=*", "=:", ":>",
  "/*", "*/", "+++", "<!--", "<!---",
].sort((a, b) => b.length - a.length);

/** Ligatures grouped by their first character, with a flat gate for "can this start one?". */
export interface LigatureTable {
  /** 128 bytes: 1 where an ASCII code starts at least one ligature. */
  gate: Uint8Array;
  /** Candidates per starting code, longest first. */
  byFirst: (string[] | undefined)[];
}

export function buildLigatureTable(ligatures: readonly string[] = COMMON_LIGATURES): LigatureTable {
  const gate = new Uint8Array(128);
  const byFirst: (string[] | undefined)[] = new Array(128);
  for (const lig of [...ligatures].sort((a, b) => b.length - a.length)) {
    const code = lig.charCodeAt(0);
    if (code > 127) continue;
    gate[code] = 1;
    (byFirst[code] ??= []).push(lig);
  }
  return { gate, byFirst };
}

const DEFAULT_TABLE = buildLigatureTable();

/**
 * The [start, end) ranges of `text` that render as one glyph group. Ranges never overlap and
 * come in ascending order — and every array is freshly allocated, because xterm rewrites the
 * tuples in place when it converts string indices to cell columns.
 */
export function ligatureRanges(text: string, table: LigatureTable = DEFAULT_TABLE): [number, number][] {
  const ranges: [number, number][] = [];
  for (let i = 0; i < text.length; i++) {
    const code = text.charCodeAt(i);
    if (code > 127 || table.gate[code] === 0) continue;
    const candidates = table.byFirst[code]!;
    for (const lig of candidates) {
      if (text.startsWith(lig, i)) {
        ranges.push([i, i + lig.length]);
        i += lig.length - 1;
        break;
      }
    }
  }
  return ranges;
}

/** Character joiner for `term.registerCharacterJoiner`, counted for `ctl debug`. */
export function ligatureJoiner(table: LigatureTable = DEFAULT_TABLE): (text: string) => [number, number][] {
  return (text) => {
    perf.joinerCalls++;
    perf.joinerChars += text.length;
    if (!perf.timing) return ligatureRanges(text, table);
    const t = performance.now();
    const ranges = ligatureRanges(text, table);
    perf.joinerMs += performance.now() - t;
    return ranges;
  };
}
