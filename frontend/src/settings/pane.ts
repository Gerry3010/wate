import { Dialogs } from "@wailsio/runtime";
import type { ImportedTab } from "../session";

import { ConfigService, ImportService, OpenerService, ThemeService, type Config, type ImportSource, type Resolved } from "../api";
import { chordId, eventChord } from "../keymap/keymap";
import type { Pane } from "../pane";

export interface SettingsPaneOptions {
  paneId: string;
  config: Config;
  configPath: string;
  keyLabels: Record<string, string>;
  onOpenConfigFile(): void;
  onClose(): void;
  /** Open tabs delivered by "Import from another terminal"; resolves to the number opened. */
  onImportTabs(tabs: ImportedTab[]): Promise<number>;
}

/** Where compatible colour schemes live (Ghostty and Alacritty exports of every scheme). */
const THEME_CATALOG = "https://github.com/mbadolato/iTerm2-Color-Schemes#readme";

/** Human-readable names for keybinding actions, in display order. */
const ACTIONS: [string, string][] = [
  ["split_right", "Split: new pane to the right"],
  ["split_down", "Split: new pane below"],
  ["focus_left", "Focus pane left"],
  ["focus_right", "Focus pane right"],
  ["focus_up", "Focus pane up"],
  ["focus_down", "Focus pane down"],
  ["resize_left", "Resize: divider left"],
  ["resize_right", "Resize: divider right"],
  ["resize_up", "Resize: divider up"],
  ["resize_down", "Resize: divider down"],
  ["swap_left", "Swap pane with the left one"],
  ["swap_right", "Swap pane with the right one"],
  ["swap_up", "Swap pane with the one above"],
  ["swap_down", "Swap pane with the one below"],
  ["close_pane", "Close pane"],
  ["close_tab", "Close tab"],
  ["new_tab", "New tab"],
  ["next_tab", "Next tab"],
  ["prev_tab", "Previous tab"],
  ["copy", "Copy"],
  ["paste", "Paste"],
  ["search", "Search"],
  ["launch_claude", "Launch Claude Code"],
  ["toggle_sidebar", "Toggle Claude sidebar"],
  ["open_settings", "Open settings"],
  ["editor_mode", "Editor: cycle Code / Split / Preview"],
  ["editor_save", "Editor: save"],
  ["font_bigger", "Font bigger"],
  ["font_smaller", "Font smaller"],
  ["font_reset", "Font reset"],
];

/** Settings form. Every control writes straight to config.toml; the app applies changes live. */
export class SettingsPane implements Pane {
  readonly id: string;
  readonly kind = "settings" as const;
  readonly element = document.createElement("div");
  title = "Settings";
  private config: Config;
  private body = document.createElement("div");
  private nav = document.createElement("nav");
  private content = document.createElement("div");
  private status = document.createElement("span");
  private themeGrid = document.createElement("div");
  private current = "appearance";

  private importTabs: (tabs: ImportedTab[]) => Promise<number>;

  constructor(opts: SettingsPaneOptions) {
    this.id = opts.paneId;
    this.config = opts.config;
    this.importTabs = opts.onImportTabs;
    this.element.className = "pane pane-settings";
    this.element.dataset.paneId = opts.paneId;
    this.element.tabIndex = -1;
    const toolbar = document.createElement("div");
    toolbar.className = "editor-toolbar";
    const name = document.createElement("span");
    name.className = "editor-name";
    name.textContent = "Settings";
    this.status.className = "editor-status";
    const openFile = document.createElement("button");
    openFile.className = "editor-save";
    openFile.textContent = "Open config.toml";
    openFile.title = opts.configPath;
    openFile.addEventListener("click", () => opts.onOpenConfigFile());
    const close = document.createElement("button");
    close.className = "editor-close";
    close.textContent = "×";
    close.addEventListener("click", () => opts.onClose());
    toolbar.append(name, this.status, openFile, close);
    this.body.className = "settings-body";
    this.nav.className = "settings-nav";
    this.content.className = "settings-content";
    this.body.append(this.nav, this.content);
    this.element.append(toolbar, this.body);
    this.build();
  }

  /** Called when the config changed (possibly from the file); rebuilds the form. */
  applyConfig(config: Config) {
    this.config = config;
    const scroll = this.content.scrollTop;
    this.build();
    this.content.scrollTop = scroll;
  }

  show(id: string) {
    this.current = id;
    // The themes page scrolls its own grid and pins the buttons; other pages scroll as a whole.
    this.content.classList.toggle("fixed", id === "appearance");
    for (const b of this.nav.querySelectorAll<HTMLButtonElement>("button")) b.classList.toggle("active", b.dataset.section === id);
    for (const sec of this.content.querySelectorAll<HTMLElement>("section")) sec.hidden = sec.dataset.section !== id;
    this.content.scrollTop = 0;
  }

  private async set(values: Record<string, unknown>) {
    try {
      await ConfigService.Set(values);
      this.flash("saved");
    } catch (err) {
      this.flash(`could not save: ${err}`, true);
    }
  }

  private flash(text: string, error = false) {
    this.status.textContent = text;
    this.status.classList.toggle("error", error);
    setTimeout(() => {
      if (this.status.textContent === text) this.status.textContent = "";
    }, error ? 6000 : 1500);
  }

  // ---- form ---------------------------------------------------------------

  private build() {
    const c = this.config;
    this.nav.replaceChildren();
    this.content.replaceChildren();

    const appearance = this.section("Themes", "appearance");
    appearance.classList.add("settings-section-themes");
    this.themeGrid.className = "theme-scroll";
    appearance.appendChild(this.themeGrid);
    void this.buildThemeGrid();
    const footer = document.createElement("div");
    footer.className = "theme-footer";
    const actions = document.createElement("div");
    actions.className = "settings-actions";
    const pasteBtn = document.createElement("button");
    pasteBtn.textContent = "Paste theme…";
    pasteBtn.title = "Paste a Ghostty, Alacritty or wate theme and save it under a name";
    const importBtn = document.createElement("button");
    importBtn.textContent = "Import file…";
    importBtn.title = "Ghostty, Alacritty or wate theme file";
    importBtn.addEventListener("click", () => void this.importTheme());
    const folder = document.createElement("button");
    folder.textContent = "Open folder";
    folder.title = "Open the themes folder";
    folder.addEventListener("click", () => void ThemeService.ThemesDir().then((d) => OpenerService.Open(d)));
    actions.append(pasteBtn, importBtn, folder);
    const note = document.createElement("p");
    note.className = "settings-hint";
    note.innerHTML =
      '<a class="settings-link" href="#">Browse 400+ compatible themes ↗</a><br>' +
      "Any scheme from <b>iTerm2-Color-Schemes</b> works: open a file in its <code>ghostty/</code> or <code>alacritty/</code> " +
      "folder, copy the text and paste it here — or import the downloaded file. Your themes live in the themes folder as editable TOML.";
    note.querySelector("a")!.addEventListener("click", (e) => {
      e.preventDefault();
      void OpenerService.OpenURL(THEME_CATALOG);
    });
    const paste = this.pasteForm();
    pasteBtn.addEventListener("click", () => {
      paste.hidden = !paste.hidden;
      if (!paste.hidden) paste.querySelector<HTMLInputElement>("input")?.focus();
    });
    footer.append(paste, actions, note);
    appearance.appendChild(footer);

    const bg = this.section("Background", "background");
    const mode = c.background.mode;
    this.select(
      bg,
      "Mode",
      "background.mode",
      mode,
      [
        ["solid", "Solid"],
        ["translucent", "Translucent (OS / compositor blur, restart needed)"],
        ["wallpaper", "Wallpaper (blurred image)"],
      ],
      async (v, sel) => {
        // Wallpaper mode needs an image: ask for one right away instead of silently staying solid.
        if (v !== "wallpaper" || c.background.wallpaper) return true;
        const p = await this.pickWallpaper();
        if (p) await this.set({ "background.wallpaper": p, "background.mode": "wallpaper" });
        else sel.value = mode;
        return false;
      },
    );
    if (mode === "wallpaper") {
      this.file(bg, "Wallpaper", "background.wallpaper", c.background.wallpaper);
      this.range(bg, "Blur", "background.blur", c.background.blur, 0, 80, 1, "px");
      this.range(bg, "Dim", "background.dim", c.background.dim, 0, 1, 0.05);
    }
    if (mode !== "solid") this.range(bg, "Opacity", "background.opacity", c.background.opacity, 0.2, 1, 0.05);
    const bgHint = document.createElement("p");
    bgHint.className = "settings-hint";
    bgHint.textContent =
      mode === "solid"
        ? "Blur, dim and opacity only apply to the wallpaper and translucent modes."
        : mode === "translucent"
          ? "Opacity is how much of the blurred desktop shows through the panes. On GNOME, add wate to Blur my Shell's application list."
          : "Blur and dim soften the image; opacity controls how much of it shows through the panes.";
    bg.appendChild(bgHint);

    const term = this.section("Terminal", "terminal");
    this.text(term, "Font", "terminal.font", c.terminal.font);
    this.number(term, "Font size", "terminal.font_size", c.terminal.font_size, 6, 40, "px");
    this.number(term, "Line height", "terminal.line_height", c.terminal.line_height, 0.8, 2.5, "", 0.05);
    this.check(term, "Ligatures", "terminal.ligatures", c.terminal.ligatures);
    this.select(term, "Cursor", "terminal.cursor_style", c.terminal.cursor_style, [["block", "Block"], ["underline", "Underline"], ["bar", "Bar"]]);
    this.check(term, "Cursor blink", "terminal.cursor_blink", c.terminal.cursor_blink);
    this.number(term, "Scrollback", "terminal.scrollback", c.terminal.scrollback, 100, 1000000, " lines", 100);
    this.number(term, "Padding", "terminal.padding", c.terminal.padding, 0, 40, "px");

    const ed = this.section("Editor", "editor");
    this.text(ed, "Font", "editor.font", c.editor.font);
    this.number(ed, "Font size", "editor.font_size", c.editor.font_size, 6, 40, "px");
    this.number(ed, "Line height", "editor.line_height", c.editor.line_height, 0.8, 2.5, "", 0.05);
    this.check(ed, "Ligatures", "editor.ligatures", c.editor.ligatures);
    this.select(ed, "Markdown opens in", "editor.markdown_default_mode", c.editor.markdown_default_mode, [["code", "Code"], ["split", "Split"], ["preview", "Preview"]]);
    this.number(ed, "Tab width", "editor.tab_width", c.editor.tab_width, 1, 16);
    this.check(ed, "Word wrap", "editor.word_wrap", c.editor.word_wrap);
    this.check(ed, "Line numbers", "editor.line_numbers", c.editor.line_numbers);

    const gen = this.section("Shell & Startup", "general");
    this.text(gen, "Shell", "general.shell", c.general.shell, "empty = $SHELL");
    this.list(gen, "Shell arguments", "general.shell_args", c.general.shell_args ?? [], "e.g. -l, --login");
    this.check(gen, "Shell integration (zsh)", "general.shell_integration", c.general.shell_integration);
    this.check(gen, "Restore session on start", "general.restore_session", c.general.restore_session);
    this.number(gen, "Window width", "window.width", c.window.width, 400, 10000, "px", 10);
    this.number(gen, "Window height", "window.height", c.window.height, 240, 10000, "px", 10);
    this.check(gen, "Remember window size & position", "window.remember_size", c.window.remember_size);
    const winHint = document.createElement("p");
    winHint.className = "settings-hint";
    winHint.textContent = "Width and height are the default for a fresh window (a second wate while one runs, or when remembering is off). Panes: Alt+drag a pane onto another to swap them, or use the swap keybindings.";
    gen.appendChild(winHint);
    this.list(gen, "Pass-through chords", "general.passthrough", c.general.passthrough ?? [], "chords the terminal keeps even if bound, e.g. ctrl+space");

    const cl = this.section("Claude Code", "claude");
    this.text(cl, "Command", "claude.command", c.claude.command);
    this.check(cl, "Desktop notifications", "claude.notify", c.claude.notify);
    this.number(cl, "Context window", "claude.context_window", c.claude.context_window, 0, 10000000, " tokens (0 = auto)", 1000);
    const clHint = document.createElement("p");
    clHint.className = "settings-hint";
    clHint.textContent = "The sidebar shows how full each session's context is (read from Claude Code's transcript). Set the window size only if the guess for your model is wrong.";
    cl.appendChild(clHint);

    const imp = this.section("Import", "import");
    const impHint = document.createElement("p");
    impHint.className = "settings-hint";
    impHint.textContent =
      "Bring theme, font, window and tab settings over from another terminal installed on this machine. " +
      "Tick what you want, everything else stays as it is.";
    imp.appendChild(impHint);
    const impList = document.createElement("div");
    impList.className = "import-list";
    impList.textContent = "Looking for terminals…";
    imp.appendChild(impList);
    void this.buildImport(impList);

    const keys = this.section("Keybindings", "keys");
    const hint = document.createElement("p");
    hint.className = "settings-hint";
    hint.textContent = "Click a binding and press the new keys. Esc cancels, Backspace clears.";
    keys.appendChild(hint);
    const table = document.createElement("div");
    table.className = "keys-table";
    for (const [action, label] of ACTIONS) this.keyRow(table, action, label, c.keys?.[action] ?? "");
    keys.appendChild(table);

    this.show(this.current);
  }

  private async importTheme() {
    try {
      const p = await Dialogs.OpenFile({ Title: "Import theme (Ghostty, Alacritty or wate TOML)" });
      if (!p) return;
      const id = await ThemeService.Import(p);
      await this.set({ "general.theme": id });
      this.flash(`imported ${id}`);
    } catch (err) {
      this.flash(String(err), true);
    }
  }

  private async buildThemeGrid() {
    const infos = (await ThemeService.ListInfo().catch(() => [])) ?? [];
    const cards = await Promise.all(
      infos.map(async (info) => {
        const t = await ThemeService.Preview(info.id).catch(() => null);
        return t ? { info, card: this.themeCard(t, !info.builtin) } : null;
      }),
    );
    const ok = cards.filter((c): c is NonNullable<typeof c> => !!c);
    const group = (title: string, items: typeof ok, empty?: string) => {
      const h = document.createElement("h3");
      h.className = "theme-group";
      h.textContent = title;
      const grid = document.createElement("div");
      grid.className = "theme-grid";
      grid.append(...items.map((c) => c.card));
      if (items.length === 0 && empty) {
        const e = document.createElement("p");
        e.className = "settings-hint";
        e.textContent = empty;
        grid.appendChild(e);
      }
      return [h, grid];
    };
    this.themeGrid.replaceChildren(
      ...group(
        "Your themes",
        ok.filter((c) => !c.info.builtin),
        "None yet — paste a theme from the catalog or import a file with the buttons below.",
      ),
      ...group("Built-in", ok.filter((c) => c.info.builtin)),
    );
  }

  /** Inline form: name + pasted theme text → user theme. */
  private pasteForm(): HTMLElement {
    const form = document.createElement("form");
    form.className = "theme-paste";
    form.hidden = true;
    const name = document.createElement("input");
    name.type = "text";
    name.placeholder = "Theme name (e.g. Rosé Pine Moon)";
    name.required = true;
    const text = document.createElement("textarea");
    text.placeholder = "Paste the theme here: Ghostty (palette = 0=#… lines), Alacritty TOML ([colors.primary] …) or wate TOML ([colors] …)";
    text.rows = 7;
    text.spellcheck = false;
    for (const el of [name, text]) el.addEventListener("keydown", (e) => e.stopPropagation());
    const row = document.createElement("div");
    row.className = "settings-actions";
    const save = document.createElement("button");
    save.type = "submit";
    save.textContent = "Save & activate";
    const cancel = document.createElement("button");
    cancel.type = "button";
    cancel.textContent = "Cancel";
    cancel.addEventListener("click", () => (form.hidden = true));
    row.append(save, cancel);
    form.append(name, text, row);
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      try {
        const id = await ThemeService.ImportText(name.value, text.value);
        await this.set({ "general.theme": id });
        this.flash(`saved ${id}`);
        name.value = "";
        text.value = "";
        form.hidden = true;
        void this.buildThemeGrid();
      } catch (err) {
        this.flash(String(err), true);
      }
    });
    return form;
  }

  private async deleteTheme(id: string) {
    try {
      await ThemeService.Delete(id);
      this.flash(`deleted ${id}`);
      if (this.config.general.theme === id) await this.set({ "general.theme": "catppuccin-mocha" });
      void this.buildThemeGrid();
    } catch (err) {
      this.flash(String(err), true);
    }
  }

  private themeCard(t: Resolved, deletable = false): HTMLElement {
    const c = t.theme.colors;
    const card = document.createElement("button");
    card.className = "theme-card" + (t.id === this.config.general.theme ? " active" : "");
    card.style.background = c.background;
    card.style.color = c.foreground;
    card.style.borderColor = t.id === this.config.general.theme ? t.theme.ui.accent : t.theme.ui.border;
    card.title = t.id;
    const prompt = document.createElement("div");
    prompt.className = "theme-sample";
    prompt.innerHTML =
      `<span style="color:${c.green}">❯</span> <span style="color:${c.blue}">git</span> commit <span style="color:${c.yellow}">-m</span> <span style="color:${c.green}">"${t.theme.name}"</span>` +
      `<br><span style="color:${c.magenta}">func</span> <span style="color:${c.cyan}">main</span>() { <span style="color:${c.red}">return</span> }`;
    const swatches = document.createElement("div");
    swatches.className = "theme-swatches";
    for (const col of [c.black, c.red, c.green, c.yellow, c.blue, c.magenta, c.cyan, c.white]) {
      const s = document.createElement("span");
      s.style.background = col;
      swatches.appendChild(s);
    }
    const name = document.createElement("div");
    name.className = "theme-name";
    name.textContent = t.theme.name;
    name.style.color = t.theme.ui.accent;
    card.append(name, prompt, swatches);
    card.addEventListener("click", () => void this.set({ "general.theme": t.id }));
    if (deletable) {
      const del = document.createElement("span");
      del.className = "theme-delete";
      del.textContent = "×";
      del.title = "Delete this theme";
      del.setAttribute("role", "button");
      del.addEventListener("click", (e) => {
        e.stopPropagation();
        void this.deleteTheme(t.id);
      });
      card.appendChild(del);
    }
    return card;
  }

  private async buildImport(list: HTMLElement) {
    let sources: ImportSource[] = [];
    try {
      sources = (await ImportService.Detect()) ?? [];
    } catch (err) {
      list.textContent = `Could not scan: ${err}`;
      return;
    }
    list.replaceChildren();
    if (sources.length === 0) {
      list.textContent = "No Warp, Ghostty, Alacritty or kitty configuration found.";
      return;
    }
    for (const src of sources) {
      const card = document.createElement("div");
      card.className = "import-card";
      const head = document.createElement("div");
      head.className = "import-head";
      const name = document.createElement("strong");
      name.textContent = src.name;
      const path = document.createElement("span");
      path.className = "import-path";
      path.textContent = src.path;
      head.append(name, path);
      card.appendChild(head);
      const boxes: HTMLInputElement[] = [];
      for (const item of src.items ?? []) {
        const row = document.createElement("label");
        row.className = "import-item";
        const cb = document.createElement("input");
        cb.type = "checkbox";
        cb.value = item.key;
        cb.checked = item.key !== "tabs" && item.key !== "history" && !item.detail.includes("not on disk") && !item.detail.includes("not found");
        const label = document.createElement("span");
        label.className = "import-label";
        label.textContent = item.label;
        const detail = document.createElement("span");
        detail.className = "import-detail";
        detail.textContent = item.detail;
        row.append(cb, label, detail);
        card.appendChild(row);
        boxes.push(cb);
      }
      const actions = document.createElement("div");
      actions.className = "settings-actions";
      const btn = document.createElement("button");
      btn.textContent = `Import selected from ${src.name}`;
      const out = document.createElement("span");
      out.className = "settings-hint import-result";
      btn.addEventListener("click", async () => {
        const keys = boxes.filter((b) => b.checked).map((b) => b.value);
        if (keys.length === 0) {
          out.textContent = "Nothing selected.";
          return;
        }
        btn.disabled = true;
        out.textContent = "Importing…";
        try {
          const res = await ImportService.Apply(src.id, keys);
          const done: string[] = [];
          const n = Object.keys(res.settings ?? {}).length;
          if (n) done.push(`${n} setting${n === 1 ? "" : "s"} written`);
          if (res.theme_id) done.push(`theme "${res.theme_id}" installed and activated`);
          if (res.tabs?.length) {
            const opened = await this.importTabs(res.tabs as unknown as ImportedTab[]);
            done.push(`${opened} tab${opened === 1 ? "" : "s"} opened`);
          }
          out.textContent = (done.length ? done.join(", ") + "." : "Nothing to import.") + (res.notes?.length ? " " + res.notes.join(" ") : "");
          if (res.settings && "background.mode" in res.settings && res.settings["background.mode"] === "translucent") {
            out.textContent += " Restart wate for the translucent window.";
          }
          this.flash("imported");
        } catch (err) {
          out.textContent = `Import failed: ${err}`;
          this.flash(String(err), true);
        } finally {
          btn.disabled = false;
        }
      });
      actions.append(btn, out);
      card.appendChild(actions);
      list.appendChild(card);
    }
  }

  private section(title: string, id: string): HTMLElement {
    const b = document.createElement("button");
    b.dataset.section = id;
    b.textContent = title;
    b.addEventListener("click", () => this.show(id));
    this.nav.appendChild(b);
    const s = document.createElement("section");
    s.className = "settings-section";
    s.dataset.section = id;
    s.hidden = true;
    const h = document.createElement("h2");
    h.textContent = title;
    s.appendChild(h);
    this.content.appendChild(s);
    return s;
  }

  /** Comma-separated list input for string arrays. */
  private list(parent: HTMLElement, label: string, key: string, value: string[], placeholder = "") {
    const i = document.createElement("input");
    i.type = "text";
    i.value = value.join(", ");
    i.placeholder = placeholder;
    i.addEventListener("change", () => {
      const items = i.value.split(",").map((x) => x.trim()).filter(Boolean);
      void this.set({ [key]: items });
    });
    this.row(parent, label, i);
  }

  private row(parent: HTMLElement, label: string, control: HTMLElement, suffix = ""): HTMLElement {
    const r = document.createElement("label");
    r.className = "settings-row";
    const l = document.createElement("span");
    l.className = "settings-label";
    l.textContent = label;
    const wrap = document.createElement("span");
    wrap.className = "settings-control";
    wrap.appendChild(control);
    if (suffix) {
      const s = document.createElement("span");
      s.className = "settings-suffix";
      s.textContent = suffix;
      wrap.appendChild(s);
    }
    r.append(l, wrap);
    parent.appendChild(r);
    return r;
  }

  private text(parent: HTMLElement, label: string, key: string, value: string, placeholder = "") {
    const i = document.createElement("input");
    i.type = "text";
    i.value = value ?? "";
    i.placeholder = placeholder;
    i.addEventListener("change", () => void this.set({ [key]: i.value }));
    this.row(parent, label, i);
  }

  private number(parent: HTMLElement, label: string, key: string, value: number, min: number, max: number, suffix = "", step = 1) {
    const i = document.createElement("input");
    i.type = "number";
    i.min = String(min);
    i.max = String(max);
    i.step = String(step);
    i.value = String(value ?? "");
    i.addEventListener("change", () => {
      const n = Number(i.value);
      if (Number.isFinite(n)) void this.set({ [key]: n });
    });
    this.row(parent, label, i, suffix);
  }

  private range(parent: HTMLElement, label: string, key: string, value: number, min: number, max: number, step: number, suffix = "") {
    const i = document.createElement("input");
    i.type = "range";
    i.min = String(min);
    i.max = String(max);
    i.step = String(step);
    i.value = String(value ?? min);
    const out = document.createElement("span");
    out.className = "settings-suffix";
    const show = () => (out.textContent = `${i.value}${suffix}`);
    show();
    i.addEventListener("input", show);
    i.addEventListener("change", () => void this.set({ [key]: Number(i.value) }));
    const r = this.row(parent, label, i);
    r.querySelector(".settings-control")!.appendChild(out);
  }

  private check(parent: HTMLElement, label: string, key: string, value: boolean) {
    const i = document.createElement("input");
    i.type = "checkbox";
    i.checked = !!value;
    i.addEventListener("change", () => void this.set({ [key]: i.checked }));
    this.row(parent, label, i);
  }

  private select(
    parent: HTMLElement,
    label: string,
    key: string,
    value: string,
    options: [string, string][],
    intercept?: (value: string, select: HTMLSelectElement) => Promise<boolean>,
  ) {
    const s = document.createElement("select");
    for (const [v, text] of options) {
      const o = document.createElement("option");
      o.value = v;
      o.textContent = text;
      o.selected = v === value;
      s.appendChild(o);
    }
    s.addEventListener("change", async () => {
      if (intercept && !(await intercept(s.value, s))) return;
      void this.set({ [key]: s.value });
    });
    this.row(parent, label, s);
  }

  private async pickWallpaper(): Promise<string> {
    try {
      const p = await Dialogs.OpenFile({
        Title: "Choose wallpaper",
        Filters: [{ DisplayName: "Images", Pattern: "*.png;*.jpg;*.jpeg;*.webp;*.avif" }],
      });
      return p ?? "";
    } catch (err) {
      this.flash(String(err), true);
      return "";
    }
  }

  private file(parent: HTMLElement, label: string, key: string, value: string) {
    const wrap = document.createElement("span");
    wrap.className = "settings-file";
    const i = document.createElement("input");
    i.type = "text";
    i.value = value ?? "";
    i.placeholder = "path to an image";
    i.addEventListener("change", () => void this.set({ [key]: i.value }));
    const b = document.createElement("button");
    b.textContent = "Choose…";
    b.addEventListener("click", async (e) => {
      e.preventDefault();
      const p = await this.pickWallpaper();
      if (p) {
        i.value = p;
        await this.set({ [key]: p, "background.mode": "wallpaper" });
      }
    });
    wrap.append(i, b);
    this.row(parent, label, wrap);
  }

  private keyRow(table: HTMLElement, action: string, label: string, chord: string) {
    const row = document.createElement("div");
    row.className = "keys-row";
    const l = document.createElement("span");
    l.textContent = label;
    const b = document.createElement("button");
    b.className = "keys-chord";
    b.textContent = chord ? pretty(chord) : "—";
    b.title = action;
    b.addEventListener("click", () => {
      b.textContent = "press keys…";
      b.classList.add("recording");
      const onKey = (e: KeyboardEvent) => {
        e.preventDefault();
        e.stopPropagation();
        if (["Control", "Shift", "Alt", "Meta"].includes(e.key)) return;
        window.removeEventListener("keydown", onKey, true);
        b.classList.remove("recording");
        if (e.key === "Escape") {
          b.textContent = chord ? pretty(chord) : "—";
          return;
        }
        const next = e.key === "Backspace" ? "" : chordId(eventChord(e));
        b.textContent = next ? pretty(next) : "—";
        void this.set({ [`keys.${action}`]: next });
      };
      window.addEventListener("keydown", onKey, true);
    });
    row.append(l, b);
    table.appendChild(row);
  }

  focus() {
    this.element.focus();
  }

  async cwd() {
    return "";
  }

  relayout() {}

  dispose() {
    this.element.remove();
  }
}

function pretty(chord: string): string {
  return chord
    .split("+")
    .map((p) => (p === "meta" ? "Cmd" : p[0].toUpperCase() + p.slice(1)))
    .join("+");
}
