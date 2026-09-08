// Thin re-export of the generated Wails bindings so UI code has one import path.
export { PtyService, ConfigService, LogService, ThemeService, OpenerService, FileService, AgentService, StateService } from "../../bindings/github.com/Gerry3010/wate/internal/app";
export type { SpawnRequest, SpawnResult, ConfigResponse } from "../../bindings/github.com/Gerry3010/wate/internal/app";
export type { Config, Terminal as TerminalConfig, Editor as EditorConfig, Background as BackgroundConfig } from "../../bindings/github.com/Gerry3010/wate/internal/config";
export { Events } from "@wailsio/runtime";
export type { Resolved } from "../../bindings/github.com/Gerry3010/wate/internal/theme";
export type { Target } from "../../bindings/github.com/Gerry3010/wate/internal/opener";
export type { Document } from "../../bindings/github.com/Gerry3010/wate/internal/files";
export type { Session } from "../../bindings/github.com/Gerry3010/wate/internal/agent";
