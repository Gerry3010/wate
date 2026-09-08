// Thin re-export of the generated Wails bindings so UI code has one import path.
export { PtyService, ConfigService, LogService, ThemeService } from "../../bindings/github.com/Gerry3010/yate/internal/app";
export type { SpawnRequest, SpawnResult, ConfigResponse } from "../../bindings/github.com/Gerry3010/yate/internal/app";
export type { Config, Terminal as TerminalConfig, Editor as EditorConfig, Background as BackgroundConfig } from "../../bindings/github.com/Gerry3010/yate/internal/config";
export { Events } from "@wailsio/runtime";
export type { Resolved } from "../../bindings/github.com/Gerry3010/yate/internal/theme";
