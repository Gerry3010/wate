/** Chord parsing ("ctrl+shift+space") and matching against KeyboardEvents. */

export interface Chord {
  ctrl: boolean;
  shift: boolean;
  alt: boolean;
  meta: boolean;
  key: string;
}

const KEY_ALIASES: Record<string, string> = {
  " ": "space",
  spacebar: "space",
  arrowleft: "left",
  arrowright: "right",
  arrowup: "up",
  arrowdown: "down",
  "+": "plus",
  "=": "plus",
  "-": "minus",
  _: "minus",
  esc: "escape",
  return: "enter",
  pgup: "pageup",
  pgdn: "pagedown",
  del: "delete",
  ",": "comma",
  ".": "period",
};

export function normalizeKey(key: string): string {
  const k = key.toLowerCase();
  return KEY_ALIASES[k] ?? k;
}

export function parseChord(spec: string): Chord {
  const parts = spec.toLowerCase().split("+").map((p) => p.trim());
  // "ctrl+plus" is written with the word so the separator stays unambiguous.
  const chord: Chord = { ctrl: false, shift: false, alt: false, meta: false, key: "" };
  for (const p of parts) {
    switch (p) {
      case "ctrl":
      case "control":
        chord.ctrl = true;
        break;
      case "shift":
        chord.shift = true;
        break;
      case "alt":
      case "opt":
      case "option":
        chord.alt = true;
        break;
      case "cmd":
      case "meta":
      case "super":
      case "win":
        chord.meta = true;
        break;
      case "":
        break;
      default:
        chord.key = normalizeKey(p);
    }
  }
  return chord;
}

export function chordId(c: Chord): string {
  return `${c.ctrl ? "ctrl+" : ""}${c.alt ? "alt+" : ""}${c.shift ? "shift+" : ""}${c.meta ? "meta+" : ""}${c.key}`;
}

/** The subset of KeyboardEvent we care about (so tests don't need a DOM). */
export interface KeyLike {
  key: string;
  code?: string;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
  metaKey: boolean;
}

export function eventChord(e: KeyLike): Chord {
  let key = e.key;
  // With Shift/Alt the layout may report a symbol ("!" for shift+1, "→" on macOS alt);
  // fall back to the physical key for digits and letters so alt+1 / ctrl+shift+c stay stable.
  if (e.code && /^(Digit\d|Key[A-Z])$/.test(e.code) && (e.shiftKey || e.altKey)) {
    key = e.code.replace(/^Digit|^Key/, "");
  }
  return { ctrl: e.ctrlKey, shift: e.shiftKey, alt: e.altKey, meta: e.metaKey, key: normalizeKey(key) };
}

export type Bindings = Record<string, string | undefined> | null | undefined;

/** Resolves key events to action names. */
export class Keymap {
  private byChord = new Map<string, string>();
  private passthrough = new Set<string>();

  constructor(bindings: Bindings, passthrough: string[] = []) {
    for (const [action, spec] of Object.entries(bindings ?? {})) {
      if (!spec) continue;
      this.byChord.set(chordId(parseChord(spec)), action);
    }
    for (const spec of passthrough) this.passthrough.add(chordId(parseChord(spec)));
  }

  /** Action for the event, or null when unbound / passed through. */
  match(e: KeyLike): string | null {
    const id = chordId(eventChord(e));
    if (this.passthrough.has(id)) return null;
    return this.byChord.get(id) ?? null;
  }

  /** Human readable chord for an action (for tooltips), e.g. "Ctrl+Shift+Space". */
  label(action: string): string {
    for (const [id, a] of this.byChord) {
      if (a === action) return id.split("+").map((p) => p[0].toUpperCase() + p.slice(1)).join("+");
    }
    return "";
  }
}
