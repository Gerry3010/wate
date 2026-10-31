/**
 * What the session panel shows and in which order: a status filter, a search, a sort key and
 * an optional grouping by directory. Pure, so it can be tested without a panel around it.
 */

import { rowCwd, rowTitle, type Row } from "./groups";

/**
 * input: Claude is waiting for you (a permission prompt, or idle at its prompt).
 * questions: Claude's last word was a question for you — answered or not, until the next prompt.
 */
export type StatusFilter = "all" | "input" | "questions" | "working" | "done";
export type SortKey = "started" | "name" | "id" | "dir";

export interface View {
  filter: StatusFilter;
  sort: SortKey;
  byDir: boolean;
  query: string;
}

export const DEFAULT_VIEW: View = { filter: "all", sort: "started", byDir: false, query: "" };

export const FILTERS: [StatusFilter, string][] = [
  ["all", "All"],
  ["input", "Input"],
  ["questions", "Questions"],
  ["working", "Working"],
  ["done", "Done"],
];

export const SORTS: [SortKey, string][] = [
  ["started", "Start order"],
  ["name", "Name"],
  ["id", "Session id"],
  ["dir", "Directory"],
];

export function matchesFilter(r: Row, f: StatusFilter): boolean {
  const s = r.live;
  switch (f) {
    case "all":
      return true;
    case "input":
      return s?.status === "waiting";
    case "questions":
      return !!s?.asking;
    case "working":
      return s?.status === "running";
    case "done":
      return s?.status === "done";
  }
}

/** Every word has to turn up somewhere: name, label, directory, session id or last message. */
export function matchesQuery(r: Row, query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return true;
  const hay = [rowTitle(r, ""), r.saved?.label, rowCwd(r), r.id, r.live?.message].filter(Boolean).join("\n").toLowerCase();
  return words.every((w) => hay.includes(w));
}

const collator = new Intl.Collator(undefined, { sensitivity: "base", numeric: true });

/** Oldest session first; ended favourites, with no start time of their own, keep their order last. */
function byStart(rows: Row[]): Row[] {
  // Go writes up to nine digits of a second; Date.parse is only sure of three.
  const at = (r: Row) => (r.live?.started_at ? Date.parse(r.live.started_at.replace(/(\.\d{3})\d+/, "$1")) : Number.POSITIVE_INFINITY);
  return [...rows].sort((a, b) => {
    const d = at(a) - at(b);
    return Number.isNaN(d) ? 0 : d;
  });
}

/** By start time, or by name, id or directory with ties broken by name. */
export function sortRows(rows: Row[], key: SortKey): Row[] {
  if (key === "started") return byStart(rows);
  const by: Record<Exclude<SortKey, "started">, (r: Row) => string> = {
    name: (r) => rowTitle(r, rowCwd(r)),
    id: (r) => r.id,
    dir: (r) => rowCwd(r),
  };
  const f = by[key];
  return [...rows].sort((a, b) => collator.compare(f(a), f(b)) || collator.compare(rowTitle(a, ""), rowTitle(b, "")));
}

export function applyView(rows: Row[], v: View): Row[] {
  return sortRows(
    rows.filter((r) => matchesFilter(r, v.filter) && matchesQuery(r, v.query)),
    v.sort,
  );
}

/** Rows under their directory, directories in alphabetical order, rows in the order given. */
export function groupByDir(rows: Row[]): { dir: string; rows: Row[] }[] {
  const groups = new Map<string, Row[]>();
  for (const r of rows) {
    const dir = rowCwd(r);
    const list = groups.get(dir);
    if (list) list.push(r);
    else groups.set(dir, [r]);
  }
  return [...groups.entries()].sort(([a], [b]) => collator.compare(a, b)).map(([dir, rows]) => ({ dir, rows }));
}

/** How many sessions each filter would leave, for the chips. The search applies; the filter not. */
export function countByFilter(rows: Row[], query: string): Record<StatusFilter, number> {
  const out = { all: 0, input: 0, questions: 0, working: 0, done: 0 } as Record<StatusFilter, number>;
  for (const r of rows) {
    if (!matchesQuery(r, query)) continue;
    for (const [f] of FILTERS) if (matchesFilter(r, f)) out[f]++;
  }
  return out;
}

/** The view as remembered, with anything unknown replaced by the default. */
export function parseView(raw: string | null): View {
  try {
    const v = JSON.parse(raw ?? "{}") as Partial<View>;
    return {
      filter: FILTERS.some(([f]) => f === v.filter) ? v.filter! : DEFAULT_VIEW.filter,
      sort: SORTS.some(([s]) => s === v.sort) ? v.sort! : DEFAULT_VIEW.sort,
      byDir: typeof v.byDir === "boolean" ? v.byDir : DEFAULT_VIEW.byDir,
      // The search is for now, not for the next start.
      query: "",
    };
  } catch {
    return { ...DEFAULT_VIEW };
  }
}
