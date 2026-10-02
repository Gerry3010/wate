import { describe, expect, it } from "vitest";
import { addressedToMe } from "./events";

describe("addressedToMe", () => {
  it("takes events addressed to this window", () => {
    expect(addressedToMe("w2", "w2")).toBe(true);
  });

  it("ignores events addressed to another window", () => {
    expect(addressedToMe("w1", "w2")).toBe(false);
  });

  it("lets broadcasts through — an emit from Go carries no sender", () => {
    expect(addressedToMe(undefined, "w2")).toBe(true);
    expect(addressedToMe("", "w2")).toBe(true);
  });

  it("still delivers while this window's own name is unknown", () => {
    // initWindowId() failing must not silence the app.
    expect(addressedToMe(undefined, "")).toBe(true);
    expect(addressedToMe("w1", "")).toBe(false);
  });
});
