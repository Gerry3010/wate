import { describe, expect, it } from "vitest";
import { groupSessions, rowCwd, rowTitle } from "./groups";
import type { SavedAgentSession, Session } from "../api";

const live = (id: string, extra: Partial<Session> = {}): Session =>
  ({ pane_id: "p-" + id, tab_id: "t", status: "running", session_id: id, title: "live " + id, cwd: "/live", ...extra }) as Session;

const saved = (id: string, extra: Partial<SavedAgentSession> = {}): SavedAgentSession =>
  ({ session_id: id, label: "", cwd: "/saved", title: "saved " + id, favorite: false, seen_at: "2026-10-01T00:00:00Z", ...extra }) as SavedAgentSession;

describe("groupSessions", () => {
  it("puts favourited live sessions in their own group", () => {
    const g = groupSessions([live("a"), live("b")], [saved("b", { favorite: true })]);
    expect(g.favorites.map((r) => r.id)).toEqual(["b"]);
    expect(g.others.map((r) => r.id)).toEqual(["a"]);
    expect(g.favorites[0].ended).toBe(false);
  });

  it("keeps a favourite listed once its session has ended", () => {
    const g = groupSessions([], [saved("gone", { favorite: true })]);
    expect(g.favorites.map((r) => [r.id, r.ended])).toEqual([["gone", true]]);
  });

  it("drops an ended session that was only renamed, never favourited", () => {
    const g = groupSessions([], [saved("gone", { label: "Perf-Jagd" })]);
    expect(g.favorites).toEqual([]);
    expect(g.others).toEqual([]);
  });

  it("lists ended favourites newest first, after the live ones", () => {
    const g = groupSessions(
      [live("now")],
      [
        saved("now", { favorite: true }),
        saved("old", { favorite: true, seen_at: "2026-09-01T00:00:00Z" }),
        saved("recent", { favorite: true, seen_at: "2026-09-30T00:00:00Z" }),
      ],
    );
    expect(g.favorites.map((r) => r.id)).toEqual(["now", "recent", "old"]);
  });

  it("still lists a session that has no id, and never saves against it", () => {
    const g = groupSessions([live("", { title: "hookless" })], []);
    expect(g.others).toHaveLength(1);
    expect(g.others[0].saved).toBeUndefined();
  });
});

describe("rowTitle", () => {
  it("prefers the user's label, then Claude's title, then the remembered one", () => {
    expect(rowTitle({ id: "a", live: live("a"), saved: saved("a", { label: "Mine" }), ended: false, favorite: true }, "?")).toBe("Mine");
    expect(rowTitle({ id: "a", live: live("a"), saved: saved("a"), ended: false, favorite: false }, "?")).toBe("live a");
    expect(rowTitle({ id: "a", saved: saved("a"), ended: true, favorite: true }, "?")).toBe("saved a");
    expect(rowTitle({ id: "a", ended: true, favorite: false }, "fallback")).toBe("fallback");
  });
});

describe("rowCwd", () => {
  it("falls back to the remembered directory once the session is gone", () => {
    expect(rowCwd({ id: "a", live: live("a"), saved: saved("a"), ended: false, favorite: false })).toBe("/live");
    expect(rowCwd({ id: "a", saved: saved("a"), ended: true, favorite: true })).toBe("/saved");
    expect(rowCwd({ id: "a", ended: true, favorite: false })).toBe("");
  });
});
