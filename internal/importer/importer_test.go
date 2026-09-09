package importer

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func fixtureEnv(t *testing.T, name string) Env {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return Env{
		Home:       root,
		ConfigHome: filepath.Join(root, "config"),
		DataHome:   filepath.Join(root, "data"),
		StateHome:  filepath.Join(root, "state"),
		GOOS:       "linux",
		ThemesDir:  t.TempDir(),
	}
}

func itemKeys(items []Item) map[string]string {
	m := map[string]string{}
	for _, it := range items {
		m[it.Key] = it.Detail
	}
	return m
}

func TestChord(t *testing.T) {
	cases := map[string]string{
		"shift-left":       "shift+left",
		"ctrl-shift-t":     "ctrl+shift+t",
		"Ctrl+Shift+Enter": "ctrl+shift+enter",
		"super+left":       "cmd+left",
		"ctrl-=":           "ctrl+plus",
		"ctrl+equal":       "ctrl+plus",
		"Control+Plus":     "ctrl+plus",
		"alt+1":            "alt+1",
		"shift+ctrl+a":     "ctrl+shift+a",
	}
	for in, want := range cases {
		if got := chord(in); got != want {
			t.Errorf("chord(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWarp(t *testing.T) {
	env := fixtureEnv(t, "warp")
	src := findSource(t, Detect(env), "warp")
	items := itemKeys(src.Items)
	if items["theme"] != "Liquid Glass Custom Dark" {
		t.Errorf("theme item: %q", items["theme"])
	}
	if items["font"] != "JetBrains Mono, 15pt" {
		t.Errorf("font item: %q", items["font"])
	}
	if items["opacity"] != "70%" {
		t.Errorf("opacity item: %q", items["opacity"])
	}
	if items["wallpaper"] != "black.png" {
		t.Errorf("wallpaper item: %q", items["wallpaper"])
	}
	if items["keys"] != "4 mapped" {
		t.Errorf("keys item: %q", items["keys"])
	}

	res, err := Apply(env, "warp", []string{"theme", "font", "opacity", "keys"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ThemeID != "liquid-glass-custom-dark" {
		t.Errorf("theme id %q", res.ThemeID)
	}
	if _, err := os.Stat(filepath.Join(env.ThemesDir, "liquid-glass-custom-dark.toml")); err != nil {
		t.Error("theme file not written:", err)
	}
	want := map[string]any{
		"general.theme":      "liquid-glass-custom-dark",
		"terminal.font":      "JetBrains Mono",
		"terminal.font_size": 15,
		"terminal.ligatures": true,
		"editor.font":        "JetBrains Mono",
		"background.mode":    "translucent",
		"background.opacity": 0.7,
		"keys.focus_left":    "shift+left",
		"keys.focus_right":   "shift+right",
		"keys.new_tab":       "ctrl+shift+t",
		"keys.font_bigger":   "ctrl+plus",
	}
	for k, v := range want {
		if res.Settings[k] != v {
			t.Errorf("settings[%s] = %#v, want %#v", k, res.Settings[k], v)
		}
	}
	if _, ok := res.Settings["keys.agent:unknown_action"]; ok {
		t.Error("unknown Warp action leaked into keys")
	}

	// Wallpaper: Warp's image opacity 60% becomes dim 0.4; gradient background uses the bottom colour.
	res, err = Apply(env, "warp", []string{"wallpaper", "theme"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Settings["background.mode"] != "wallpaper" || res.Settings["background.dim"] != 0.4 {
		t.Errorf("wallpaper settings: %#v", res.Settings)
	}
	data, _ := os.ReadFile(filepath.Join(env.ThemesDir, "liquid-glass-custom-dark.toml"))
	if !contains(string(data), `background = "#0f111a"`) || !contains(string(data), `accent = "#0f62fe"`) {
		t.Errorf("theme content:\n%s", data)
	}
}

func TestWarpTabs(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	env := fixtureEnv(t, "warp")
	env.StateHome = t.TempDir()
	env.SQLite = sqliteCLI
	db := filepath.Join(env.StateHome, "warp-terminal", "warp.sqlite")
	_ = os.MkdirAll(filepath.Dir(db), 0o755)
	schema := `
CREATE TABLE windows (id INTEGER PRIMARY KEY, active_tab_index INTEGER);
CREATE TABLE tab_groups (id INTEGER PRIMARY KEY, window_id INTEGER, name TEXT, color TEXT, collapsed BOOLEAN, pinned BOOLEAN);
CREATE TABLE tabs (id INTEGER PRIMARY KEY, window_id INTEGER, custom_title TEXT, color TEXT, tab_group_id INTEGER, pinned BOOLEAN);
CREATE TABLE pane_nodes (id INTEGER PRIMARY KEY, tab_id INTEGER, parent_pane_node_id INTEGER, flex FLOAT, is_leaf BOOLEAN);
CREATE TABLE pane_branches (id INTEGER PRIMARY KEY, pane_node_id INTEGER, horizontal BOOLEAN);
CREATE TABLE pane_leaves (pane_node_id INTEGER, kind TEXT, is_focused BOOLEAN);
CREATE TABLE terminal_panes (id INTEGER PRIMARY KEY, kind TEXT, cwd TEXT);
INSERT INTO windows VALUES (1, 0);
INSERT INTO tab_groups VALUES (1, 1, 'Work', 'Blue', 0, 0);
INSERT INTO tabs VALUES (1, 1, 'Jacky & GH.net', NULL, 1, 0), (2, 1, NULL, 'Red', NULL, 0);
-- tab 1: root row split with three panes weighted 1, 2, 1
INSERT INTO pane_nodes VALUES (1, 1, NULL, NULL, 0), (2, 1, 1, 1.0, 1), (3, 1, 1, 2.0, 1), (4, 1, 1, 1.0, 1);
INSERT INTO pane_branches VALUES (1, 1, 1);
INSERT INTO pane_leaves VALUES (2, 'terminal', 0), (3, 'terminal', 1), (4, 'terminal', 0);
INSERT INTO terminal_panes VALUES (2, 'terminal', '/home/a'), (3, 'terminal', '/home/b'), (4, 'terminal', NULL);
-- tab 2: single pane
INSERT INTO pane_nodes VALUES (5, 2, NULL, NULL, 1);
INSERT INTO pane_leaves VALUES (5, 'terminal', 1);
INSERT INTO terminal_panes VALUES (5, 'terminal', '/home/c');
`
	cmd := exec.Command("sqlite3", db, schema)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v: %s", err, out)
	}
	src := findSource(t, Detect(env), "warp")
	if d := itemKeys(src.Items)["tabs"]; d != "2 tabs, 4 panes: Jacky & GH.net" {
		t.Errorf("tabs item: %q", d)
	}
	res, err := Apply(env, "warp", []string{"tabs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tabs) != 2 {
		t.Fatalf("tabs: %d", len(res.Tabs))
	}
	t1 := res.Tabs[0]
	if t1.Title != "Jacky & GH.net" || t1.Group != "Work" || t1.Color != "blue" {
		t.Errorf("tab 1 meta: %+v", t1)
	}
	root := t1.Root
	if root.Kind != "split" || root.Dir != "row" || root.Ratio != 0.25 {
		t.Fatalf("tab 1 root: %+v", root)
	}
	if root.A.Cwd != "/home/a" || root.B.Kind != "split" || root.B.Ratio != 0.67 || !root.B.A.Focused {
		t.Errorf("tab 1 tree: %+v / %+v", root.A, root.B)
	}
	if root.B.B.Cwd != env.Home {
		t.Errorf("empty cwd should fall back to home, got %q", root.B.B.Cwd)
	}
	t2 := res.Tabs[1]
	if t2.Title != "" || t2.Color != "red" || t2.Root.Kind != "leaf" || t2.Root.Cwd != "/home/c" {
		t.Errorf("tab 2: %+v root %+v", t2, t2.Root)
	}
}

func TestGhostty(t *testing.T) {
	env := fixtureEnv(t, "ghostty")
	src := findSource(t, Detect(env), "ghostty")
	items := itemKeys(src.Items)
	if items["theme"] != "mytheme" || items["font"] != "JetBrainsMono Nerd Font, 13pt, no ligatures" || items["opacity"] != "90%" || items["keys"] != "6 mapped" {
		t.Errorf("items: %#v", items)
	}
	res, err := Apply(env, "ghostty", []string{"theme", "font", "opacity", "keys"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"general.theme":      "mytheme",
		"terminal.font":      "JetBrainsMono Nerd Font",
		"terminal.font_size": 13,
		"terminal.ligatures": false,
		"background.opacity": 0.9,
		"keys.split_right":   "ctrl+shift+enter",
		"keys.split_down":    "ctrl+shift+d",
		"keys.focus_left":    "cmd+left",
		"keys.new_tab":       "ctrl+shift+t",
		"keys.tab_3":         "ctrl+3",
		"keys.font_bigger":   "ctrl+plus",
	}
	for k, v := range want {
		if res.Settings[k] != v {
			t.Errorf("settings[%s] = %#v, want %#v", k, res.Settings[k], v)
		}
	}
}

func TestAlacritty(t *testing.T) {
	env := fixtureEnv(t, "alacritty")
	src := findSource(t, Detect(env), "alacritty")
	items := itemKeys(src.Items)
	if items["theme"] != "nord" || items["font"] != "Fira Code, 12.5pt" || items["opacity"] != "80%" || items["keys"] != "2 mapped" {
		t.Errorf("items: %#v", items)
	}
	res, err := Apply(env, "alacritty", []string{"theme", "font", "keys"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ThemeID != "nord" || res.Settings["terminal.font_size"] != 13 || res.Settings["keys.new_tab"] != "ctrl+shift+t" || res.Settings["keys.font_bigger"] != "ctrl+plus" {
		t.Errorf("result: %+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(env.ThemesDir, "nord.toml"))
	if !contains(string(data), `background = "#2e3440"`) {
		t.Errorf("theme content:\n%s", data)
	}
}

func TestKitty(t *testing.T) {
	env := fixtureEnv(t, "kitty")
	src := findSource(t, Detect(env), "kitty")
	items := itemKeys(src.Items)
	if items["theme"] == "" || items["font"] != "Iosevka, 11pt, no ligatures" || items["opacity"] != "85%" || items["keys"] != "6 mapped" {
		t.Errorf("items: %#v", items)
	}
	res, err := Apply(env, "kitty", []string{"theme", "font", "opacity", "keys"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ThemeID != "gruvbox-dark" {
		t.Errorf("theme id %q", res.ThemeID)
	}
	want := map[string]any{
		"terminal.font":      "Iosevka",
		"terminal.font_size": 11,
		"terminal.ligatures": false,
		"background.mode":    "translucent",
		"background.opacity": 0.85,
		"keys.split_right":   "ctrl+shift+enter",
		"keys.split_down":    "ctrl+shift+minus",
		"keys.new_tab":       "ctrl+shift+t",
		"keys.focus_left":    "ctrl+alt+h",
		"keys.font_bigger":   "ctrl+shift+plus",
		"keys.font_reset":    "ctrl+shift+backspace",
	}
	for k, v := range want {
		if res.Settings[k] != v {
			t.Errorf("settings[%s] = %#v, want %#v", k, res.Settings[k], v)
		}
	}
	data, _ := os.ReadFile(filepath.Join(env.ThemesDir, "gruvbox-dark.toml"))
	if !contains(string(data), `red = "#cc241d"`) || !contains(string(data), `bright_white = "#ebdbb2"`) {
		t.Errorf("theme content:\n%s", data)
	}
}

func TestDetectNothing(t *testing.T) {
	env := fixtureEnv(t, "empty")
	if got := Detect(env); len(got) != 0 {
		t.Errorf("expected no sources, got %+v", got)
	}
	if _, err := Apply(env, "nope", nil); err == nil {
		t.Error("unknown source should fail")
	}
}

func findSource(t *testing.T, sources []Source, id string) Source {
	t.Helper()
	for _, s := range sources {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("source %q not detected in %+v", id, sources)
	return Source{}
}

func contains(s, sub string) bool { return len(sub) == 0 || len(s) >= len(sub) && indexOf(s, sub) >= 0 }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
