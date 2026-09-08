import { ConfigService, Events, LogService } from "./api";
import { YateApp } from "./app";

// Mirror console errors/warnings into the Go log so packaged builds are debuggable.
for (const level of ["error", "warn"] as const) {
  const orig = console[level].bind(console);
  console[level] = (...args: unknown[]) => {
    orig(...args);
    LogService.Log(level, args.map((a) => (a instanceof Error ? a.stack ?? a.message : String(a))).join(" ")).catch(() => {});
  };
}
window.addEventListener("error", (e) => console.error(e.message, e.filename, e.lineno));
window.addEventListener("unhandledrejection", (e) => console.error("unhandled rejection:", e.reason));

async function boot() {
  const root = document.getElementById("app")!;
  const { config, warning } = await ConfigService.Get();
  if (warning) console.warn(warning);

  const app = new YateApp(root, config);
  Events.On("config:changed", (ev: { data: { config: typeof config } }) => app.applyConfig(ev.data.config));
  await app.newTab();
}

boot().catch((err) => {
  console.error(err);
  document.getElementById("app")!.textContent = String(err);
});
