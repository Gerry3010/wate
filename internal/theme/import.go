package theme

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// ImportFile converts a terminal colour scheme into a wate theme and writes it to userDir.
// Supported: wate's own TOML, Ghostty (`palette = N=#hex` lines) and Alacritty TOML —
// both exported for every scheme in github.com/mbadolato/iTerm2-Color-Schemes.
// Returns the new theme id.
func ImportFile(path, userDir string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var t Theme
	switch {
	case bytes.Contains(data, []byte("palette")) && bytes.Contains(data, []byte("=#")) || bytes.Contains(data, []byte("cursor-color")):
		t, err = ParseGhostty(data)
	case bytes.Contains(data, []byte("[colors.primary]")) || bytes.Contains(data, []byte("[colors.normal]")):
		t, err = ParseAlacritty(data)
	case bytes.Contains(data, []byte("[colors]")):
		var r Resolved
		r, err = Parse(name, data)
		t = r.Theme
	default:
		return "", fmt.Errorf("%s: unknown theme format (expected wate, Ghostty or Alacritty)", filepath.Base(path))
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if t.Name == "" {
		t.Name = name
	}
	return WriteUser(t, name, userDir, filepath.Base(path))
}

// WriteUser validates a theme, fills defaults and writes it as <userDir>/<slug(name)>.toml.
// origin is mentioned in the file header. Returns the theme id.
func WriteUser(t Theme, name, userDir, origin string) (string, error) {
	fillDefaults(&t)
	if err := validate(t); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	id := Slug(name)
	if id == "" {
		return "", fmt.Errorf("%s: cannot derive a theme id", name)
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# imported by wate from %s\n", origin)
	if err := toml.NewEncoder(&buf).Encode(t); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(userDir, id+".toml"), buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	return id, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns "Catppuccin Mocha" into "catppuccin-mocha".
func Slug(name string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

var ghosttyLine = regexp.MustCompile(`^\s*([a-z-]+)\s*=\s*(.*?)\s*$`)

// ParseGhostty reads Ghostty's `key = value` colour keys (theme files and config files alike).
func ParseGhostty(data []byte) (Theme, error) {
	var t Theme
	var pal [16]string
	for _, line := range strings.Split(string(data), "\n") {
		m := ghosttyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, val := m[1], NormHex(m[2])
		switch key {
		case "palette":
			idx, col, ok := strings.Cut(m[2], "=")
			n, err := strconv.Atoi(strings.TrimSpace(idx))
			if !ok || err != nil || n < 0 || n > 15 {
				continue
			}
			pal[n] = NormHex(col)
		case "background":
			t.Colors.Background = val
		case "foreground":
			t.Colors.Foreground = val
		case "cursor-color":
			t.Colors.Cursor = val
		case "cursor-text":
			t.Colors.CursorText = val
		case "selection-background":
			t.Colors.SelectionBackground = val
		case "selection-foreground":
			t.Colors.SelectionForeground = val
		}
	}
	AssignPalette(&t.Colors, pal)
	if t.Colors.Background == "" || t.Colors.Foreground == "" {
		return t, fmt.Errorf("ghostty theme without background/foreground")
	}
	return t, nil
}

type alacrittyTheme struct {
	Colors struct {
		Primary   struct{ Background, Foreground string } `toml:"primary"`
		Cursor    struct{ Cursor, Text string }           `toml:"cursor"`
		Selection struct{ Background, Text string }       `toml:"selection"`
		Normal    map[string]string                       `toml:"normal"`
		Bright    map[string]string                       `toml:"bright"`
	} `toml:"colors"`
}

// ParseAlacritty reads the [colors.*] tables of an Alacritty TOML file.
func ParseAlacritty(data []byte) (Theme, error) {
	var a alacrittyTheme
	if _, err := toml.Decode(string(data), &a); err != nil {
		return Theme{}, err
	}
	var t Theme
	c := &t.Colors
	c.Background = NormHex(a.Colors.Primary.Background)
	c.Foreground = NormHex(a.Colors.Primary.Foreground)
	c.Cursor = NormHex(a.Colors.Cursor.Cursor)
	c.CursorText = NormHex(a.Colors.Cursor.Text)
	c.SelectionBackground = NormHex(a.Colors.Selection.Background)
	c.SelectionForeground = NormHex(a.Colors.Selection.Text)
	var pal [16]string
	order := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}
	for i, n := range order {
		pal[i] = NormHex(a.Colors.Normal[n])
		pal[i+8] = NormHex(a.Colors.Bright[n])
	}
	AssignPalette(c, pal)
	if c.Background == "" || c.Foreground == "" {
		return t, fmt.Errorf("alacritty theme without colors.primary")
	}
	return t, nil
}

// AssignPalette copies the 16 ANSI colours (empty entries are skipped).
func AssignPalette(c *Colors, pal [16]string) {
	dst := []*string{&c.Black, &c.Red, &c.Green, &c.Yellow, &c.Blue, &c.Magenta, &c.Cyan, &c.White,
		&c.BrightBlack, &c.BrightRed, &c.BrightGreen, &c.BrightYellow, &c.BrightBlue, &c.BrightMagenta, &c.BrightCyan, &c.BrightWhite}
	for i, p := range dst {
		if pal[i] != "" {
			*p = pal[i]
		}
	}
}

// NormHex accepts "#rrggbb", "rrggbb", "0xrrggbb" and quoted forms.
func NormHex(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"'`)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "#")
	if len(s) != 6 {
		return ""
	}
	return "#" + strings.ToLower(s)
}
