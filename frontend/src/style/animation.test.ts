import { describe, expect, it } from "vitest";

/**
 * An endless CSS animation keeps WebKit compositing at the display's refresh rate for as
 * long as the rule matches — a whole CPU core while a Claude session runs, and about half
 * a kilobyte leaked per frame in the GTK dma-buf path. Stepped timing repaints only when
 * the value changes, so every endless animation here has to be stepped.
 */
const sheets = import.meta.glob("./*.css", { query: "?inline", import: "default", eager: true }) as Record<
  string,
  string
>;

describe("endless animations", () => {
  it("covers the stylesheets", () => {
    expect(Object.keys(sheets).length).toBeGreaterThan(0);
  });

  for (const [path, css] of Object.entries(sheets)) {
    it(`${path} steps every infinite animation`, () => {
      for (const line of css.replace(/\/\*[\s\S]*?\*\//g, "").split("\n")) {
        if (!/animation:[^;]*\binfinite\b/.test(line)) continue;
        expect(line, `${path}: ${line.trim()}`).toMatch(/steps\(/);
      }
    });
  }
});
