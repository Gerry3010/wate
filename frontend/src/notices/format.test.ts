import { describe, expect, it } from "vitest";
import { ago, badgeText } from "./format";

describe("ago", () => {
  const now = new Date("2026-11-25T23:00:00+01:00");
  it("says just now for the last few seconds", () => {
    expect(ago("2026-11-25T22:59:30+01:00", now)).toBe("just now");
  });
  it("counts minutes, then hours", () => {
    expect(ago("2026-11-25T22:52:00+01:00", now)).toBe("8 min ago");
    expect(ago("2026-11-25T20:00:00+01:00", now)).toBe("3 h ago");
  });
  it("never says something happened in the future", () => {
    expect(ago("2026-11-25T23:05:00+01:00", now)).toBe("just now");
  });
});

describe("badgeText", () => {
  it("shows nothing when everything is read", () => {
    expect(badgeText(0)).toBe("");
  });
  it("keeps to three characters", () => {
    expect(badgeText(7)).toBe("7");
    expect(badgeText(99)).toBe("99");
    expect(badgeText(250)).toBe("99+");
  });
});
