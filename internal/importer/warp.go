package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"github.com/Gerry3010/wate/internal/theme"
)

// warp imports Warp (warp.dev): settings.toml / user_preferences.json, YAML themes,
// keybindings.yaml and the saved tabs (titles, colours, split layouts, cwds) from warp.sqlite.
type warp struct{}

func (warp) id() string   { return "warp" }
func (warp) name() string { return "Warp" }

// warpPaths are the candidate locations per platform.
type warpPaths struct {
	settings, prefs, keys, themes, db string
}

func (warp) paths(env Env) warpPaths {
	if env.GOOS == "darwin" {
		return warpPaths{
			settings: filepath.Join(env.Home, ".warp", "settings.toml"),
			keys:     filepath.Join(env.Home, ".warp", "keybindings.yaml"),
			themes:   filepath.Join(env.Home, ".warp", "themes"),
			db:       filepath.Join(env.Home, "Library", "Application Support", "dev.warp.Warp-Stable", "warp.sqlite"),
		}
	}
	return warpPaths{
		settings: filepath.Join(env.ConfigHome, "warp-terminal", "settings.toml"),
		prefs:    filepath.Join(env.ConfigHome, "warp-terminal", "user_preferences.json"),
		keys:     filepath.Join(env.ConfigHome, "warp-terminal", "keybindings.yaml"),
		themes:   filepath.Join(env.DataHome, "warp-terminal", "themes"),
		db:       filepath.Join(env.StateHome, "warp-terminal", "warp.sqlite"),
	}
}

// warpSettings is the subset of Warp's settings.toml wate cares about.
type warpSettings struct {
	Appearance struct {
		Text struct {
			FontName  string  `toml:"font_name"`
			FontSize  float64 `toml:"font_size"`
			Ligatures *bool   `toml:"ligature_rendering_enabled"`
		} `toml:"text"`
		Window struct {
			OverrideOpacity float64 `toml:"override_opacity"`
		} `toml:"window"`
		Themes struct {
			Theme any `toml:"theme"`
		} `toml:"themes"`
	} `toml:"appearance"`
}

// warpState is what we learned from settings/preferences.
type warpState struct {
	font      string
	size      float64
	ligatures *bool
	opacity   float64 // 0..100, 100 = opaque
	themeName string
	themePath string // "" for Warp's built-in themes (not on disk)
}

func (w warp) read(env Env) (warpState, string) {
	p := w.paths(env)
	st := warpState{opacity: 100}
	src := ""
	if exists(p.settings) {
		src = p.settings
		var s warpSettings
		if _, err := toml.DecodeFile(p.settings, &s); err == nil {
			st.font, st.size, st.ligatures = s.Appearance.Text.FontName, s.Appearance.Text.FontSize, s.Appearance.Text.Ligatures
			if s.Appearance.Window.OverrideOpacity > 0 {
				st.opacity = s.Appearance.Window.OverrideOpacity
			}
			st.themeName, st.themePath = warpThemeRef(s.Appearance.Themes.Theme)
		}
	} else if exists(p.prefs) {
		src = p.prefs
		st = w.readPrefs(p.prefs, st)
	}
	if st.themePath != "" && !filepath.IsAbs(st.themePath) {
		st.themePath = filepath.Join(p.themes, st.themePath)
	}
	return st, src
}

// warpThemeRef decodes `theme = "phenomenon"` or `theme = { custom = { name, path } }`.
func warpThemeRef(v any) (name, path string) {
	switch t := v.(type) {
	case string:
		return t, ""
	case map[string]any:
		for _, k := range []string{"custom", "Custom"} {
			if c, ok := t[k].(map[string]any); ok {
				name, _ := c["name"].(string)
				path, _ := c["path"].(string)
				return name, path
			}
		}
	}
	return "", ""
}

// readPrefs falls back to user_preferences.json (values are JSON-encoded strings).
func (warp) readPrefs(path string, st warpState) warpState {
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	var doc struct {
		Prefs map[string]string `json:"prefs"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return st
	}
	str := func(k string) string {
		var s string
		if json.Unmarshal([]byte(doc.Prefs[k]), &s) == nil {
			return s
		}
		return doc.Prefs[k]
	}
	st.font = str("FontName")
	st.size, _ = strconv.ParseFloat(doc.Prefs["FontSize"], 64)
	if v, err := strconv.ParseBool(doc.Prefs["LigatureRenderingEnabled"]); err == nil {
		st.ligatures = &v
	}
	if v, err := strconv.ParseFloat(doc.Prefs["OverrideOpacity"], 64); err == nil && v > 0 {
		st.opacity = v
	}
	var ref any
	if json.Unmarshal([]byte(doc.Prefs["Theme"]), &ref) == nil {
		st.themeName, st.themePath = warpThemeRef(ref)
	}
	return st
}

func (w warp) detect(env Env) (string, []Item) {
	p := w.paths(env)
	st, src := w.read(env)
	if src == "" && !exists(p.db) {
		return "", nil
	}
	var items []Item
	if st.themePath != "" && exists(st.themePath) {
		items = append(items, Item{Key: "theme", Label: "Theme", Detail: st.themeName})
		if wt, err := readWarpTheme(st.themePath); err == nil && wt.BackgroundImage.Path != "" {
			img := expand(env, wt.BackgroundImage.Path, filepath.Dir(st.themePath))
			if exists(img) {
				items = append(items, Item{Key: "wallpaper", Label: "Background image", Detail: filepath.Base(img)})
			}
		}
	} else if st.themeName != "" {
		items = append(items, Item{Key: "theme", Label: "Theme", Detail: st.themeName + " (built-in Warp theme, not on disk — import a matching scheme from iTerm2-Color-Schemes instead)"})
	}
	if st.font != "" || st.size > 0 {
		items = append(items, Item{Key: "font", Label: "Font", Detail: fontDetail(st.font, st.size, st.ligatures)})
	}
	if st.opacity > 0 && st.opacity < 100 {
		items = append(items, Item{Key: "opacity", Label: "Window opacity", Detail: fmt.Sprintf("%g%%", st.opacity)})
	}
	if keys := w.readKeys(p.keys); len(keys) > 0 {
		items = append(items, Item{Key: "keys", Label: "Key bindings", Detail: fmt.Sprintf("%d mapped", len(keys))})
	}
	if exists(p.db) && env.SQLite != nil {
		if tabs, err := w.readTabs(env, p.db); err == nil && len(tabs) > 0 {
			panes := 0
			for _, t := range tabs {
				panes += countLeaves(t.Root)
			}
			items = append(items, Item{Key: "tabs", Label: "Tabs & panes", Detail: fmt.Sprintf("%d tabs, %d panes: %s", len(tabs), panes, tabTitles(tabs))})
		}
	}
	if src == "" {
		src = p.db
	}
	return src, items
}

func tabTitles(tabs []Tab) string {
	var names []string
	for _, t := range tabs {
		if t.Title != "" {
			names = append(names, t.Title)
		}
	}
	s := strings.Join(names, ", ")
	if len(s) > 80 {
		s = s[:77] + "…"
	}
	return s
}

func (w warp) apply(env Env, want map[string]bool, res *Result) error {
	p := w.paths(env)
	st, _ := w.read(env)
	if want["theme"] || want["wallpaper"] {
		if st.themePath == "" || !exists(st.themePath) {
			res.Notes = append(res.Notes, "Warp theme "+st.themeName+" is built into Warp and cannot be read from disk.")
		} else {
			wt, err := readWarpTheme(st.themePath)
			if err != nil {
				return fmt.Errorf("warp theme: %w", err)
			}
			if want["theme"] {
				name := wt.Name
				if name == "" {
					name = strings.TrimSuffix(filepath.Base(st.themePath), filepath.Ext(st.themePath))
				}
				if err := writeTheme(env, wt.toTheme(), name, "Warp "+filepath.Base(st.themePath), res); err != nil {
					return err
				}
			}
			if want["wallpaper"] && wt.BackgroundImage.Path != "" {
				img := expand(env, wt.BackgroundImage.Path, filepath.Dir(st.themePath))
				op := wt.BackgroundImage.Opacity
				if op <= 0 {
					op = 100
				}
				// Warp lays the image over the background at `opacity` percent: the rest shows the
				// background colour — that is exactly wate's dim.
				setWallpaper(res, img, 1-op/100, -1)
			}
		}
	}
	if want["font"] {
		setFont(res, st.font, st.size, st.ligatures)
	}
	if want["opacity"] {
		setOpacity(res, st.opacity/100)
	}
	if want["keys"] {
		setKeys(res, w.readKeys(p.keys))
	}
	if want["tabs"] {
		if env.SQLite == nil {
			res.Notes = append(res.Notes, "Tabs need the sqlite3 command-line tool, which was not found.")
		} else {
			tabs, err := w.readTabs(env, p.db)
			if err != nil {
				return fmt.Errorf("warp tabs: %w", err)
			}
			res.Tabs = tabs
		}
	}
	return nil
}

// ---- themes -------------------------------------------------------------

type warpTheme struct {
	Name            string `yaml:"name"`
	Accent          string `yaml:"accent"`
	Background      any    `yaml:"background"` // "#hex" or {top, bottom} gradient
	Foreground      string `yaml:"foreground"`
	Cursor          string `yaml:"cursor"`
	Selection       string `yaml:"selection"`
	Details         string `yaml:"details"`
	BackgroundImage struct {
		Path    string  `yaml:"path"`
		Opacity float64 `yaml:"opacity"`
	} `yaml:"background_image"`
	TerminalColors struct {
		Normal map[string]string `yaml:"normal"`
		Bright map[string]string `yaml:"bright"`
	} `yaml:"terminal_colors"`
}

func readWarpTheme(path string) (warpTheme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return warpTheme{}, err
	}
	var t warpTheme
	if err := yaml.Unmarshal(data, &t); err != nil {
		return warpTheme{}, err
	}
	return t, nil
}

func (wt warpTheme) background() string {
	switch b := wt.Background.(type) {
	case string:
		return theme.NormHex(b)
	case map[string]any:
		// Gradient: the bottom colour is the dominant one in a terminal.
		for _, k := range []string{"bottom", "top"} {
			if s, ok := b[k].(string); ok && theme.NormHex(s) != "" {
				return theme.NormHex(s)
			}
		}
	}
	return ""
}

func (wt warpTheme) toTheme() theme.Theme {
	var t theme.Theme
	t.Name = wt.Name
	t.Colors.Background = wt.background()
	t.Colors.Foreground = theme.NormHex(wt.Foreground)
	t.Colors.Cursor = theme.NormHex(wt.Cursor)
	t.Colors.SelectionBackground = theme.NormHex(wt.Selection)
	t.UI.Accent = theme.NormHex(wt.Accent)
	var pal [16]string
	order := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}
	for i, n := range order {
		pal[i] = theme.NormHex(wt.TerminalColors.Normal[n])
		pal[i+8] = theme.NormHex(wt.TerminalColors.Bright[n])
	}
	theme.AssignPalette(&t.Colors, pal)
	return t
}

// ---- key bindings ---------------------------------------------------------

// warpActions maps Warp action ids to wate actions.
var warpActions = map[string]string{
	"pane_group:add_right":                  "split_right",
	"pane_group:add_down":                   "split_down",
	"pane_group:navigate_left":              "focus_left",
	"pane_group:navigate_right":             "focus_right",
	"pane_group:navigate_up":                "focus_up",
	"pane_group:navigate_down":              "focus_down",
	"pane_group:navigate_prev":              "focus_left",
	"pane_group:navigate_next":              "focus_right",
	"pane_group:resize_left":                "resize_left",
	"pane_group:resize_right":               "resize_right",
	"pane_group:resize_up":                  "resize_up",
	"pane_group:resize_down":                "resize_down",
	"pane_group:close_pane":                 "close_pane",
	"workspace:close_active_pane_group":     "close_pane",
	"workspace:new_tab":                     "new_tab",
	"workspace:activate_next_tab":           "next_tab",
	"workspace:activate_prev_tab":           "prev_tab",
	"terminal:copy":                         "copy",
	"terminal:paste":                        "paste",
	"terminal:find":                         "search",
	"workspace:show_settings_modal":         "open_settings",
	"workspace:show_settings_page":          "open_settings",
	"terminal:increase_font_size":           "font_bigger",
	"terminal:decrease_font_size":           "font_smaller",
	"terminal:reset_font_size":              "font_reset",
	"workspace:activate_tab_at_index_0":     "tab_1",
	"workspace:activate_tab_at_index_1":     "tab_2",
	"workspace:activate_tab_at_index_2":     "tab_3",
	"workspace:activate_tab_at_index_3":     "tab_4",
	"workspace:activate_tab_at_index_4":     "tab_5",
	"workspace:activate_tab_at_index_5":     "tab_6",
	"workspace:activate_tab_at_index_6":     "tab_7",
	"workspace:activate_tab_at_index_7":     "tab_8",
	"workspace:activate_last_tab":           "tab_9",
	"workspace:toggle_warp_ai_panel":        "toggle_sidebar",
	"workspace:toggle_agent_management_panel": "toggle_sidebar",
}

func (warp) readKeys(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw map[string]string
	if yaml.Unmarshal(data, &raw) != nil {
		return nil
	}
	out := map[string]string{}
	for action, spec := range raw {
		if wateAction, ok := warpActions[action]; ok {
			if c := chord(spec); c != "" {
				out[wateAction] = c
			}
		}
	}
	return out
}

// ---- tabs ---------------------------------------------------------------

// readTabs rebuilds Warp's saved windows: tabs → pane_nodes tree → terminal cwds.
func (warp) readTabs(env Env, db string) ([]Tab, error) {
	tabs, err := env.SQLite(db, `SELECT t.id, t.custom_title, t.color, g.name AS group_name, g.color AS group_color
		FROM tabs t LEFT JOIN tab_groups g ON g.id = t.tab_group_id ORDER BY t.window_id, t.id`)
	if err != nil {
		return nil, err
	}
	nodes, err := env.SQLite(db, `SELECT n.id, n.tab_id, n.parent_pane_node_id AS parent, n.flex, n.is_leaf,
		b.horizontal, l.kind, l.is_focused, p.cwd
		FROM pane_nodes n
		LEFT JOIN pane_branches b ON b.pane_node_id = n.id
		LEFT JOIN pane_leaves l ON l.pane_node_id = n.id
		LEFT JOIN terminal_panes p ON p.id = n.id
		ORDER BY n.id`)
	if err != nil {
		return nil, err
	}
	byTab := map[int][]map[string]any{}
	for _, n := range nodes {
		byTab[rowInt(n, "tab_id")] = append(byTab[rowInt(n, "tab_id")], n)
	}
	var out []Tab
	for _, t := range tabs {
		id := rowInt(t, "id")
		root := warpTree(env, byTab[id])
		if root == nil {
			continue
		}
		tab := Tab{Title: strings.Trim(rowStr(t, "custom_title"), `"`), Color: strings.ToLower(rowStr(t, "color")), Root: root}
		if g := rowStr(t, "group_name"); g != "" {
			tab.Group = g
			if tab.Color == "" {
				tab.Color = strings.ToLower(rowStr(t, "group_color"))
			}
		}
		out = append(out, tab)
	}
	return out, nil
}

// warpTree converts one tab's pane_nodes rows into a binary layout.
func warpTree(env Env, rows []map[string]any) *Node {
	children := map[int][]map[string]any{}
	var root map[string]any
	for _, r := range rows {
		if r["parent"] == nil {
			root = r
			continue
		}
		p := rowInt(r, "parent")
		children[p] = append(children[p], r)
	}
	if root == nil {
		return nil
	}
	var build func(r map[string]any) *Node
	build = func(r map[string]any) *Node {
		if rowInt(r, "is_leaf") == 1 {
			kind := rowStr(r, "kind")
			if kind != "" && kind != "terminal" && kind != "agent" {
				return nil
			}
			cwd := rowStr(r, "cwd")
			if cwd == "" {
				cwd = env.Home
			}
			return &Node{Kind: "leaf", Cwd: cwd, Focused: rowInt(r, "is_focused") == 1}
		}
		kids := children[rowInt(r, "id")]
		sort.Slice(kids, func(i, j int) bool { return rowInt(kids[i], "id") < rowInt(kids[j], "id") })
		var ns []*Node
		var ws []float64
		for _, k := range kids {
			if n := build(k); n != nil {
				ns = append(ns, n)
				w := rowFloat(k, "flex")
				if w <= 0 {
					w = 1
				}
				ws = append(ws, w)
			}
		}
		dir := "col"
		if rowInt(r, "horizontal") == 1 {
			dir = "row"
		}
		return binaryTree(ns, ws, dir)
	}
	return build(root)
}
