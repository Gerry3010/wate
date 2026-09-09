// Package importer reads the configuration of other terminal emulators (Warp, Ghostty,
// Alacritty, kitty) and translates the parts the user picks into wate settings, a wate
// theme and — where the source has them — tabs with split layouts and working directories.
package importer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Gerry3010/wate/internal/theme"
)

// Item is one importable aspect of a source (theme, font, …).
type Item struct {
	// Key is one of "theme", "font", "opacity", "wallpaper", "keys", "tabs".
	Key string `json:"key"`
	// Label is the human-readable name, Detail a short preview of the value.
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

// Source is a terminal whose config was found on this machine.
type Source struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Items []Item `json:"items"`
}

// Node is a pane layout: a leaf with a working directory or a split with two children.
type Node struct {
	Kind string `json:"kind"` // "leaf" | "split"
	Cwd  string `json:"cwd,omitempty"`
	// Dir is "row" (side by side) or "col" (stacked); Ratio the share of A.
	Dir   string  `json:"dir,omitempty"`
	Ratio float64 `json:"ratio,omitempty"`
	A     *Node   `json:"a,omitempty"`
	B     *Node   `json:"b,omitempty"`
	// Focused marks the leaf that had focus in the source.
	Focused bool `json:"focused,omitempty"`
	// History is pre-rendered terminal text (ANSI) replayed into the pane before its shell starts:
	// the source's recent commands and their output.
	History string `json:"history,omitempty"`
	// uuid identifies the leaf in the source (used to attach history).
	uuid string
}

// Tab is an imported tab: an optional custom title and colour plus its layout.
type Tab struct {
	Title string `json:"title"`
	Color string `json:"color"`
	Group string `json:"group,omitempty"`
	Root  *Node  `json:"root"`
}

// Result is what Apply produced.
type Result struct {
	// Settings are config.toml values (dotted keys) to write.
	Settings map[string]any `json:"settings"`
	// ThemeID is set when a theme was written to the user themes dir.
	ThemeID string `json:"theme_id"`
	Tabs    []Tab  `json:"tabs"`
	// Notes are human-readable remarks (what was skipped and why).
	Notes []string `json:"notes"`
}

// Env abstracts the machine so tests can point the importer at fixtures.
type Env struct {
	Home       string
	ConfigHome string // $XDG_CONFIG_HOME or ~/.config
	DataHome   string // $XDG_DATA_HOME or ~/.local/share
	StateHome  string // $XDG_STATE_HOME or ~/.local/state
	GOOS       string
	// ThemesDir is wate's user themes directory (where imported themes are written).
	ThemesDir string
	// SQLite runs a query against a database file and returns rows; nil when unavailable.
	SQLite func(db, query string) ([]map[string]any, error)
}

// SystemEnv describes the current machine.
func SystemEnv(themesDir string) Env {
	home, _ := os.UserHomeDir()
	pick := func(env, fallback string) string {
		if v := os.Getenv(env); v != "" {
			return v
		}
		return fallback
	}
	e := Env{
		Home:       home,
		ConfigHome: pick("XDG_CONFIG_HOME", filepath.Join(home, ".config")),
		DataHome:   pick("XDG_DATA_HOME", filepath.Join(home, ".local", "share")),
		StateHome:  pick("XDG_STATE_HOME", filepath.Join(home, ".local", "state")),
		GOOS:       runtime.GOOS,
		ThemesDir:  themesDir,
	}
	if _, err := exec.LookPath("sqlite3"); err == nil {
		e.SQLite = sqliteCLI
	}
	return e
}

// source is the internal contract every terminal adapter implements.
type source interface {
	id() string
	name() string
	// detect returns the config path and items, or "" when the terminal is not configured here.
	detect(env Env) (path string, items []Item)
	// apply collects the selected items into the result.
	apply(env Env, keys map[string]bool, res *Result) error
}

var sources = []source{warp{}, ghostty{}, alacritty{}, kitty{}}

// Detect lists every terminal with a readable config on this machine.
func Detect(env Env) []Source {
	var out []Source
	for _, s := range sources {
		path, items := s.detect(env)
		if path == "" || len(items) == 0 {
			continue
		}
		out = append(out, Source{ID: s.id(), Name: s.name(), Path: path, Items: items})
	}
	return out
}

// Apply imports the given item keys from one source. It writes theme files but not the
// config: Settings in the result are for the caller to persist.
func Apply(env Env, sourceID string, keys []string) (Result, error) {
	res := Result{Settings: map[string]any{}}
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	for _, s := range sources {
		if s.id() != sourceID {
			continue
		}
		if err := s.apply(env, want, &res); err != nil {
			return res, err
		}
		return res, nil
	}
	return res, fmt.Errorf("unknown import source %q", sourceID)
}

// ---- shared helpers -----------------------------------------------------

// writeTheme stores a theme in the user dir and records it in the result.
func writeTheme(env Env, t theme.Theme, name, origin string, res *Result) error {
	id, err := theme.WriteUser(t, name, env.ThemesDir, origin)
	if err != nil {
		return err
	}
	res.ThemeID = id
	res.Settings["general.theme"] = id
	return nil
}

// setFont writes the terminal and editor font settings.
func setFont(res *Result, family string, size float64, ligatures *bool) {
	if family != "" {
		res.Settings["terminal.font"] = family
		res.Settings["editor.font"] = family
	}
	if size > 0 {
		px := int(size + 0.5)
		res.Settings["terminal.font_size"] = px
		res.Settings["editor.font_size"] = px
	}
	if ligatures != nil {
		res.Settings["terminal.ligatures"] = *ligatures
		res.Settings["editor.ligatures"] = *ligatures
	}
}

// setOpacity switches to translucent mode with the given window alpha (0..1).
func setOpacity(res *Result, opacity float64) {
	if opacity <= 0 || opacity >= 1 {
		return
	}
	res.Settings["background.mode"] = "translucent"
	res.Settings["background.opacity"] = round2(opacity)
}

// setWallpaper switches to wallpaper mode. dim is 0..1, blur in px (negative = keep default).
func setWallpaper(res *Result, path string, dim float64, blur int) {
	res.Settings["background.mode"] = "wallpaper"
	res.Settings["background.wallpaper"] = path
	if dim >= 0 && dim <= 1 {
		res.Settings["background.dim"] = round2(dim)
	}
	if blur >= 0 {
		res.Settings["background.blur"] = blur
	}
}

// setKeys writes wate key bindings (action → chord).
func setKeys(res *Result, keys map[string]string) {
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		res.Settings["keys."+k] = keys[k]
	}
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

func fontDetail(family string, size float64, ligatures *bool) string {
	parts := []string{}
	if family != "" {
		parts = append(parts, family)
	}
	if size > 0 {
		parts = append(parts, fmt.Sprintf("%gpt", size))
	}
	if ligatures != nil && !*ligatures {
		parts = append(parts, "no ligatures")
	}
	return strings.Join(parts, ", ")
}

// chord converts a key spec like "ctrl-shift-t", "Ctrl+Shift+T" or "super+enter" into wate's form.
func chord(spec string) string {
	spec = strings.ToLower(strings.TrimSpace(spec))
	parts := strings.FieldsFunc(spec, func(r rune) bool { return r == '+' || r == '-' || r == ' ' })
	// A trailing "-" or "+" is the key itself.
	if strings.HasSuffix(spec, "-") || strings.HasSuffix(spec, "+") {
		parts = append(parts, spec[len(spec)-1:])
	}
	var mods []string
	key := ""
	for _, p := range parts {
		switch p {
		case "ctrl", "control":
			mods = append(mods, "ctrl")
		case "alt", "opt", "option":
			mods = append(mods, "alt")
		case "shift":
			mods = append(mods, "shift")
		case "cmd", "super", "meta", "win", "command":
			mods = append(mods, "cmd")
		case "":
		default:
			key = keyName(p)
		}
	}
	if key == "" {
		return ""
	}
	order := map[string]int{"ctrl": 0, "alt": 1, "shift": 2, "cmd": 3}
	sort.Slice(mods, func(i, j int) bool { return order[mods[i]] < order[mods[j]] })
	return strings.Join(append(mods, key), "+")
}

func keyName(k string) string {
	switch k {
	case "arrowleft", "leftarrow":
		return "left"
	case "arrowright", "rightarrow":
		return "right"
	case "arrowup", "uparrow":
		return "up"
	case "arrowdown", "downarrow":
		return "down"
	case "return":
		return "enter"
	case "esc":
		return "escape"
	case "=", "equal", "equals", "+":
		return "plus"
	case "-", "minus":
		return "minus"
	case ",":
		return "comma"
	case ".":
		return "period"
	case "space", "spacebar":
		return "space"
	}
	return k
}

// exists reports whether a file is present and readable.
func exists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// expand resolves "~" and relative paths against base.
func expand(env Env, p, base string) string {
	p = strings.Trim(strings.TrimSpace(p), `"'`)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(env.Home, p[2:])
	}
	if !filepath.IsAbs(p) && base != "" {
		p = filepath.Join(base, p)
	}
	return p
}

// binaryTree turns an n-ary list of weighted children into nested binary splits.
func binaryTree(children []*Node, weights []float64, dir string) *Node {
	if len(children) == 0 {
		return nil
	}
	if len(children) == 1 {
		return children[0]
	}
	total := 0.0
	for _, w := range weights {
		total += w
	}
	ratio := 0.5
	if total > 0 {
		ratio = weights[0] / total
	}
	return &Node{Kind: "split", Dir: dir, Ratio: round2(ratio), A: children[0], B: binaryTree(children[1:], weights[1:], dir)}
}

// countLeaves returns the number of panes in a layout.
func countLeaves(n *Node) int {
	if n == nil {
		return 0
	}
	if n.Kind == "leaf" {
		return 1
	}
	return countLeaves(n.A) + countLeaves(n.B)
}
