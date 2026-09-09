package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Gerry3010/wate/internal/theme"
)

// kitty imports kitty.conf (`key value` lines, `include` directives, `map` bindings).
type kitty struct{}

func (kitty) id() string   { return "kitty" }
func (kitty) name() string { return "kitty" }

func (kitty) configPaths(env Env) []string {
	paths := []string{filepath.Join(env.ConfigHome, "kitty", "kitty.conf")}
	if env.GOOS == "darwin" {
		paths = append(paths, filepath.Join(env.Home, "Library", "Preferences", "kitty", "kitty.conf"))
	}
	return paths
}

type kittyConfig struct {
	values map[string]string
	maps   [][2]string // key spec, action
	raw    []string    // colour lines in Ghostty-like "key = value" form
}

func (k kitty) load(env Env, path string, depth int, cfg *kittyConfig) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if name, ok := strings.CutPrefix(line, "## name:"); ok {
			cfg.values["##"] = strings.TrimSpace(name)
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "include", "globinclude":
			if depth < 3 {
				matches, _ := filepath.Glob(expand(env, val, filepath.Dir(path)))
				for _, m := range matches {
					k.load(env, m, depth+1, cfg)
				}
			}
		case "map":
			spec, action, _ := strings.Cut(val, " ")
			cfg.maps = append(cfg.maps, [2]string{strings.TrimSpace(spec), strings.TrimSpace(action)})
		default:
			cfg.values[key] = val
			if strings.HasPrefix(key, "color") {
				if n, err := strconv.Atoi(strings.TrimPrefix(key, "color")); err == nil && n < 16 {
					cfg.raw = append(cfg.raw, fmt.Sprintf("palette = %d=%s", n, val))
				}
			}
		}
	}
}

func (k kitty) find(env Env) (string, kittyConfig) {
	for _, p := range k.configPaths(env) {
		if exists(p) {
			cfg := kittyConfig{values: map[string]string{}}
			k.load(env, p, 0, &cfg)
			return p, cfg
		}
	}
	return "", kittyConfig{}
}

func (kitty) ligatures(cfg kittyConfig) *bool {
	switch cfg.values["disable_ligatures"] {
	case "always":
		off := false
		return &off
	case "never":
		on := true
		return &on
	}
	return nil
}

func (kitty) font(cfg kittyConfig) (string, float64) {
	family := cfg.values["font_family"]
	if strings.HasPrefix(family, "family=") {
		// kitty 0.34 syntax: family="JetBrains Mono" style=Regular
		family = strings.TrimPrefix(family, "family=")
		family, _, _ = strings.Cut(family, " style=")
		family = strings.Trim(family, `"`)
	}
	if family == "monospace" || family == "auto" {
		family = ""
	}
	size, _ := strconv.ParseFloat(cfg.values["font_size"], 64)
	return family, size
}

func (k kitty) themeName(cfg kittyConfig) string {
	// kitty writes "## name: X" into current-theme.conf; when absent use a generic id.
	if n := cfg.values["##"]; n != "" {
		return n
	}
	return "kitty"
}

func (k kitty) detect(env Env) (string, []Item) {
	path, cfg := k.find(env)
	if path == "" {
		return "", nil
	}
	var items []Item
	if cfg.values["background"] != "" && len(cfg.raw) > 0 {
		items = append(items, Item{Key: "theme", Label: "Theme", Detail: "colours from kitty.conf"})
	}
	family, size := k.font(cfg)
	if family != "" || size > 0 {
		items = append(items, Item{Key: "font", Label: "Font", Detail: fontDetail(family, size, k.ligatures(cfg))})
	}
	if op, err := strconv.ParseFloat(cfg.values["background_opacity"], 64); err == nil && op > 0 && op < 1 {
		items = append(items, Item{Key: "opacity", Label: "Window opacity", Detail: fmt.Sprintf("%g%%", op*100)})
	}
	if img := cfg.values["background_image"]; img != "" && img != "none" {
		if p := expand(env, img, filepath.Dir(path)); exists(p) {
			items = append(items, Item{Key: "wallpaper", Label: "Background image", Detail: filepath.Base(p)})
		}
	}
	if keys := k.keys(cfg); len(keys) > 0 {
		items = append(items, Item{Key: "keys", Label: "Key bindings", Detail: fmt.Sprintf("%d mapped", len(keys))})
	}
	return path, items
}

func (k kitty) apply(env Env, want map[string]bool, res *Result) error {
	path, cfg := k.find(env)
	if path == "" {
		return fmt.Errorf("kitty.conf not found")
	}
	if want["theme"] {
		lines := append([]string{}, cfg.raw...)
		for kk, wk := range map[string]string{"background": "background", "foreground": "foreground", "cursor": "cursor-color",
			"cursor_text_color": "cursor-text", "selection_background": "selection-background", "selection_foreground": "selection-foreground"} {
			if v := cfg.values[kk]; v != "" && v != "none" && v != "background" {
				lines = append(lines, wk+" = "+v)
			}
		}
		t, err := theme.ParseGhostty([]byte(strings.Join(lines, "\n")))
		if err != nil {
			return fmt.Errorf("kitty colours: %w", err)
		}
		name := k.themeName(cfg)
		t.Name = name
		if err := writeTheme(env, t, name, "kitty.conf", res); err != nil {
			return err
		}
	}
	if want["font"] {
		family, size := k.font(cfg)
		setFont(res, family, size, k.ligatures(cfg))
	}
	if want["opacity"] {
		op, _ := strconv.ParseFloat(cfg.values["background_opacity"], 64)
		setOpacity(res, op)
	}
	if want["wallpaper"] {
		img := expand(env, cfg.values["background_image"], filepath.Dir(path))
		tint, _ := strconv.ParseFloat(cfg.values["background_tint"], 64)
		setWallpaper(res, img, tint, -1)
	}
	if want["keys"] {
		setKeys(res, k.keys(cfg))
	}
	return nil
}

func (kitty) keys(cfg kittyConfig) map[string]string {
	kittyMod := cfg.values["kitty_mod"]
	if kittyMod == "" {
		kittyMod = "ctrl+shift"
	}
	out := map[string]string{}
	for _, m := range cfg.maps {
		spec, action := m[0], m[1]
		if strings.Contains(spec, ">") { // key sequences
			continue
		}
		spec = strings.ReplaceAll(spec, "kitty_mod", kittyMod)
		fields := strings.Fields(action)
		if len(fields) == 0 {
			continue
		}
		wate := ""
		switch fields[0] {
		case "new_tab", "next_tab", "copy_to_clipboard", "paste_from_clipboard":
			wate = map[string]string{"new_tab": "new_tab", "next_tab": "next_tab", "copy_to_clipboard": "copy", "paste_from_clipboard": "paste"}[fields[0]]
		case "previous_tab":
			wate = "prev_tab"
		case "close_window":
			wate = "close_pane"
		case "close_tab":
			wate = "close_tab"
		case "goto_tab":
			if len(fields) > 1 {
				if n, err := strconv.Atoi(fields[1]); err == nil && n >= 1 && n <= 9 {
					wate = "tab_" + fields[1]
				}
			}
		case "launch":
			for _, f := range fields[1:] {
				switch f {
				case "--location=vsplit":
					wate = "split_right"
				case "--location=hsplit":
					wate = "split_down"
				}
			}
		case "neighboring_window":
			if len(fields) > 1 {
				wate = map[string]string{"left": "focus_left", "right": "focus_right", "up": "focus_up", "down": "focus_down", "top": "focus_up", "bottom": "focus_down"}[fields[1]]
			}
		case "change_font_size":
			if len(fields) > 2 {
				switch {
				case strings.HasPrefix(fields[2], "+"):
					wate = "font_bigger"
				case strings.HasPrefix(fields[2], "-"):
					wate = "font_smaller"
				case fields[2] == "0":
					wate = "font_reset"
				}
			}
		case "show_scrollback", "launch_search", "search":
		}
		if wate == "" {
			continue
		}
		if c := chord(spec); c != "" {
			out[wate] = c
		}
	}
	return out
}
