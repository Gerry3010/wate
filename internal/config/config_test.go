package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
