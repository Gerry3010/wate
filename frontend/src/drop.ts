/**
 * Files dropped onto the window.
 *
 * Without a handler the WebView does what a browser does: it navigates the document to
 * `file:///…`. The dropped image then fills the window, every pane is gone and there is no
 * back button — the app has to be killed. So wate takes every drop itself and turns it into
 * what a terminal is expected to do with a file: put its path into the prompt.
 */

/** Paths carried by a drop: the uri-list (`file://` only), or plain text that looks like a path. */
export function droppedPaths(dt: DataTransfer | null): string[] {
  if (!dt) return [];
  const list = dt.getData("text/uri-list") || dt.getData("text/plain") || "";
  const paths: string[] = [];
  for (const raw of list.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue; // uri-list comments
    const m = /^file:\/\/[^/]*(\/.*)$/.exec(line);
    if (m) {
      try {
        paths.push(decodeURIComponent(m[1]));
      } catch {
        paths.push(m[1]); // a stray % in the name: better the raw path than nothing
      }
    } else if (line.startsWith("/") || line.startsWith("~/")) {
      paths.push(line);
    }
  }
  return paths;
}

/** Shell-quoted, so a name with spaces or quotes survives the paste: `'it'\''s here.png'`. */
export function quotePath(path: string): string {
  return /^[A-Za-z0-9_@%+=:,./-]+$/.test(path) ? path : `'${path.replace(/'/g, `'\\''`)}'`;
}

/** What the drop types into the shell: the quoted paths, with a trailing space to type on. */
export function dropText(paths: string[]): string {
  return paths.length === 0 ? "" : paths.map(quotePath).join(" ") + " ";
}
