// Package theme loads colour themes (built-in or from the user's themes dir)
// and turns them into what the frontend needs: xterm.js colours and CSS variables.
package theme

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed themes/*.toml
var builtin embed.FS

// Theme is the on-disk TOML shape.
type Theme struct {
	Name       string `toml:"name" json:"name"`
	CodeScheme string `toml:"code_scheme" json:"code_scheme"`
	Colors     Colors `toml:"colors" json:"colors"`
	UI         UI     `toml:"ui" json:"ui"`
}

type Colors struct {
	Background          string `toml:"background" json:"background"`
	Foreground          string `toml:"foreground" json:"foreground"`
	Cursor              string `toml:"cursor" json:"cursor"`
	CursorText          string `toml:"cursor_text" json:"cursor_text"`
	SelectionBackground string `toml:"selection_background" json:"selection_background"`
	SelectionForeground string `toml:"selection_foreground" json:"selection_foreground"`
	Black               string `toml:"black" json:"black"`
	Red                 string `toml:"red" json:"red"`
	Green               string `toml:"green" json:"green"`
	Yellow              string `toml:"yellow" json:"yellow"`
	Blue                string `toml:"blue" json:"blue"`
	Magenta             string `toml:"magenta" json:"magenta"`
	Cyan                string `toml:"cyan" json:"cyan"`
	White               string `toml:"white" json:"white"`
	BrightBlack         string `toml:"bright_black" json:"bright_black"`
	BrightRed           string `toml:"bright_red" json:"bright_red"`
	BrightGreen         string `toml:"bright_green" json:"bright_green"`
	BrightYellow        string `toml:"bright_yellow" json:"bright_yellow"`
	BrightBlue          string `toml:"bright_blue" json:"bright_blue"`
	BrightMagenta       string `toml:"bright_magenta" json:"bright_magenta"`
	BrightCyan          string `toml:"bright_cyan" json:"bright_cyan"`
	BrightWhite         string `toml:"bright_white" json:"bright_white"`
}

type UI struct {
	Accent        string `toml:"accent" json:"accent"`
	TabBar        string `toml:"tab_bar" json:"tab_bar"`
	TabActive     string `toml:"tab_active" json:"tab_active"`
	TabInactiveFg string `toml:"tab_inactive_fg" json:"tab_inactive_fg"`
	Border        string `toml:"border" json:"border"`
	Sidebar       string `toml:"sidebar" json:"sidebar"`
}

// Resolved is what the frontend consumes.
type Resolved struct {
	ID    string `json:"id"`
	Theme Theme  `json:"theme"`
	// XTerm maps xterm.js ITheme keys to colours.
	XTerm map[string]string `json:"xterm"`
	// CSSVars maps CSS custom property names (without --) to values.
	CSSVars map[string]string `json:"css_vars"`
}

var hexRe = regexp.MustCompile(`^#([0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// Load resolves a theme id: a file in userDir wins over a built-in of the same name.
func Load(id string, userDir string) (Resolved, error) {
	id = strings.TrimSuffix(id, ".toml")
	var data []byte
	var err error
	if userDir != "" {
		data, err = os.ReadFile(filepath.Join(userDir, id+".toml"))
	}
	if userDir == "" || err != nil {
		data, err = builtin.ReadFile("themes/" + id + ".toml")
		if err != nil {
			return Resolved{}, fmt.Errorf("theme %q not found", id)
		}
	}
	return Parse(id, data)
}

// Parse decodes and validates a theme.
func Parse(id string, data []byte) (Resolved, error) {
	var t Theme
	if _, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&t); err != nil {
		return Resolved{}, fmt.Errorf("theme %s: %w", id, err)
	}
	if t.Name == "" {
		t.Name = id
	}
	fillDefaults(&t)
	if err := validate(t); err != nil {
		return Resolved{}, fmt.Errorf("theme %s: %w", id, err)
	}
	return Resolved{ID: id, Theme: t, XTerm: xtermColors(t.Colors), CSSVars: cssVars(t)}, nil
}

// List returns the ids of all available themes (user themes first, deduplicated).
func List(userDir string) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(name string) {
		id := strings.TrimSuffix(name, ".toml")
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if userDir != "" {
		if entries, err := os.ReadDir(userDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
					add(e.Name())
				}
			}
		}
	}
	entries, _ := fs.ReadDir(builtin, "themes")
	for _, e := range entries {
		add(e.Name())
	}
	sort.Strings(ids)
	return ids
}

func fillDefaults(t *Theme) {
	c := &t.Colors
	def := func(dst *string, v string) {
		if *dst == "" {
			*dst = v
		}
	}
	def(&c.BrightBlack, c.Black)
	def(&c.BrightRed, c.Red)
	def(&c.BrightGreen, c.Green)
	def(&c.BrightYellow, c.Yellow)
	def(&c.BrightBlue, c.Blue)
	def(&c.BrightMagenta, c.Magenta)
	def(&c.BrightCyan, c.Cyan)
	def(&c.BrightWhite, c.White)
	def(&c.Cursor, c.Foreground)
	def(&c.CursorText, c.Background)
	def(&c.SelectionBackground, c.BrightBlack)
	def(&c.SelectionForeground, c.Foreground)
	u := &t.UI
	def(&u.Accent, c.Blue)
	def(&u.TabBar, c.Background)
	def(&u.TabActive, c.Background)
	def(&u.TabInactiveFg, c.BrightBlack)
	def(&u.Border, c.BrightBlack)
	def(&u.Sidebar, u.TabBar)
	if t.CodeScheme == "" {
		t.CodeScheme = "dark"
	}
}

func validate(t Theme) error {
	for k, v := range xtermColors(t.Colors) {
		if !hexRe.MatchString(v) {
			return fmt.Errorf("colors.%s: %q is not a #rrggbb colour", k, v)
		}
	}
	for k, v := range map[string]string{"accent": t.UI.Accent, "tab_bar": t.UI.TabBar, "tab_active": t.UI.TabActive, "tab_inactive_fg": t.UI.TabInactiveFg, "border": t.UI.Border, "sidebar": t.UI.Sidebar} {
		if !hexRe.MatchString(v) {
			return fmt.Errorf("ui.%s: %q is not a #rrggbb colour", k, v)
		}
	}
	return nil
}

func xtermColors(c Colors) map[string]string {
	return map[string]string{
		"background":          c.Background,
		"foreground":          c.Foreground,
		"cursor":              c.Cursor,
		"cursorAccent":        c.CursorText,
		"selectionBackground": c.SelectionBackground,
		"selectionForeground": c.SelectionForeground,
		"black":               c.Black,
		"red":                 c.Red,
		"green":               c.Green,
		"yellow":              c.Yellow,
		"blue":                c.Blue,
		"magenta":             c.Magenta,
		"cyan":                c.Cyan,
		"white":               c.White,
		"brightBlack":         c.BrightBlack,
		"brightRed":           c.BrightRed,
		"brightGreen":         c.BrightGreen,
		"brightYellow":        c.BrightYellow,
		"brightBlue":          c.BrightBlue,
		"brightMagenta":       c.BrightMagenta,
		"brightCyan":          c.BrightCyan,
		"brightWhite":         c.BrightWhite,
	}
}

func cssVars(t Theme) map[string]string {
	c, u := t.Colors, t.UI
	return map[string]string{
		"bg":              c.Background,
		"fg":              c.Foreground,
		"accent":          u.Accent,
		"tabbar-bg":       u.TabBar,
		"tab-active-bg":   u.TabActive,
		"tab-inactive-fg": u.TabInactiveFg,
		"border":          u.Border,
		"sidebar-bg":      u.Sidebar,
		"selection":       c.SelectionBackground,
		"red":             c.Red,
		"green":           c.Green,
		"yellow":          c.Yellow,
		"blue":            c.Blue,
		"magenta":         c.Magenta,
		"cyan":            c.Cyan,
	}
}
