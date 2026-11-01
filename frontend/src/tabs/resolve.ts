/**
 * The tab a request names: by id, else by title (case and surrounding space ignored). -1 when
 * none does. The root agent names tabs the way the user does, by what is written on them.
 */
export function findTab(tabs: { id: string; title: string }[], spec: string): number {
  const want = spec.trim();
  if (!want) return -1;
  const byId = tabs.findIndex((t) => t.id === want);
  if (byId >= 0) return byId;
  const lower = want.toLowerCase();
  return tabs.findIndex((t) => t.title.trim().toLowerCase() === lower);
}
