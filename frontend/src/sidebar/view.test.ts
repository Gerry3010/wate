import { describe, expect, it } from "vitest";
import type { Session } from "../api";
import type { Row } from "./groups";
import { DEFAULT_VIEW, applyView, countByFilter, groupByDir, matchesQuery, parseView, sortRows } from "./view";

// Status is a generated enum; the tests write its values as the plain strings they are.
const row = (id: string, extra: Record<string, unknown> = {}): Row => ({
  id,
  live: { pane_id: "p-" + id, tab_id: "t", status: "running", session_id: id, title: id, cwd: "/w/" + id, message: "", asking: false, ...extra } as Session,
  ended: false,
  favorite: false,
});

// Started in this order; Go writes fractions of a second with as many digits as they need.
const rows = [
  row("c-work", { title: "Tidy the tab bar", cwd: "/w/wate", started_at: "2026-10-31T12:00:00.5+01:00" }),
  row("a-wait", { title: "Push to main", status: "waiting", cwd: "/w/chattr", message: "Claude needs your permission to use Bash", started_at: "2026-10-31T12:00:00.123456789+01:00" }),
  row("b-ask", { title: "Pick a colour", status: "done", asking: true, cwd: "/w/wate", started_at: "2026-10-31T12:01:00+01:00" }),
  row("d-done", { title: "Release notes", status: "done", cwd: "/w/chattr", started_at: "2026-10-31T12:02:00+01:00" }),
];
const ids = (rs: Row[]) => rs.map((r) => r.id);

describe("the status filter", () => {
  it("finds what is waiting for input, and what asked a question", () => {
    expect(ids(applyView(rows, { ...DEFAULT_VIEW, filter: "input" }))).toEqual(["a-wait"]);
    expect(ids(applyView(rows, { ...DEFAULT_VIEW, filter: "questions" }))).toEqual(["b-ask"]);
    expect(ids(applyView(rows, { ...DEFAULT_VIEW, filter: "working" }))).toEqual(["c-work"]);
    expect(ids(applyView(rows, { ...DEFAULT_VIEW, filter: "done" }))).toEqual(["b-ask", "d-done"]);
    expect(ids(applyView(rows, DEFAULT_VIEW))).toEqual(["a-wait", "c-work", "b-ask", "d-done"]);
  });

  it("counts each filter over what the search leaves", () => {
    expect(countByFilter(rows, "")).toEqual({ all: 4, input: 1, questions: 1, working: 1, done: 2 });
    expect(countByFilter(rows, "chattr")).toEqual({ all: 2, input: 1, questions: 0, working: 0, done: 1 });
  });
});

describe("the search", () => {
  it("looks at name, directory, session id and the last message, every word", () => {
    expect(matchesQuery(rows[0], "tab bar")).toBe(true);
    expect(matchesQuery(rows[0], "wate")).toBe(true);
    expect(matchesQuery(rows[1], "permission chattr")).toBe(true);
    expect(matchesQuery(rows[1], "a-wa")).toBe(true);
    expect(matchesQuery(rows[1], "permission wate")).toBe(false);
    expect(matchesQuery(rows[0], "   ")).toBe(true);
  });
});

describe("sorting and grouping", () => {
  it("sorts by start time, name, id or directory", () => {
    expect(ids(sortRows(rows, "name"))).toEqual(["b-ask", "a-wait", "d-done", "c-work"]);
    expect(ids(sortRows(rows, "id"))).toEqual(["a-wait", "b-ask", "c-work", "d-done"]);
    // Same directory: by name.
    expect(ids(sortRows(rows, "dir"))).toEqual(["a-wait", "d-done", "b-ask", "c-work"]);
    // 0.123… s before 0.5 s, which a string comparison of the two would get the wrong way round.
    expect(ids(sortRows(rows, "started"))).toEqual(["a-wait", "c-work", "b-ask", "d-done"]);
  });

  it("puts the fullest context first, and sessions without one last", () => {
    const ctx = [
      row("low", { title: "Low", context_percent: 12, context: { tokens: 120_000, window: 1_000_000 } }),
      // No percent from Go yet: worked out from tokens and window.
      row("high", { title: "High", context: { tokens: 170_000, window: 200_000 } }),
      row("none", { title: "None" }),
      row("mid", { title: "Mid", context_percent: 40 }),
      row("also-none", { title: "Also none" }),
    ];
    expect(ids(sortRows(ctx, "context"))).toEqual(["high", "mid", "low", "also-none", "none"]);
    expect(ids(applyView(ctx, { ...DEFAULT_VIEW, sort: "context", filter: "working" }))[0]).toBe("high");
    expect(parseView(JSON.stringify({ sort: "context" })).sort).toBe("context");
  });

  it("groups by directory, keeping the order inside each group", () => {
    const g = groupByDir(sortRows(rows, "name"));
    expect(g.map((x) => [x.dir, ids(x.rows)])).toEqual([
      ["/w/chattr", ["a-wait", "d-done"]],
      ["/w/wate", ["b-ask", "c-work"]],
    ]);
  });
});

describe("the remembered view", () => {
  it("comes back without the search and with nonsense replaced", () => {
    expect(parseView(JSON.stringify({ filter: "questions", sort: "dir", byDir: true, query: "x" }))).toEqual({
      filter: "questions",
      sort: "dir",
      byDir: true,
      query: "",
    });
    expect(parseView(JSON.stringify({ filter: "bogus", sort: 3 }))).toEqual(DEFAULT_VIEW);
    expect(parseView("{not json")).toEqual(DEFAULT_VIEW);
    expect(parseView(null)).toEqual(DEFAULT_VIEW);
  });
});
