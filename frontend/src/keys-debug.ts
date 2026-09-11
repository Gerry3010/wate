/**
 * Key inspector for `wate ctl action __keys`: records what the WebView reports for the next
 * key presses and what xterm sends to the PTY for them. Remapped modifiers (xkb options,
 * keyd, swapped Ctrl/Alt keyboards) are otherwise guesswork.
 */
const MAX = 40;
const log: string[] = [];

export const keys = { recording: false };

/** Printable form of a byte sequence: "\x1b[1;5D" → "ESC [1;5D". */
export function escapeBytes(text: string): string {
  return Array.from(text)
    .map((c) => {
      const code = c.charCodeAt(0);
      if (code === 0x1b) return "ESC ";
      if (code === 0x7f) return "DEL";
      if (code < 0x20) return `^${String.fromCharCode(code + 64)}`;
      return c;
    })
    .join("");
}

export function logKey(e: KeyboardEvent) {
  if (!keys.recording) return;
  const mods = [e.ctrlKey && "ctrl", e.altKey && "alt", e.shiftKey && "shift", e.metaKey && "meta"].filter(Boolean).join("+");
  push(`key ${e.key} (${e.code})${mods ? " " + mods : " —"}`);
}

export function logData(text: string) {
  if (!keys.recording) return;
  push(`  sent ${escapeBytes(text)}`);
}

function push(line: string) {
  log.push(line);
  if (log.length > MAX) log.shift();
}

/** The recorded lines; clears the log. */
export function takeKeys(): string[] {
  const out = log.slice();
  log.length = 0;
  return out;
}
