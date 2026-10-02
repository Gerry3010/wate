/**
 * Index arithmetic for reordering the tab bar.
 *
 * The drag itself lives in tabbar.ts, where it cannot be tested — vitest runs without a DOM.
 * Everything that can be decided from numbers alone is here instead.
 */

/**
 * `arr` with the item at `from` moved so it ends up at index `to` in the result.
 * Out-of-range indices and a no-op move return the input unchanged (same reference), which
 * lets callers skip the repaint.
 */
export function moveItem<T>(arr: readonly T[], from: number, to: number): readonly T[] {
  if (from < 0 || from >= arr.length) return arr;
  const target = Math.min(Math.max(to, 0), arr.length - 1);
  if (target === from) return arr;
  const out = arr.slice();
  const [item] = out.splice(from, 1);
  out.splice(target, 0, item);
  return out;
}

/**
 * Where a tab dragged to `x` belongs, as a gap index in `0..centers.length`: the number of
 * tab centres left of the pointer. `centers` is in bar order, measured once per drag.
 */
export function insertionIndex(centers: readonly number[], x: number): number {
  let i = 0;
  while (i < centers.length && centers[i] < x) i++;
  return i;
}

/**
 * The index the dragged tab would land on, given a gap index from insertionIndex(). Removing
 * the tab first shifts every gap after it one to the left, which is the off-by-one this
 * function exists to hide.
 */
export function dropIndex(from: number, gap: number): number {
  return gap > from ? gap - 1 : gap;
}
