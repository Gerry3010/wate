package config

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	c := Defaults()
	if c.Keys["split_right"] != "ctrl+space" || c.Terminal.Scrollback != 10000 || c.Background.Mode != "solid" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.Editor.MarkdownDefaultMode != "preview" || !c.Editor.Ligatures || c.Editor.FontSize != 13 {
		t.Fatalf("editor defaults: %+v", c.Editor)
	}
	if !c.Terminal.Ligatures || c.Terminal.CursorStyle != "block" || c.Terminal.FontSize != 13 {
		t.Fatalf("terminal defaults: %+v", c.Terminal)
	}
}

func TestParseOverlay(t *testing.T) {
	c, err := Parse([]byte(`
[terminal]
font_size = 14
cursor_style = "weird"
[keys]
split_right = "ctrl+enter"
[background]
mode = "wallpaper"
wallpaper = "~/wall.png"
opacity = 0.5
`), Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if c.Terminal.FontSize != 14 || c.Terminal.Scrollback != 10000 || c.Terminal.CursorStyle != "block" {
		t.Fatalf("overlay lost defaults or invalid enum not reset: %+v", c.Terminal)
	}
	if c.Keys["split_right"] != "ctrl+enter" || c.Keys["split_down"] != "ctrl+shift+space" {
		t.Fatalf("keys not merged: %v", c.Keys)
	}
	home, _ := os.UserHomeDir()
	if c.Background.Wallpaper != filepath.Join(home, "wall.png") || c.Background.Mode != "wallpaper" || c.Background.Opacity != 0.5 {
		t.Fatalf("background: %+v", c.Background)
	}
}

func TestParseUnknownKeys(t *testing.T) {
	c, err := Parse([]byte("[terminal]\nfont_size = 9\nbogus = 1\n"), Defaults())
	var uk *UnknownKeysError
	if !errors.As(err, &uk) || len(uk.Keys) != 1 || uk.Keys[0] != "terminal.bogus" {
		t.Fatalf("want UnknownKeysError, got %v", err)
	}
	if c.Terminal.FontSize != 9 {
		t.Fatal("config should still be usable")
	}
}

func TestParseInvalidModeFallsBack(t *testing.T) {
	c, err := Parse([]byte("[background]\nmode = \"wallpaper\"\n"), Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if c.Background.Mode != "solid" {
		t.Fatalf("wallpaper mode without image should fall back to solid, got %q", c.Background.Mode)
	}
}

func TestWordKeys(t *testing.T) {
	if got := Defaults().Terminal.WordKeys; got != "ctrl" {
		t.Fatalf("default word_keys = %q", got)
	}
	for in, want := range map[string]string{"alt": "alt", "both": "both", "off": "off", "nonsense": "ctrl", "": "ctrl"} {
		c, err := Parse([]byte("[terminal]\nword_keys = \""+in+"\"\n"), Defaults())
		if err != nil {
			t.Fatal(err)
		}
		if c.Terminal.WordKeys != want {
			t.Errorf("word_keys %q → %q, want %q", in, c.Terminal.WordKeys, want)
		}
	}
}

func TestDarwinOverrides(t *testing.T) {
	raw := Defaults().KeysRaw
	linux := effectiveKeys(raw, "linux")
	darwin := effectiveKeys(raw, "darwin")
	if linux["copy"] != "ctrl+shift+c" || linux["split_right"] != "ctrl+space" {
		t.Fatalf("linux keys: %v", linux)
	}
	if darwin["copy"] != "cmd+c" || darwin["split_right"] != "ctrl+space" {
		t.Fatalf("darwin keys: %v", darwin)
	}
	if _, leaked := linux["darwin"]; leaked {
		t.Fatal("sub-table leaked into bindings")
	}
	c := Defaults()
	if want := effectiveKeys(raw, runtime.GOOS); c.Keys["copy"] != want["copy"] {
		t.Fatalf("Defaults() did not apply platform keys")
	}
}

func TestLoadMissingAndWriteDefault(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.toml")
	c, err := Load(p)
	if err != nil || c.Terminal.Scrollback != 10000 {
		t.Fatalf("missing file should yield defaults: %v", err)
	}
	if err := WriteDefaultIfMissing(p); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatalf("written defaults must parse cleanly: %v", err)
	}
}

func TestSetPreservesOtherKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("[terminal]\nfont_size = 11\n[keys]\nsplit_right = \"ctrl+space\"\n[keys.darwin]\ncopy = \"cmd+c\"\n[custom]\nx = 1\n"), 0o644)
	if err := Set(p, map[string]any{"terminal.font_size": 15, "background.mode": "wallpaper", "keys.new_tab": "ctrl+n"}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		// [custom] is unknown → UnknownKeysError, but the config is still usable
		var uk *UnknownKeysError
		if !errors.As(err, &uk) {
			t.Fatal(err)
		}
	}
	if c.Terminal.FontSize != 15 || c.Background.Mode != "solid" /* no wallpaper set → falls back */ {
		t.Fatalf("%+v", c)
	}
	raw, _ := os.ReadFile(p)
	for _, want := range []string{"split_right", "cmd+c", "x = 1", "new_tab"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("lost %q in\n%s", want, raw)
		}
	}
	if runtime.GOOS == "darwin" && c.Keys["new_tab"] != "ctrl+n" {
		t.Fatalf("darwin override not applied: %v", c.Keys)
	}
}

func TestSetKeepsCommentsAndLayout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := WriteDefaultIfMissing(p); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p)
	if err := Set(p, map[string]any{
		"terminal.font_size": 14.0,
		"general.theme":      "nord",
		"general.shell_args": []any{"-l"},
		"background.mode":    "translucent",
		"keys.new_tab":       "ctrl+n",
		"claude.notify":      false,
		"custom.thing":       "x",
	}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p)
	if strings.Count(string(after), "#") < strings.Count(string(before), "#") {
		t.Fatalf("comments lost:\n%s", after)
	}
	for _, want := range []string{
		`font_size = 14\s+# px`,
		`theme = "nord"\s+# name of a built-in`,
		`shell_args = \["-l"\]`,
		`mode = "translucent"\s+# "wallpaper"`,
		`\[custom\]\nthing = "x"`,
	} {
		if !regexp.MustCompile(want).Match(after) {
			t.Fatalf("missing %q in\n%s", want, after)
		}
	}
	c, err := Load(p)
	var uk *UnknownKeysError
	if err != nil && !errors.As(err, &uk) {
		t.Fatal(err)
	}
	if c.Terminal.FontSize != 14 || c.General.Theme != "nord" || c.Claude.Notify || len(c.General.ShellArgs) != 1 {
		t.Fatalf("%+v", c)
	}
	if runtime.GOOS != "darwin" && c.Keys["new_tab"] != "ctrl+n" {
		t.Fatalf("keys: %v", c.Keys)
	}
	// [keys.darwin] must still be intact and after [keys].
	if !strings.Contains(string(after), "[keys.darwin]\ncopy") {
		t.Fatalf("darwin table damaged:\n%s", after)
	}
}

func TestSetLineHelpers(t *testing.T) {
	if commentIndex(`"a # b"  # real`) != 9 || commentIndex(`'#'`) != -1 || commentIndex(`x`) != -1 {
		t.Fatal("commentIndex")
	}
	lines := setLine([]string{"[a]", "x = 1", "", "[b]", "y = 2"}, "a", "z", "3")
	if strings.Join(lines, "|") != "[a]|x = 1|z = 3||[b]|y = 2" {
		t.Fatalf("insert: %v", lines)
	}
	lines = setLine([]string{"[a]", "x = [", "  1,", "]"}, "a", "x", "[2]")
	if strings.Join(lines, "|") != "[a]|x = [2]" {
		t.Fatalf("multiline: %v", lines)
	}
}

func TestMigrateLegacyDir(t *testing.T) {
	base := t.TempDir()
	old, cur := filepath.Join(base, "yate"), filepath.Join(base, "wate")
	os.MkdirAll(filepath.Join(old, "shell"), 0o755)
	os.WriteFile(filepath.Join(old, "config.toml"), []byte("[general]\n"), 0o644)
	migrateLegacyDir(old, cur)
	if _, err := os.Stat(filepath.Join(cur, "config.toml")); err != nil {
		t.Fatal("config not migrated")
	}
	if _, err := os.Stat(filepath.Join(cur, "shell")); err == nil {
		t.Fatal("stale shell shim should be dropped")
	}
	if _, err := os.Stat(old); err == nil {
		t.Fatal("old dir should be gone")
	}
	migrateLegacyDir(old, cur) // no-op
}
