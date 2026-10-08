import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

const here = new URL(".", import.meta.url).pathname;

/**
 * An endless CSS animation keeps WebKit compositing at the display's refresh rate for as
 * long as the rule matches — a whole CPU core while a Claude session runs, and about half
 * a kilobyte leaked per frame in the GTK dma-buf path. Stepped timing repaints only when
 * the value changes, so every endless animation here has to be stepped.
 */
describe("endless animations", () => {
  const files = readdirSync(here).filter((f) => f.endsWith(".css"));

  it("covers the stylesheets", () => {
    expect(files.length).toBeGreaterThan(0);
  });

  for (const file of files) {
    it(`${file} steps every infinite animation`, () => {
      const css = readFileSync(join(here, file), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
      for (const line of css.split("\n")) {
        if (!/animation:[^;]*\binfinite\b/.test(line)) continue;
        expect(line, `${file}: ${line.trim()}`).toMatch(/steps\(/);
      }
    });
  }
});
