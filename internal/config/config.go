// Package config loads wate's TOML configuration with embedded defaults.
package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed defaults.toml
var defaultsTOML []byte

// Config is the full user configuration.
type Config struct {
	General    General    `toml:"general" json:"general"`
	Terminal   Terminal   `toml:"terminal" json:"terminal"`
	Background Background `toml:"background" json:"background"`
	// KeysRaw is the [keys] table as decoded: string bindings plus an optional
	// [keys.darwin] sub-table with macOS overrides. Use Keys after Load/Parse.
	KeysRaw map[string]any `toml:"keys" json:"-"`
	// Keys is the effective action → chord map for this platform.
	Keys   map[string]string `toml:"-" json:"keys"`
	Claude Claude            `toml:"claude" json:"claude"`
	Editor Editor            `toml:"editor" json:"editor"`
}

type General struct {
	Shell     string   `toml:"shell" json:"shell"`
	ShellArgs []string `toml:"shell_args" json:"shell_args"`
	Theme     string   `toml:"theme" json:"theme"`
	// ShellIntegration auto-loads wate's zsh snippet (OSC 7 cwd + OSC 133 prompt marks).
	ShellIntegration bool `toml:"shell_integration" json:"shell_integration"`
	// RestoreSession reopens last run's tabs, splits, directories and files.
	RestoreSession bool `toml:"restore_session" json:"restore_session"`
	// Passthrough lists key chords that are sent to the terminal even if bound.
	Passthrough []string `toml:"passthrough" json:"passthrough"`
}

// Terminal is the appearance of terminal panes.
type Terminal struct {
	Font        string  `toml:"font" json:"font"`
	FontSize    float64 `toml:"font_size" json:"font_size"`
	Ligatures   bool    `toml:"ligatures" json:"ligatures"`
	LineHeight  float64 `toml:"line_height" json:"line_height"`
	Scrollback  int     `toml:"scrollback" json:"scrollback"`
	CursorStyle string  `toml:"cursor_style" json:"cursor_style"`
	CursorBlink bool    `toml:"cursor_blink" json:"cursor_blink"`
	Padding     int     `toml:"padding" json:"padding"`
}

type Background struct {
	// Mode is "wallpaper", "translucent" or "solid".
	Mode      string  `toml:"mode" json:"mode"`
	Wallpaper string  `toml:"wallpaper" json:"wallpaper"`
	Blur      int     `toml:"blur" json:"blur"`
	Dim       float64 `toml:"dim" json:"dim"`
	Opacity   float64 `toml:"opacity" json:"opacity"`
}

type Claude struct {
	Command string `toml:"command" json:"command"`
	Notify  bool   `toml:"notify" json:"notify"`
	// ContextWindow overrides the assumed context size in tokens (0 = guess from the model).
	ContextWindow int `toml:"context_window" json:"context_window"`
}

// Editor is the appearance and behaviour of editor panes.
type Editor struct {
	Font                string  `toml:"font" json:"font"`
	FontSize            float64 `toml:"font_size" json:"font_size"`
	Ligatures           bool    `toml:"ligatures" json:"ligatures"`
	LineHeight          float64 `toml:"line_height" json:"line_height"`
	MarkdownDefaultMode string  `toml:"markdown_default_mode" json:"markdown_default_mode"`
	TabWidth            int     `toml:"tab_width" json:"tab_width"`
	WordWrap            bool    `toml:"word_wrap" json:"word_wrap"`
	LineNumbers         bool    `toml:"line_numbers" json:"line_numbers"`
}

// Defaults returns the embedded default configuration.
func Defaults() Config {
	var c Config
	if _, err := toml.Decode(string(defaultsTOML), &c); err != nil {
		panic("embedded defaults.toml invalid: " + err.Error())
	}
	return finish(c)
}

// Dir is the user config directory (~/.config/wate).
func Dir() string {
	if d := os.Getenv("WATE_CONFIG_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	dir := filepath.Join(base, "wate")
	migrateLegacyDir(filepath.Join(base, "yate"), dir)
	return dir
}

// migrateLegacyDir moves a directory left behind by the project's old name (yate) into place,
// once, when the new one does not exist yet.
func migrateLegacyDir(old, cur string) {
	if _, err := os.Stat(cur); err == nil {
		return
	}
	if st, err := os.Stat(old); err != nil || !st.IsDir() {
		return
	}
	if err := os.Rename(old, cur); err != nil {
		return
	}
	// The shell shim embeds absolute paths; it is regenerated on the next start.
	_ = os.RemoveAll(filepath.Join(cur, "shell"))
}

// StateDir is where session state lives (~/.local/state/wate), migrated from the old name too.
func StateDir() string {
	if d := os.Getenv("WATE_CONFIG_DIR"); d != "" {
		return d
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(base, "wate")
	migrateLegacyDir(filepath.Join(base, "yate"), dir)
	return dir
}

// Path is the user config file.
func Path() string { return filepath.Join(Dir(), "config.toml") }

// Load reads path on top of Defaults. A missing file yields Defaults without error.
func Load(path string) (Config, error) {
	c := Defaults()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return finish(c), nil
	}
	if err != nil {
		return c, err
	}
	return Parse(data, c)
}

// Parse decodes TOML on top of base.
func Parse(data []byte, base Config) (Config, error) {
	c := base
	md, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&c)
	if err != nil {
		return base, fmt.Errorf("config: %w", err)
	}
	var unknown []string
	for _, k := range md.Undecoded() {
		// [keys] is decoded as map[string]any, so its nested tables are always reported here.
		if len(k) > 0 && k[0] == "keys" {
			continue
		}
		unknown = append(unknown, k.String())
	}
	if len(unknown) > 0 {
		return finish(c), &UnknownKeysError{Keys: unknown}
	}
	return finish(c), nil
}

// UnknownKeysError is returned (with a usable Config) when the file contains keys wate doesn't know.
type UnknownKeysError struct{ Keys []string }

func (e *UnknownKeysError) Error() string {
	return "config: unknown keys: " + strings.Join(e.Keys, ", ")
}

func finish(c Config) Config {
	c.Keys = effectiveKeys(c.KeysRaw, runtime.GOOS)
	c.Background.Wallpaper = ExpandHome(c.Background.Wallpaper)
	if c.Background.Opacity <= 0 || c.Background.Opacity > 1 {
		c.Background.Opacity = 1
	}
	if c.Background.Dim < 0 || c.Background.Dim > 1 {
		c.Background.Dim = 0
	}
	switch c.Background.Mode {
	case "wallpaper", "translucent", "solid":
	default:
		c.Background.Mode = "solid"
	}
	if c.Background.Mode == "wallpaper" && c.Background.Wallpaper == "" {
		c.Background.Mode = "solid"
	}
	switch c.Terminal.CursorStyle {
	case "block", "underline", "bar":
	default:
		c.Terminal.CursorStyle = "block"
	}
	switch c.Editor.MarkdownDefaultMode {
	case "code", "split", "preview":
	default:
		c.Editor.MarkdownDefaultMode = "preview"
	}
	return c
}

// ExpandHome replaces a leading ~ with the user's home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// WriteDefaultIfMissing creates the config file with the embedded defaults so users have a template.
func WriteDefaultIfMissing(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, defaultsTOML, 0o644)
}

// effectiveKeys flattens the [keys] table, applying [keys.<goos>] overrides.
func effectiveKeys(raw map[string]any, goos string) map[string]string {
	keys := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			keys[k] = s
		}
	}
	if overrides, ok := raw[goos].(map[string]any); ok {
		for k, v := range overrides {
			if s, ok := v.(string); ok {
				keys[k] = s
			}
		}
	}
	return keys
}

// Set writes dotted keys ("terminal.font_size", "keys.split_right") into the TOML file at path.
// It edits the file line by line so comments, ordering and unknown keys survive: an existing
// `key = value  # comment` keeps its comment, missing keys are appended to their section and
// missing sections are appended to the file. Key bindings go into [keys.darwin] on macOS so they
// don't clobber the Linux defaults.
func Set(path string, values map[string]any) error {
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		if _, err := toml.Decode(string(data), &map[string]any{}); err != nil {
			return fmt.Errorf("config: %w", err)
		}
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		if len(lines) == 1 && lines[0] == "" {
			lines = nil
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts := strings.Split(k, ".")
		if len(parts) < 2 {
			return fmt.Errorf("config: key %q must be section.key", k)
		}
		if parts[0] == "keys" && runtime.GOOS == "darwin" && len(parts) == 2 {
			parts = []string{"keys", "darwin", parts[1]}
		}
		section := strings.Join(parts[:len(parts)-1], ".")
		val, err := tomlValue(values[k])
		if err != nil {
			return fmt.Errorf("config: %s: %w", k, err)
		}
		lines = setLine(lines, section, parts[len(parts)-1], val)
	}
	out := strings.Join(lines, "\n") + "\n"
	if _, err := toml.Decode(out, &map[string]any{}); err != nil {
		return fmt.Errorf("config: refusing to write invalid TOML: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(out), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var (
	sectionRe = regexp.MustCompile(`^\s*\[\s*([A-Za-z0-9_.\-"]+)\s*\]`)
	keyRe     = regexp.MustCompile(`^(\s*)([A-Za-z0-9_\-"]+)(\s*=\s*)(.*)$`)
)

// setLine replaces or inserts `key = val` inside [section] and returns the new lines.
func setLine(lines []string, section, key, val string) []string {
	secStart, secEnd := -1, len(lines)
	for i, l := range lines {
		if m := sectionRe.FindStringSubmatch(l); m != nil {
			if secStart >= 0 {
				secEnd = i
				break
			}
			if strings.ReplaceAll(m[1], `"`, "") == section {
				secStart = i
			}
		}
	}
	if secStart < 0 {
		// New section at the end of the file.
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		return append(lines, "["+section+"]", key+" = "+val)
	}
	// Sub-tables ([keys.darwin]) that start with this section's name end it too.
	lastKey := secStart
	for i := secStart + 1; i < secEnd; i++ {
		m := keyRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		lastKey = i
		if strings.Trim(m[2], `"`) != key {
			continue
		}
		rest := m[4]
		if !balanced(rest) {
			// Multi-line value: don't try to be clever, replace the whole thing.
			end := i
			for end < secEnd-1 && !balanced(strings.Join(lines[i:end+1], "\n")) {
				end++
			}
			replaced := append([]string{}, lines[:i]...)
			replaced = append(replaced, m[1]+m[2]+m[3]+val)
			return append(replaced, lines[end+1:]...)
		}
		// Keep whatever followed the old value (padding + comment) verbatim.
		comment := ""
		if idx := commentIndex(rest); idx >= 0 {
			comment = rest[len(strings.TrimRight(rest[:idx], " \t")):]
		}
		lines[i] = m[1] + m[2] + m[3] + val + comment
		return lines
	}
	// Key missing: insert after the last key of the section (before trailing blank lines).
	insert := lastKey + 1
	out := append([]string{}, lines[:insert]...)
	out = append(out, key+" = "+val)
	return append(out, lines[insert:]...)
}

// commentIndex finds the start of a trailing comment, ignoring '#' inside quotes.
func commentIndex(s string) int {
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr != 0:
			if c == '\\' && inStr == '"' {
				i++
			} else if c == inStr {
				inStr = 0
			}
		case c == '"' || c == '\'':
			inStr = c
		case c == '#':
			return i
		}
	}
	return -1
}

func balanced(s string) bool {
	if idx := commentIndex(s); idx >= 0 {
		s = s[:idx]
	}
	return strings.Count(s, "[") == strings.Count(s, "]") && strings.Count(s, "{") == strings.Count(s, "}")
}

// tomlValue renders a JSON-ish value as a TOML literal.
func tomlValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return strconv.Quote(x), nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10), nil
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case []string:
		parts := make([]string, len(x))
		for i, s := range x {
			parts[i] = strconv.Quote(s)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			p, err := tomlValue(e)
			if err != nil {
				return "", err
			}
			parts[i] = p
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case nil:
		return "", errors.New("null is not a TOML value")
	default:
		return "", fmt.Errorf("unsupported value %T", v)
	}
}
