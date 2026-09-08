import type { ILink, ILinkProvider, Terminal } from "@xterm/xterm";

import { OpenerService, type Target } from "../api";

/** Candidate file paths in a line of terminal text: ~/x, ./x, ../x, /x, a/b, name.ext, with optional :line:col. */
export const FILE_RE =
  /(?<![:\/\w])(?:~|\.{1,2})?\/[\w.@%+\-\/~]+(?::\d+(?::\d+)?)?|(?<![\w\/.@:-])[\w@%+\-]+(?:\/[\w.@%+\-]+)+(?::\d+(?::\d+)?)?|(?<![\w\/.@:-])[\w@%+\-]+\.[A-Za-z][\w]{0,7}(?::\d+(?::\d+)?)?/g;

const URL_RE = /^[a-z][a-z0-9+.-]*:\/\//i;

/** Find candidate paths in `text` with their 0-based column ranges. */
export function findPaths(text: string): { text: string; start: number; end: number }[] {
  const out: { text: string; start: number; end: number }[] = [];
  for (const m of text.matchAll(FILE_RE)) {
    let t = m[0];
    if (URL_RE.test(t) || t.length < 2) continue;
    // Trailing punctuation belongs to the sentence, not the path.
    const trimmed = t.replace(/[.,;:)\]}'"]+$/, "");
    if (!trimmed) continue;
    t = trimmed;
    out.push({ text: t, start: m.index!, end: m.index! + t.length });
  }
  return out;
}

export const isModifierClick = (e: MouseEvent) => e.ctrlKey || e.metaKey;

/**
 * xterm.js link provider that verifies candidates against the file system (via Go)
 * so only real files/dirs get underlined, and opens them on Ctrl/Cmd-click.
 */
export class FileLinkProvider implements ILinkProvider {
  constructor(
    private term: Terminal,
    private cwd: () => Promise<string>,
    private onOpen: (t: Target) => void,
  ) {}

  provideLinks(lineNo: number, cb: (links: ILink[] | undefined) => void): void {
    const line = this.term.buffer.active.getLine(lineNo - 1);
    if (!line) return cb(undefined);
    const text = line.translateToString(true);
    const cands = findPaths(text);
    if (cands.length === 0) return cb(undefined);
    void (async () => {
      let cwd = "";
      try {
        cwd = await this.cwd();
      } catch {
        return cb(undefined);
      }
      let targets: Target[];
      try {
        targets = (await OpenerService.Resolve(cwd, cands.map((c) => c.text))) ?? [];
      } catch {
        return cb(undefined);
      }
      const links: ILink[] = [];
      targets.forEach((t, i) => {
        if (!t || t.kind === "missing") return;
        const c = cands[i];
        links.push({
          text: c.text,
          range: { start: { x: c.start + 1, y: lineNo }, end: { x: c.end, y: lineNo } },
          decorations: { pointerCursor: true, underline: true },
          activate: (e) => {
            if (isModifierClick(e)) this.onOpen(t);
          },
        });
      });
      cb(links.length ? links : undefined);
    })();
  }
}
