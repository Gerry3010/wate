// Package config loads yate's TOML configuration with embedded defaults.
package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	// ShellIntegration auto-loads yate's zsh snippet (OSC 7 cwd + OSC 133 prompt marks).
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

// Dir is the user config directory (~/.config/yate).
func Dir() string {
	if d := os.Getenv("YATE_CONFIG_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "yate")
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

// UnknownKeysError is returned (with a usable Config) when the file contains keys yate doesn't know.
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

// Set writes dotted keys ("terminal.font_size", "keys.split_right") into the TOML file at path,
// keeping every other key (including [keys.darwin] and unknown ones) intact. Comments are lost,
// which is the price of not hand-parsing TOML. Key bindings are written into the platform table
// on macOS so they don't clobber the Linux defaults.
func Set(path string, values map[string]any) error {
	doc := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if _, err := toml.Decode(string(data), &doc); err != nil {
			return fmt.Errorf("config: %w", err)
		}
	}
	for k, v := range values {
		parts := strings.Split(k, ".")
		if len(parts) < 2 {
			return fmt.Errorf("config: key %q must be section.key", k)
		}
		if parts[0] == "keys" && runtime.GOOS == "darwin" && len(parts) == 2 {
			parts = []string{"keys", "darwin", parts[1]}
		}
		cur := doc
		for _, p := range parts[:len(parts)-1] {
			next, ok := cur[p].(map[string]any)
			if !ok {
				next = map[string]any{}
				cur[p] = next
			}
			cur = next
		}
		cur[parts[len(parts)-1]] = v
	}
	var buf bytes.Buffer
	buf.WriteString("# yate configuration — https://github.com/Gerry3010/yate\n")
	buf.WriteString("# Written by the settings pane; see internal/config/defaults.toml for all keys and comments.\n\n")
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
