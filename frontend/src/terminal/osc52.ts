/**
 * OSC 52 — a program asks the terminal to put text on the clipboard:
 *   ESC ] 52 ; <selection> ; <base64> BEL
 * Claude Code, tmux, vim and friends use it (it is the only way to copy out of a program
 * that owns the mouse, and the only one that survives an ssh hop).
 */

/** Anything bigger than this is refused: a stray escape should not ship a megabyte of junk. */
const MAX_BYTES = 1024 * 1024;

/**
 * Decodes an OSC 52 payload ("<selection>;<base64>") into the text to put on the clipboard.
 * Returns null for anything wate will not act on: a read request ("?"), a selection other than
 * the clipboard, a malformed payload or one over the size limit.
 */
export function parseOsc52(data: string): string | null {
  const semi = data.indexOf(";");
  if (semi < 0) return null;
  const selection = data.slice(0, semi);
  // "c" is the clipboard, "s" the selection the shell asked for; an empty field means the
  // default. The primary selection ("p") and the cut buffers ("0".."7") are separate on X11
  // and wate has only the one system clipboard, so those are left alone.
  if (selection !== "" && !/[cs]/.test(selection)) return null;
  const payload = data.slice(semi + 1);
  // "?" asks the terminal to hand the clipboard back to the program; wate never answers that.
  if (payload === "" || payload.startsWith("?")) return null;
  if (payload.length > MAX_BYTES) return null;
  let binary: string;
  try {
    binary = atob(payload);
  } catch {
    return null;
  }
  // atob yields one char per byte; the bytes are UTF-8, so decode them properly
  // (a naive atob would turn "Grüße" into mojibake).
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i) & 0xff;
  const text = new TextDecoder("utf-8").decode(bytes);
  return text || null;
}
