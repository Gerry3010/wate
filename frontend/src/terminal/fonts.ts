/**
 * JetBrains Mono, shipped with the app instead of looked up on the system.
 *
 * Since macOS 27, WebKit's canvas draws a font installed in ~/Library/Fonts unreliably: the
 * WebGL renderer rasterises each glyph into its atlas on its own, and some of them (ä, ö, ü —
 * which ones changes from launch to launch) come out shifted up or a size too large. The DOM
 * renderer, system fonts (Menlo) and fonts loaded through @font-face are all unaffected.
 *
 * These faces are declared under the family name "JetBrains Mono", so they shadow the installed
 * copy and the configured font list needs no change. Fonts that are not bundled — a Nerd Font,
 * say — still go through the system lookup.
 */

import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/700.css";
import "@fontsource/jetbrains-mono/400-italic.css";
import "@fontsource/jetbrains-mono/700-italic.css";

const FAMILY = "JetBrains Mono";

/**
 * Load every bundled face before the first terminal opens. The faces are split by
 * unicode-range and the browser only fetches one when a page uses it — but xterm's glyph atlas
 * keeps whatever it drew first, so a glyph rasterised before its face arrived would stay in
 * the fallback font for good.
 */
export async function loadBundledFonts(): Promise<void> {
  await document.fonts.ready;
  const faces = [...document.fonts].filter((f) => f.family.replace(/["']/g, "") === FAMILY);
  const results = await Promise.allSettled(faces.map((f) => f.load()));
  const failed = results.filter((r) => r.status === "rejected").length;
  if (failed) console.warn(`fonts: ${failed} of ${faces.length} ${FAMILY} faces failed to load`);
}
