package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinsLoadAndValidate(t *testing.T) {
	ids := List("")
	if len(ids) < 5 {
		t.Fatalf("expected built-in themes, got %v", ids)
	}
	for _, id := range ids {
		r, err := Load(id, "")
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if r.XTerm["background"] == "" || r.CSSVars["accent"] == "" || r.Theme.Name == "" {
			t.Fatalf("%s: incomplete %+v", id, r)
		}
	}
}

func TestUserThemeOverridesAndDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "catppuccin-mocha.toml"), []byte(`
[colors]
background = "#000000"
foreground = "#ffffff"
black = "#111111"
red = "#ff0000"
green = "#00ff00"
yellow = "#ffff00"
blue = "#0000ff"
magenta = "#ff00ff"
cyan = "#00ffff"
white = "#eeeeee"
`), 0o644)
	r, err := Load("catppuccin-mocha", dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.XTerm["background"] != "#000000" || r.Theme.Name != "catppuccin-mocha" {
		t.Fatalf("user theme not used: %+v", r.Theme)
	}
	if r.XTerm["brightRed"] != "#ff0000" || r.XTerm["cursor"] != "#ffffff" || r.CSSVars["accent"] != "#0000ff" {
		t.Fatalf("defaults not filled: %v", r.XTerm)
	}
	ids := List(dir)
	if n := strings.Count(strings.Join(ids, ","), "catppuccin-mocha"); n != 1 {
		t.Fatalf("expected dedup, got %v", ids)
	}
}

func TestInvalidColour(t *testing.T) {
	_, err := Parse("x", []byte("[colors]\nbackground = \"red\"\nforeground = \"#fff\"\n"))
	if err == nil || !strings.Contains(err.Error(), "not a #rrggbb") {
		t.Fatalf("want validation error, got %v", err)
	}
	if _, err := Load("does-not-exist", ""); err == nil {
		t.Fatal("missing theme must error")
	}
}
