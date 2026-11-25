/** Pure helpers for the notification bell, kept apart from the DOM so they can be tested. */

/** How long ago something happened, as short as the bell's rows need it. */
export function ago(at: string | Date, now: Date = new Date()): string {
  const t = typeof at === "string" ? new Date(at) : at;
  const secs = Math.max(0, Math.round((now.getTime() - t.getTime()) / 1000));
  if (!Number.isFinite(secs)) return "";
  if (secs < 45) return "just now";
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins} min ago`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours} h ago`;
  return t.toLocaleDateString([], { day: "numeric", month: "short" });
}

/** What the badge on the bell says: nothing at zero, and no more than three characters. */
export function badgeText(unread: number): string {
  if (unread <= 0) return "";
  return unread > 99 ? "99+" : String(unread);
}

/** The few words a notice's kind is shown with. */
export const KIND_LABEL: Record<string, string> = {
  waiting: "waiting",
  finished: "finished",
  root: "root agent",
  agent: "from a session",
  wate: "wate",
};

/** How many banners stack up at most; an older one makes room for a newer. */
export const MAX_BANNERS = 3;

/** How long a banner stays up while the pointer is not on it. */
export const BANNER_MS = 6000;
