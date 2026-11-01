import { describe, expect, it } from "vitest";
import { rootLayout } from "./panel";

describe("rootLayout", () => {
  it("keeps a known layout and falls back to the drawer", () => {
    expect(rootLayout("top")).toBe("top");
    expect(rootLayout("overlay")).toBe("overlay");
    expect(rootLayout("sideways")).toBe("drawer");
    expect(rootLayout(undefined)).toBe("drawer");
  });
});
