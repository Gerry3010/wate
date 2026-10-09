/**
 * Reading a pane's screen back out, as plain text.
 *
 * The arithmetic lives here, away from xterm.js, so it can be tested — vitest runs without a
 * DOM, and a terminal buffer is the most DOM-bound thing in the app.
 */

/** Lines returned when the caller does not say. One screenful of a tall terminal, plus room. */
export const SNAPSHOT_LINES = 200;

/** The most a single read may ever return, however much is asked for. */
export const SNAPSHOT_MAX_LINES = 2000;

/**
 * Character ceiling for one read. UTF-16 code units, not bytes: counting real bytes would mean
 * encoding the whole string to measure it, and the point of the cap is to keep a reply from
 * running away, not to hit an exact size.
 */
export const SNAPSHOT_MAX_CHARS = 64 * 1024;

/**
 * The buffer rows a read covers: the last `lines` ending at `lastRow`.
 *
 * `lastRow` is where the output ends — the cursor's row, not the bottom of the grid. A terminal
 * grid is always full height, so the rows below the cursor are blank padding; counting back from
 * there would spend the whole budget on nothing and hand back an empty answer.
 *
 * `end` is inclusive, and `end < start` means there is nothing to read.
 */
export function snapshotRange(length: number, lines = SNAPSHOT_LINES, lastRow?: number): { start: number; end: number } {
  if (!Number.isFinite(length) || length <= 0) return { start: 0, end: -1 };
  const want = Number.isFinite(lines) && lines > 0 ? Math.floor(lines) : SNAPSHOT_LINES;
  const n = Math.min(want, SNAPSHOT_MAX_LINES);
  const end = Number.isFinite(lastRow) ? Math.min(Math.max(0, Math.floor(lastRow!)), length - 1) : length - 1;
  return { start: Math.max(0, end - n + 1), end };
}

/**
 * Keep the end of `text`, at most `maxChars` of it, cut at a line boundary.
 *
 * The end is what matters in a terminal — the newest output — and a half line at the top reads
 * as corrupted output rather than as a truncation. If a single line is longer than the budget
 * there is no boundary to cut at, and it is cut mid-line rather than dropped entirely.
 */
export function clampTail(text: string, maxChars = SNAPSHOT_MAX_CHARS): string {
  if (maxChars <= 0) return "";
  if (text.length <= maxChars) return text;
  const from = text.length - maxChars;
  const nl = text.indexOf("\n", from);
  return nl >= 0 ? text.slice(nl + 1) : text.slice(from);
}

/** Drop the empty rows a terminal grid carries below its last line of output. */
export function trimTrailingBlanks(lines: string[]): string[] {
  let end = lines.length;
  while (end > 0 && lines[end - 1].trim() === "") end--;
  return lines.slice(0, end);
}
