/** Anything that can live in a layout leaf: a terminal or an editor. */
export interface Pane {
  readonly id: string;
  readonly kind: "terminal" | "editor" | "settings";
  readonly element: HTMLElement;
  /** Display title (shell title, file name). */
  title: string;
  focus(): void;
  /** Whether this is the tab's focused pane (cursor blink, highlight). */
  setActive?(active: boolean): void;
  /** Working directory to inherit for new panes (best effort). */
  cwd(): Promise<string>;
  /** Called when the pane's box changed size. */
  relayout(): void;
  /** While true, size changes are ignored until relayout() (divider drags). */
  setFitSuspended?(suspended: boolean): void;
  dispose(): void;
}
