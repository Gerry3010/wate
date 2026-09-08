import type { BackgroundConfig, Resolved } from "../api";

/** Push theme colours into CSS variables and the background layer. */
export function applyTheme(theme: Resolved, bg: BackgroundConfig) {
  const root = document.documentElement;
  for (const [k, v] of Object.entries(theme.css_vars ?? {})) root.style.setProperty(`--${k}`, v ?? "");
  root.dataset.theme = theme.id;
  root.dataset.bgMode = bg.mode;

  const [r, g, b] = hexToRgb(theme.theme.colors.background);
  const opaque = bg.mode === "solid";
  const alpha = opaque ? 1 : bg.opacity;
  root.style.setProperty("--bg-rgb", `${r}, ${g}, ${b}`);
  root.style.setProperty("--surface", `rgba(${r}, ${g}, ${b}, ${alpha})`);
  root.style.setProperty("--surface-alpha", String(alpha));
  root.style.setProperty("--wallpaper-blur", `${bg.blur}px`);
  root.style.setProperty("--wallpaper-dim", String(bg.dim));

  const layer = document.getElementById("bg")!;
  if (bg.mode === "wallpaper") {
    layer.style.backgroundImage = `url(/wallpaper?v=${Date.now()})`;
    layer.hidden = false;
  } else {
    layer.style.backgroundImage = "";
    layer.hidden = true;
  }
}

export function hexToRgb(hex: string): [number, number, number] {
  const h = hex.replace("#", "");
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)];
}

/** xterm.js theme: transparent background so the surface/wallpaper shows through. */
export function xtermTheme(theme: Resolved, bg: BackgroundConfig) {
  const t: Record<string, string> = { ...(theme.xterm as Record<string, string>) };
  if (bg.mode !== "solid") t.background = "#00000000";
  t.scrollbarSliderBackground = "rgba(255,255,255,0.12)";
  t.scrollbarSliderHoverBackground = "rgba(255,255,255,0.22)";
  t.scrollbarSliderActiveBackground = "rgba(255,255,255,0.3)";
  return t;
}
