/** What a pane runs right now, as reported by the backend (`pty:command`). */
export interface PaneCommand {
  pane: string;
  /** Foreground command's name; empty while the shell sits at its prompt. */
  name: string;
  /** ssh destination, when name is "ssh". */
  host: string;
  cwd: string;
}

/**
 * The path the way a shell prompt writes it: home as "~", and — once it grows too long —
 * the leading directories shortened one by one, the way zsh and p10k truncate them, so the
 * current directory stays readable. "~/Sync-Projekte/GO-Projekte/wate" → "~/Sy/GO-Projekte/wate".
 */
export function promptPath(cwd: string, max = 30): string {
  if (!cwd) return "";
  const home = /^\/(?:home|Users)\/[^/]+/.exec(cwd)?.[0];
  const full = (home ? "~" + cwd.slice(home.length) : cwd).replace(/^(.+)\/$/, "$1");
  if (full.length <= max) return full;
  const parts = full.split("/");
  // Shrink the parents from the left; the root ("~", "") and the last directory stay whole.
  for (let i = 1; i < parts.length - 1 && parts.join("/").length > max; i++) {
    parts[i] = abbreviate(parts[i]);
  }
  const short = parts.join("/");
  if (short.length <= max) return short;
  return `${parts[0]}/…/${parts[parts.length - 1]}`;
}

/** Two characters of a directory name, dotfile-style names keeping their dot. */
function abbreviate(name: string): string {
  return name.startsWith(".") ? name.slice(0, 3) : name.slice(0, 2);
}

/** Tab/pane title: the ssh host while a session is up, otherwise the prompt's path. */
export function paneTitle(cmd: PaneCommand | undefined, cwd: string, fallback = ""): string {
  if (cmd?.name === "ssh" && cmd.host) return `ssh ${cmd.host}`;
  const path = promptPath(cwd || cmd?.cwd || "");
  return path || fallback;
}
