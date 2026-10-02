/**
 * Splitting the sidebar into favourites and the rest.
 *
 * Two sources have to be reconciled: the live sessions the backend polls (keyed by pane, and
 * gone the moment Claude exits) and what wate remembers about them (keyed by session id, and
 * kept on disk). A favourite therefore outlives its session — that is the whole point of the
 * section — so a row can have a live session, a saved entry, or both.
 */

import type { SavedAgentSession, Session } from "../api";

export interface Row {
  /** Join key. Sessions without one (no hooks installed) can still be listed, never saved. */
  id: string;
  live?: Session;
  saved?: SavedAgentSession;
  /** No live session behind it: it can be resumed, not talked to. */
  ended: boolean;
  favorite: boolean;
}

export interface Groups {
  favorites: Row[];
  others: Row[];
}

/** What to show as the row's name: the user's own label wins over Claude's summary. */
export function rowTitle(r: Row, fallback: string): string {
  return r.saved?.label || r.live?.title || r.saved?.title || fallback;
}

/** The directory a row belongs to, live if we have it and remembered otherwise. */
export function rowCwd(r: Row): string {
  return r.live?.cwd || r.saved?.cwd || "";
}

/**
 * Group live sessions and saved entries into the two sections. Live sessions keep the order
 * they arrive in; ended favourites follow the live ones, most recently seen first.
 */
export function groupSessions(live: readonly Session[], saved: readonly SavedAgentSession[]): Groups {
  const byId = new Map<string, SavedAgentSession>();
  for (const e of saved) if (e.session_id) byId.set(e.session_id, e);

  const favorites: Row[] = [];
  const others: Row[] = [];
  const seen = new Set<string>();

  for (const s of live) {
    const entry = s.session_id ? byId.get(s.session_id) : undefined;
    if (s.session_id) seen.add(s.session_id);
    const row: Row = { id: s.session_id, live: s, saved: entry, ended: false, favorite: !!entry?.favorite };
    (row.favorite ? favorites : others).push(row);
  }

  // A favourite whose session has ended stays listed so it can be resumed. A merely renamed
  // one does not — the label is there to come back to if the session does.
  const ended = saved
    .filter((e) => e.favorite && e.session_id && !seen.has(e.session_id))
    .sort((a, b) => (a.seen_at < b.seen_at ? 1 : a.seen_at > b.seen_at ? -1 : 0))
    .map((e): Row => ({ id: e.session_id, saved: e, ended: true, favorite: true }));

  return { favorites: [...favorites, ...ended], others };
}
