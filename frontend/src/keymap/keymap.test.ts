import { describe, expect, it } from "vitest";
import { Keymap, chordId, parseChord } from "./keymap";

const ev = (key: string, mods: Partial<{ ctrl: boolean; shift: boolean; alt: boolean; meta: boolean }> = {}, code?: string) => ({
  key,
  code,
  ctrlKey: !!mods.ctrl,
  shiftKey: !!mods.shift,
  altKey: !!mods.alt,
  metaKey: !!mods.meta,
});

describe("parseChord", () => {
  it("parses modifiers and aliases", () => {
    expect(chordId(parseChord("Ctrl+Shift+Space"))).toBe("ctrl+shift+space");
    expect(chordId(parseChord("cmd+c"))).toBe("meta+c");
    expect(chordId(parseChord("shift+left"))).toBe("shift+left");
    expect(chordId(parseChord("ctrl+plus"))).toBe("ctrl+plus");
    expect(chordId(parseChord("alt+1"))).toBe("alt+1");
  });
});

describe("Keymap.match", () => {
  const km = new Keymap(
    {
      split_right: "ctrl+space",
      split_down: "ctrl+shift+space",
      focus_left: "shift+left",
      tab_1: "alt+1",
      copy: "ctrl+shift+c",
      font_bigger: "ctrl+plus",
      next_tab: "ctrl+tab",
    },
    ["ctrl+space"],
  );

  it("matches bound chords", () => {
    expect(km.match(ev(" ", { ctrl: true, shift: true }))).toBe("split_down");
    expect(km.match(ev("ArrowLeft", { shift: true }))).toBe("focus_left");
    expect(km.match(ev("Tab", { ctrl: true }))).toBe("next_tab");
  });

  it("uses the physical key when shift/alt change e.key", () => {
    expect(km.match(ev("!", { alt: true }, "Digit1"))).toBe("tab_1");
    expect(km.match(ev("C", { ctrl: true, shift: true }, "KeyC"))).toBe("copy");
    expect(km.match(ev("+", { ctrl: true, shift: true }, "Equal"))).toBeNull();
    expect(km.match(ev("+", { ctrl: true }, "Equal"))).toBe("font_bigger");
    expect(km.match(ev("=", { ctrl: true }, "Equal"))).toBe("font_bigger");
  });

  it("honours passthrough and ignores unbound keys", () => {
    expect(km.match(ev(" ", { ctrl: true }))).toBeNull();
    expect(km.match(ev("a"))).toBeNull();
  });

  it("labels actions", () => {
    expect(km.label("split_down")).toBe("Ctrl+Shift+Space");
    expect(km.label("nope")).toBe("");
  });
});
