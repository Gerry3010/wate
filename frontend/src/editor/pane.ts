import { closeBrackets, closeBracketsKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { bracketMatching, indentUnit, LanguageDescription } from "@codemirror/language";
import { languages } from "@codemirror/language-data";
import { highlightSelectionMatches, openSearchPanel, searchKeymap } from "@codemirror/search";
import { Compartment, EditorState } from "@codemirror/state";
import { drawSelection, EditorView, highlightActiveLine, highlightActiveLineGutter, keymap, lineNumbers } from "@codemirror/view";
import { Dialogs } from "@wailsio/runtime";

import { FileService, type Document, type EditorConfig } from "../api";
import type { Pane } from "../pane";
import { highlightCode, renderMarkdown } from "../preview/markdown";
import { wateEditorTheme, wateHighlighting } from "./cmtheme";

export type EditorMode = "code" | "split" | "preview";
const MODES: EditorMode[] = ["code", "split", "preview"];

export interface EditorPaneOptions {
  paneId: string;
  path: string;
  line?: number;
  col?: number;
  editor: EditorConfig;
  onTitle?: () => void;
  onClose?: () => void;
  /** Link clicked inside the preview. */
  onLink?: (href: string, fromDir: string) => void;
}

const isMarkdown = (p: string) => /\.(md|markdown|mdx|mkd)$/i.test(p);

/** Text editor pane (CodeMirror 6) with Code / Split / Preview modes for Markdown. */
export class EditorPane implements Pane {
  readonly id: string;
  readonly kind = "editor" as const;
  readonly element = document.createElement("div");
  title = "";
  readonly path: string;

  private view!: EditorView;
  private doc?: Document;
  private modified = false;
  private mode: EditorMode = "code";
  private previewEl = document.createElement("div");
  private codeEl = document.createElement("div");
  private toolbar = document.createElement("div");
  private status = document.createElement("span");
  private previewTimer?: ReturnType<typeof setTimeout>;
  private language = new Compartment();
  private wrap = new Compartment();
  private modeButtons = new Map<EditorMode, HTMLButtonElement>();

  constructor(private opts: EditorPaneOptions) {
    this.id = opts.paneId;
    this.path = opts.path;
    this.title = basename(opts.path);
    this.element.className = "pane pane-editor";
    this.element.dataset.paneId = opts.paneId;
    this.element.tabIndex = -1;
    this.buildToolbar();
    const body = document.createElement("div");
    body.className = "editor-body";
    this.codeEl.className = "editor-code";
    this.previewEl.className = "editor-preview markdown-body";
    body.append(this.codeEl, this.previewEl);
    this.element.append(this.toolbar, body);
    this.previewEl.addEventListener("click", (e) => this.onPreviewClick(e));
    this.setMode(isMarkdown(opts.path) ? opts.editor.markdown_default_mode as EditorMode : "code");
  }

  private buildToolbar() {
    this.toolbar.className = "editor-toolbar";
    const name = document.createElement("span");
    name.className = "editor-name";
    name.title = this.path;
    const dir = document.createElement("span");
    dir.className = "editor-dir";
    // <bdi> keeps the text left-to-right while the rtl container ellipsizes the *start*.
    const bdi = document.createElement("bdi");
    bdi.textContent = dirname(this.path) + "/";
    dir.appendChild(bdi);
    const base = document.createElement("span");
    base.className = "editor-basename";
    base.textContent = basename(this.path);
    name.append(dir, base);
    const modes = document.createElement("div");
    modes.className = "editor-modes";
    for (const m of MODES) {
      const b = document.createElement("button");
      b.textContent = m[0].toUpperCase() + m.slice(1);
      b.addEventListener("click", () => this.setMode(m));
      this.modeButtons.set(m, b);
      modes.appendChild(b);
    }
    const save = document.createElement("button");
    save.className = "editor-save";
    save.textContent = "Save";
    save.addEventListener("click", () => void this.save());
    const close = document.createElement("button");
    close.className = "editor-close";
    close.textContent = "×";
    close.title = "Close";
    close.addEventListener("click", () => this.opts.onClose?.());
    this.status.className = "editor-status";
    this.toolbar.append(name, this.status, modes, save, close);
  }

  async load(): Promise<void> {
    this.doc = await FileService.Read(this.path);
    const lang = await LanguageDescription.matchFilename(languages, basename(this.path))?.load();
    const e = this.opts.editor;
    const state = EditorState.create({
      doc: this.doc.content,
      extensions: [
        lineNumbers(),
        highlightActiveLineGutter(),
        history(),
        drawSelection(),
        highlightActiveLine(),
        bracketMatching(),
        closeBrackets(),
        highlightSelectionMatches(),
        indentUnit.of(" ".repeat(e.tab_width || 4)),
        EditorState.tabSize.of(e.tab_width || 4),
        keymap.of([...closeBracketsKeymap, ...defaultKeymap, ...historyKeymap, ...searchKeymap, indentWithTab]),
        this.language.of(lang ? [lang] : []),
        this.wrap.of(e.word_wrap ? EditorView.lineWrapping : []),
        wateEditorTheme,
        wateHighlighting,
        EditorView.updateListener.of((u) => {
          if (u.docChanged) {
            this.setModified(true);
            this.schedulePreview();
          }
        }),
      ],
    });
    this.view = new EditorView({ state, parent: this.codeEl });
    if (!e.line_numbers) this.codeEl.classList.add("no-gutter");
    if (this.opts.line) this.gotoLine(this.opts.line, this.opts.col);
    this.renderPreview();
    this.setModified(false);
  }

  gotoLine(line: number, col = 0) {
    const l = this.view.state.doc.line(Math.max(1, Math.min(line, this.view.state.doc.lines)));
    const pos = Math.min(l.from + Math.max(0, col - 1), l.to);
    this.view.dispatch({ selection: { anchor: pos }, effects: EditorView.scrollIntoView(pos, { y: "center" }) });
  }

  setMode(mode: EditorMode) {
    if (!isMarkdown(this.path)) mode = "code";
    this.mode = mode;
    this.element.dataset.mode = mode;
    for (const [m, b] of this.modeButtons) {
      b.classList.toggle("active", m === mode);
      b.disabled = !isMarkdown(this.path) && m !== "code";
    }
    if (mode !== "code") this.renderPreview();
    if (mode !== "preview") this.view?.requestMeasure();
    // Keep keyboard focus inside the pane when the mode changes under it.
    if (this.element.contains(document.activeElement) || this.element === document.activeElement) this.focus();
  }

  cycleMode() {
    if (!isMarkdown(this.path)) return;
    this.setMode(MODES[(MODES.indexOf(this.mode) + 1) % MODES.length]);
  }

  private schedulePreview() {
    if (this.mode === "code") return;
    clearTimeout(this.previewTimer);
    this.previewTimer = setTimeout(() => this.renderPreview(), 300);
  }

  private renderPreview() {
    if (!this.view || this.mode === "code" || !isMarkdown(this.path)) return;
    const scroll = this.previewEl.scrollTop;
    this.previewEl.innerHTML = renderMarkdown(this.view.state.doc.toString());
    highlightCode(this.previewEl);
    this.previewEl.scrollTop = scroll;
  }

  private onPreviewClick(e: MouseEvent) {
    const a = (e.target as HTMLElement).closest("a");
    if (!a) return;
    e.preventDefault();
    const href = a.getAttribute("href");
    if (href) this.opts.onLink?.(href, dirname(this.path));
  }

  private setModified(m: boolean) {
    this.modified = m;
    this.title = (m ? "● " : "") + basename(this.path);
    this.status.textContent = m ? "modified" : "";
    this.element.classList.toggle("modified", m);
    this.opts.onTitle?.();
  }

  get isModified() {
    return this.modified;
  }

  async save(force = false): Promise<boolean> {
    if (!this.view) return false;
    const content = this.view.state.doc.toString();
    try {
      this.doc = await FileService.Write(this.path, content, force ? "" : this.doc?.hash ?? "");
      this.setModified(false);
      this.flash("saved");
      return true;
    } catch (err) {
      if (String(err).includes("changed")) {
        const answer = await Dialogs.Question({
          Title: "File changed on disk",
          Message: `${basename(this.path)} was modified by another program. Overwrite it?`,
          Buttons: [{ Label: "Overwrite", IsDefault: false }, { Label: "Cancel", IsCancel: true, IsDefault: true }],
        });
        if (answer === "Overwrite") return this.save(true);
        return false;
      }
      this.flash(`save failed: ${err}`, true);
      return false;
    }
  }

  /** Ask before discarding unsaved changes. Resolves true when the pane may close. */
  async confirmClose(): Promise<boolean> {
    if (!this.modified) return true;
    const answer = await Dialogs.Question({
      Title: "Unsaved changes",
      Message: `Save changes to ${basename(this.path)}?`,
      Buttons: [
        { Label: "Save", IsDefault: true },
        { Label: "Don't save" },
        { Label: "Cancel", IsCancel: true },
      ],
    });
    if (answer === "Save") return this.save();
    return answer === "Don't save";
  }

  private flash(text: string, error = false) {
    this.status.textContent = text;
    this.status.classList.toggle("error", error);
    setTimeout(() => {
      if (this.status.textContent === text) this.status.textContent = this.modified ? "modified" : "";
    }, 2000);
  }

  openSearch() {
    if (this.view) openSearchPanel(this.view);
  }

  selectedText(): string {
    if (!this.view) return "";
    const { from, to } = this.view.state.selection.main;
    return this.view.state.sliceDoc(from, to);
  }

  insertText(text: string) {
    this.view?.dispatch(this.view.state.replaceSelection(text));
  }

  focus() {
    if (this.mode === "preview") this.element.focus();
    else this.view?.focus();
  }

  async cwd() {
    return dirname(this.path);
  }

  relayout() {
    this.view?.requestMeasure();
  }

  dispose() {
    clearTimeout(this.previewTimer);
    this.view?.destroy();
    this.element.remove();
  }
}

const basename = (p: string) => p.slice(p.lastIndexOf("/") + 1);
const dirname = (p: string) => p.slice(0, Math.max(0, p.lastIndexOf("/"))) || "/";
