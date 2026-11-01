import { describe, expect, it } from "vitest";
import { findTab } from "./resolve";

const tabs = [
  { id: "tab-1", title: "ÖPG" },
  { id: "tab-2", title: "Jacky & GH.net" },
  { id: "tab-3", title: "Sleep, Chattr" },
];

describe("findTab", () => {
  it("finds a tab by id", () => expect(findTab(tabs, "tab-2")).toBe(1));
  it("finds a tab by title, ignoring case and space", () => {
    expect(findTab(tabs, "  sleep, chattr ")).toBe(2);
    expect(findTab(tabs, "öpg")).toBe(0);
  });
  it("says so when nothing matches", () => {
    expect(findTab(tabs, "Minecraft")).toBe(-1);
    expect(findTab(tabs, "")).toBe(-1);
  });
});
