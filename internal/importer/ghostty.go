package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Gerry3010/wate/internal/theme"
)

// ghostty imports Ghostty's `key = value` config (plus one level of config-file includes).
type ghostty struct{}

func (ghostty) id() string   { return "ghostty" }
func (ghostty) name() string { return "Ghostty" }

func (ghostty) configPaths(env Env) []string {
	paths := []string{filepath.Join(env.ConfigHome, "ghostty", "config")}
	if env.GOOS == "darwin" {
		paths = append(paths, filepath.Join(env.Home, "Library", "Application Support", "com.mitchellh.ghostty", "config"))
	}
	return paths
}

func (ghostty) themeDirs(env Env) []string {
	dirs := []string{filepath.Join(env.ConfigHome, "ghostty", "themes"), "/usr/share/ghostty/themes"}
	if env.GOOS == "darwin" {
		dirs = append(dirs, "/Applications/Ghostty.app/Contents/Resources/ghostty/themes")
	}
	return dirs
}

var ghosttyKV = regexp.MustCompile(`^\s*([a-z0-9-]+)\s*=\s*(.*?)\s*$`)

// ghosttyConfig is the flattened config: repeated keys (keybind, palette, config-file) are kept in order.
type ghosttyConfig struct {
	values map[string]string
	multi  map[string][]string
	raw    []string // every "key = value" line, for the colour parser
}

func (g ghostty) load(env Env, path string, depth int) ghosttyConfig {
	cfg := ghosttyConfig{values: map[string]string{}, multi: map[string][]string{}}
	g.loadInto(env, path, depth, &cfg)
	return cfg
}

func (g ghostty) loadInto(env Env, path string, depth int, cfg *ghosttyConfig) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "#"); i >= 0 && !strings.Contains(line[:i], "=") {
			line = line[:i]
		}
		m := ghosttyKV.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, val := m[1], strings.Trim(m[2], `"`)
		cfg.raw = append(cfg.raw, line)
		switch key {
		case "keybind", "palette", "font-feature", "config-file":
			cfg.multi[key] = append(cfg.multi[key], val)
			if key == "config-file" && depth < 3 {
				inc := strings.TrimPrefix(val, "?")
				g.loadInto(env, expand(env, inc, filepath.Dir(path)), depth+1, cfg)
			}
		default:
			cfg.values[key] = val
		}
	}
}

func (g ghostty) find(env Env) (string, ghosttyConfig) {
	for _, p := range g.configPaths(env) {
		if exists(p) {
			return p, g.load(env, p, 0)
		}
	}
	return "", ghosttyConfig{}
}

// themeFile resolves `theme = name` (or "dark:name,light:name") to a file on disk.
func (g ghostty) themeFile(env Env, cfg ghosttyConfig, base string) (name, path string) {
	name = cfg.values["theme"]
	if name == "" {
		return "", ""
	}
	if strings.Contains(name, ":") {
		picked := ""
		for _, part := range strings.Split(name, ",") {
			k, v, _ := strings.Cut(strings.TrimSpace(part), ":")
			if k == "dark" || picked == "" {
				picked = strings.TrimSpace(v)
			}
		}
		name = picked
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "~") {
		return filepath.Base(name), expand(env, name, base)
	}
	for _, d := range g.themeDirs(env) {
		if p := filepath.Join(d, name); exists(p) {
			return name, p
		}
	}
	return name, ""
}

func (g ghostty) ligatures(cfg ghosttyConfig) *bool {
	for _, f := range cfg.multi["font-feature"] {
		for _, feat := range strings.Split(f, ",") {
			feat = strings.TrimSpace(feat)
			if feat == "-calt" || feat == "-liga" || feat == "calt=0" || feat == "liga=0" {
				off := false
				return &off
			}
		}
	}
	return nil
}

func (g ghostty) detect(env Env) (string, []Item) {
	path, cfg := g.find(env)
	if path == "" {
		return "", nil
	}
	var items []Item
	name, file := g.themeFile(env, cfg, filepath.Dir(path))
	switch {
	case file != "":
		items = append(items, Item{Key: "theme", Label: "Theme", Detail: name})
	case name != "":
		items = append(items, Item{Key: "theme", Label: "Theme", Detail: name + " (theme file not found)"})
	case cfg.values["background"] != "" && len(cfg.multi["palette"]) > 0:
		items = append(items, Item{Key: "theme", Label: "Theme", Detail: "colours from config"})
	}
	size, _ := strconv.ParseFloat(cfg.values["font-size"], 64)
	if cfg.values["font-family"] != "" || size > 0 {
		items = append(items, Item{Key: "font", Label: "Font", Detail: fontDetail(cfg.values["font-family"], size, g.ligatures(cfg))})
	}
	if op, err := strconv.ParseFloat(cfg.values["background-opacity"], 64); err == nil && op > 0 && op < 1 {
		items = append(items, Item{Key: "opacity", Label: "Window opacity", Detail: fmt.Sprintf("%g%%", op*100)})
	}
	if img := cfg.values["background-image"]; img != "" {
		p := expand(env, img, filepath.Dir(path))
		if exists(p) {
			items = append(items, Item{Key: "wallpaper", Label: "Background image", Detail: filepath.Base(p)})
		}
	}
	if keys := g.keys(cfg); len(keys) > 0 {
		items = append(items, Item{Key: "keys", Label: "Key bindings", Detail: fmt.Sprintf("%d mapped", len(keys))})
	}
	return path, items
}

func (g ghostty) apply(env Env, want map[string]bool, res *Result) error {
	path, cfg := g.find(env)
	if path == "" {
		return fmt.Errorf("ghostty config not found")
	}
	if want["theme"] {
		name, file := g.themeFile(env, cfg, filepath.Dir(path))
		var t theme.Theme
		var err error
		origin := "Ghostty config"
		switch {
		case file != "":
			var data []byte
			data, err = os.ReadFile(file)
			if err == nil {
				t, err = theme.ParseGhostty(data)
				origin = "Ghostty theme " + name
			}
		case name != "":
			err = fmt.Errorf("theme %q not found in Ghostty's theme directories", name)
		default:
			t, err = theme.ParseGhostty([]byte(strings.Join(cfg.raw, "\n")))
			name = "ghostty"
		}
		if err != nil {
			return err
		}
		t.Name = name
		if err := writeTheme(env, t, name, origin, res); err != nil {
			return err
		}
	}
	if want["font"] {
		size, _ := strconv.ParseFloat(cfg.values["font-size"], 64)
		setFont(res, cfg.values["font-family"], size, g.ligatures(cfg))
	}
	if want["opacity"] {
		op, _ := strconv.ParseFloat(cfg.values["background-opacity"], 64)
		setOpacity(res, op)
	}
	if want["wallpaper"] {
		img := expand(env, cfg.values["background-image"], filepath.Dir(path))
		op := 1.0
		if v, err := strconv.ParseFloat(cfg.values["background-image-opacity"], 64); err == nil && v > 0 {
			op = v
		}
		setWallpaper(res, img, 1-op, -1)
	}
	if want["keys"] {
		setKeys(res, g.keys(cfg))
	}
	return nil
}

// ghosttyActions maps Ghostty keybind actions to wate actions.
var ghosttyActions = map[string]string{
	"new_split:right":      "split_right",
	"new_split:down":       "split_down",
	"goto_split:left":      "focus_left",
	"goto_split:right":     "focus_right",
	"goto_split:up":        "focus_up",
	"goto_split:down":      "focus_down",
	"goto_split:previous":  "focus_left",
	"goto_split:next":      "focus_right",
	"resize_split:left":    "resize_left",
	"resize_split:right":   "resize_right",
	"resize_split:up":      "resize_up",
	"resize_split:down":    "resize_down",
	"close_surface":        "close_pane",
	"close_tab":            "close_tab",
	"new_tab":              "new_tab",
	"next_tab":             "next_tab",
	"previous_tab":         "prev_tab",
	"copy_to_clipboard":    "copy",
	"paste_from_clipboard": "paste",
	"increase_font_size":   "font_bigger",
	"decrease_font_size":   "font_smaller",
	"reset_font_size":      "font_reset",
	"open_config":          "open_settings",
}

func (ghostty) keys(cfg ghosttyConfig) map[string]string {
	out := map[string]string{}
	for _, kb := range cfg.multi["keybind"] {
		spec, action, ok := strings.Cut(kb, "=")
		if !ok {
			continue
		}
		spec = strings.TrimSpace(spec)
		for _, prefix := range []string{"global:", "all:", "unconsumed:", "performable:"} {
			spec = strings.TrimPrefix(spec, prefix)
		}
		if strings.Contains(spec, ">") { // key sequences have no wate equivalent
			continue
		}
		action = strings.TrimSpace(action)
		wateAction, ok := ghosttyActions[action]
		if !ok {
			// resize_split:left,10 / increase_font_size:1 / goto_tab:3
			base, arg, _ := strings.Cut(action, ":")
			first, _, _ := strings.Cut(arg, ",")
			if base == "goto_tab" {
				if n, err := strconv.Atoi(first); err == nil && n >= 1 && n <= 9 {
					wateAction, ok = "tab_"+strconv.Itoa(n), true
				}
			} else {
				wateAction, ok = ghosttyActions[base+":"+first]
				if !ok {
					wateAction, ok = ghosttyActions[base]
				}
			}
		}
		if !ok {
			continue
		}
		if c := chord(spec); c != "" {
			out[wateAction] = c
		}
	}
	return out
}
