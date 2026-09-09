import { ConfigService, Events, LogService } from "./api";
import { WateApp } from "./app";

// Mirror console errors/warnings into the Go log so packaged builds are debuggable.
for (const level of ["error", "warn"] as const) {
  const orig = console[level].bind(console);
  console[level] = (...args: unknown[]) => {
    orig(...args);
    const fmt = (a: unknown) => (a instanceof Error ? a.stack ?? a.message : typeof a === "object" && a !== null ? JSON.stringify(a) : String(a));
    LogService.Log(level, args.map(fmt).join(" ")).catch(() => {});
  };
}
window.addEventListener("error", (e) => console.error(e.message, e.filename, e.lineno));
window.addEventListener("unhandledrejection", (e) => console.error("unhandled rejection:", e.reason));

async function boot() {
  const root = document.getElementById("app")!;
  const { config, warning, path, initial_cwd, secondary } = await ConfigService.Get();
  if (warning) console.warn(warning);

  const app = new WateApp(root, config);
  app.secondary = !!secondary;
  app.configPath = path;
  await app.loadTheme();
  Events.On("config:changed", (ev: { data: { config: typeof config; warning?: string } }) => {
    if (ev.data.warning) console.warn(ev.data.warning);
    void app.applyConfig(ev.data.config);
  });
  Events.On("ctl:open", (ev: { data: { path: string; line: number; col: number; tab: string } }) => {
    const tab = app.tabs.find((t) => t.id === ev.data.tab) ?? app.active;
    if (tab) {
      app.activate(tab);
      void app.openEditor(tab, ev.data.path, ev.data.line || undefined, ev.data.col || undefined);
    }
  });
  Events.On("ctl:action", (ev: { data: { name: string } }) => void app.run(ev.data.name));
  Events.On("ctl:new-tab", (ev: { data: { path: string } }) => {
    void app.newTab({ cwd: ev.data.path });
  });
  Events.On("agent:status", (ev: { data: Parameters<typeof app.onAgentStatus>[0] }) => app.onAgentStatus(ev.data));
  const restored = await app.restoreSession();
  if (initial_cwd || !restored) await app.newTab({ cwd: initial_cwd || undefined });
}

boot().catch((err) => {
  console.error(err);
  document.getElementById("app")!.textContent = String(err);
});
