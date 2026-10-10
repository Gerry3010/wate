import { ConfigService, Events, LogService, WindowService } from "./api";
import { initWindowId, onMine } from "./api/events";
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
  // Before any listener: a targeted event that arrives while this is unknown would be dropped.
  await initWindowId();
  const root = document.getElementById("app")!;
  const { config, warning, path, os } = await ConfigService.Get();
  // Per-window facts come from WindowService, not ConfigService: the config payload is also
  // broadcast as config:changed, where anything window-specific would be wrong somewhere.
  const boot = await WindowService.Bootstrap();
  if (warning) console.warn(warning);
  // Lets the stylesheet dodge the macOS traffic lights (see .tabbar in base.css).
  document.documentElement.dataset.platform = os;

  const app = new WateApp(root, config);
  app.windowId = boot.window_id;
  app.restoreMode = boot.restore;
  app.stateKey = boot.state_key;
  app.persist = boot.persist;
  app.configPath = path;
  await app.loadTheme();
  Events.On("config:changed", (ev: { data: { config: typeof config; warning?: string } }) => {
    if (ev.data.warning) console.warn(ev.data.warning);
    void app.applyConfig(ev.data.config);
  });
  onMine("ctl:open", (d: { path: string; line: number; col: number; tab: string }) => {
    const tab = app.tabs.find((t) => t.id === d.tab) ?? app.active;
    if (tab) {
      app.activate(tab);
      void app.openEditor(tab, d.path, d.line || undefined, d.col || undefined);
    }
  });
  onMine("ctl:action", (d: { name: string; pane: string }) => void app.run(d.name, d.pane || undefined));
  onMine("pane:request", (d: Parameters<typeof app.handlePaneRequest>[0]) => void app.handlePaneRequest(d));
  onMine("window:drop", (d: { paths: string[] | null; x: number; y: number }) => app.onFilesDropped(d));
  onMine("window:fullscreen", (d: { fullscreen: boolean }) => {
    document.documentElement.toggleAttribute("data-fullscreen", d.fullscreen);
  });
  onMine("ctl:new-tab", (d: { path: string }) => {
    void app.newTab({ cwd: d.path });
  });
  Events.On("restart:changed", (ev: { data: Parameters<typeof app.onRestartChanged>[0] }) => app.onRestartChanged(ev.data));
  Events.On("access:changed", (ev: { data: Parameters<typeof app.onAccessChanged>[0] }) => app.onAccessChanged(ev.data));
  Events.On("agent:status", (ev: { data: Parameters<typeof app.onAgentStatus>[0] }) => app.onAgentStatus(ev.data));
  onMine("window:activate-tab", (d: { tab: string; pane: string }) => app.activateTab(d.tab, d.pane));
  onMine("window:adopt-tab", (d: Parameters<typeof app.adoptTab>[0]) => void app.adoptTab(d));
  onMine("window:release-tab", (d: { tab: string }) => app.releaseTab(d.tab));
  if (boot.restore === "adopt") {
    // Opened to receive a tab dragged out of another window.
    const pending = await WindowService.PendingTab();
    if (pending) {
      await app.adoptTab(pending as unknown as Parameters<typeof app.adoptTab>[0]);
      return;
    }
  }
  const restored = boot.restore === "session" ? await app.restoreSession() : false;
  if (boot.initial_cwd || !restored) await app.newTab({ cwd: boot.initial_cwd || undefined });
}

boot().catch((err) => {
  console.error(err);
  document.getElementById("app")!.textContent = String(err);
});
