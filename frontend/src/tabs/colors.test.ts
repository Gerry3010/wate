import { describe, expect, it } from "vitest";
import { customColor, tabColorValue } from "./tabbar";

describe("tab colours", () => {
  it("knows a colour of the user's own from a named one", () => {
    expect(customColor("#B006E3")).toBe("#b006e3");
    expect(customColor("blue")).toBe("");
    expect(customColor("")).toBe("");
    expect(customColor("#abc")).toBe("");
  });
  it("draws a custom colour as it is and a named one from the list", () => {
    expect(tabColorValue("#12ab34")).toBe("#12ab34");
    expect(tabColorValue("blue")).toBe("#89b4fa");
    expect(tabColorValue("mauve")).toBe("");
  });
});
