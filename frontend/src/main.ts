import { ConfigService, LogService } from "./api";

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
import { TerminalPane } from "./terminal/pane";

async function boot() {
  const root = document.getElementById("app")!;
  const { config, warning } = await ConfigService.Get();
  if (warning) console.warn(warning);

  const pane = new TerminalPane({
    paneId: crypto.randomUUID(),
    tabId: crypto.randomUUID(),
    terminal: config.terminal,
    onExit: () => pane.dispose(),
  });
  root.appendChild(pane.element);
  await pane.start();
  pane.focus();
}

boot().catch((err) => {
  console.error(err);
  document.getElementById("app")!.textContent = String(err);
});
