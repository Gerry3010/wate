package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Gerry3010/wate/internal/theme"
)

// alacritty imports Alacritty's TOML config, following `import` / `[general] import` lists.
type alacritty struct{}

func (alacritty) id() string   { return "alacritty" }
func (alacritty) name() string { return "Alacritty" }

func (alacritty) configPaths(env Env) []string {
	return []string{
		filepath.Join(env.ConfigHome, "alacritty", "alacritty.toml"),
		filepath.Join(env.ConfigHome, "alacritty.toml"),
		filepath.Join(env.Home, ".alacritty.toml"),
	}
}

type alacrittyConfig struct {
	Import  []string `toml:"import"`
	General struct {
		Import []string `toml:"import"`
	} `toml:"general"`
	Font struct {
		Size   float64 `toml:"size"`
		Normal struct {
			Family string `toml:"family"`
		} `toml:"normal"`
	} `toml:"font"`
	Window struct {
		Opacity float64 `toml:"opacity"`
	} `toml:"window"`
	Keyboard struct {
		Bindings []struct {
			Key    string `toml:"key"`
			Mods   string `toml:"mods"`
			Action string `toml:"action"`
		} `toml:"bindings"`
	} `toml:"keyboard"`
}

// loaded is the merged view: main config plus the colour tables of every imported file.
type alacrittyLoaded struct {
	path   string
	cfg    alacrittyConfig
	colors []byte // TOML text of the file that defines [colors.primary], for the theme parser
	theme  string // name derived from that file
}

func (a alacritty) find(env Env) (alacrittyLoaded, bool) {
	for _, p := range a.configPaths(env) {
		if !exists(p) {
			continue
		}
		var l alacrittyLoaded
		l.path = p
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if _, err := toml.Decode(string(data), &l.cfg); err != nil {
			continue
		}
		if strings.Contains(string(data), "[colors.primary]") || strings.Contains(string(data), "[colors.normal]") {
			l.colors, l.theme = data, "alacritty"
		}
		for _, imp := range append(append([]string{}, l.cfg.Import...), l.cfg.General.Import...) {
			ip := expand(env, imp, filepath.Dir(p))
			d, err := os.ReadFile(ip)
			if err != nil {
				continue
			}
			var sub alacrittyConfig
			_, _ = toml.Decode(string(d), &sub)
			if l.cfg.Font.Normal.Family == "" {
				l.cfg.Font.Normal.Family = sub.Font.Normal.Family
			}
			if l.cfg.Font.Size == 0 {
				l.cfg.Font.Size = sub.Font.Size
			}
			if strings.Contains(string(d), "[colors.primary]") || strings.Contains(string(d), "[colors.normal]") {
				l.colors, l.theme = d, strings.TrimSuffix(filepath.Base(ip), filepath.Ext(ip))
			}
		}
		return l, true
	}
	return alacrittyLoaded{}, false
}

func (a alacritty) detect(env Env) (string, []Item) {
	l, ok := a.find(env)
	if !ok {
		return "", nil
	}
	var items []Item
	if l.colors != nil {
		items = append(items, Item{Key: "theme", Label: "Theme", Detail: l.theme})
	}
	if l.cfg.Font.Normal.Family != "" || l.cfg.Font.Size > 0 {
		items = append(items, Item{Key: "font", Label: "Font", Detail: fontDetail(l.cfg.Font.Normal.Family, l.cfg.Font.Size, nil)})
	}
	if op := l.cfg.Window.Opacity; op > 0 && op < 1 {
		items = append(items, Item{Key: "opacity", Label: "Window opacity", Detail: fmt.Sprintf("%g%%", op*100)})
	}
	if keys := a.keys(l.cfg); len(keys) > 0 {
		items = append(items, Item{Key: "keys", Label: "Key bindings", Detail: fmt.Sprintf("%d mapped", len(keys))})
	}
	return l.path, items
}

func (a alacritty) apply(env Env, want map[string]bool, res *Result) error {
	l, ok := a.find(env)
	if !ok {
		return fmt.Errorf("alacritty config not found")
	}
	if want["theme"] {
		if l.colors == nil {
			return fmt.Errorf("alacritty config defines no colours")
		}
		t, err := theme.ParseAlacritty(l.colors)
		if err != nil {
			return err
		}
		t.Name = l.theme
		if err := writeTheme(env, t, l.theme, "Alacritty "+l.theme, res); err != nil {
			return err
		}
	}
	if want["font"] {
		setFont(res, l.cfg.Font.Normal.Family, l.cfg.Font.Size, nil)
	}
	if want["opacity"] {
		setOpacity(res, l.cfg.Window.Opacity)
	}
	if want["keys"] {
		setKeys(res, a.keys(l.cfg))
	}
	return nil
}

var alacrittyActions = map[string]string{
	"CreateNewTab":      "new_tab",
	"CreateNewWindow":   "new_tab",
	"SpawnNewInstance":  "new_tab",
	"SelectNextTab":     "next_tab",
	"SelectPreviousTab": "prev_tab",
	"Copy":              "copy",
	"Paste":             "paste",
	"SearchForward":     "search",
	"IncreaseFontSize":  "font_bigger",
	"DecreaseFontSize":  "font_smaller",
	"ResetFontSize":     "font_reset",
}

func (alacritty) keys(cfg alacrittyConfig) map[string]string {
	out := map[string]string{}
	for _, b := range cfg.Keyboard.Bindings {
		action, ok := alacrittyActions[b.Action]
		if !ok {
			if strings.HasPrefix(b.Action, "SelectTab") && len(b.Action) == len("SelectTab")+1 {
				action, ok = "tab_"+b.Action[len("SelectTab"):], true
			}
		}
		if !ok || b.Key == "" {
			continue
		}
		spec := strings.ReplaceAll(b.Mods, "|", "+")
		if spec != "" {
			spec += "+"
		}
		if c := chord(spec + b.Key); c != "" {
			out[action] = c
		}
	}
	return out
}
