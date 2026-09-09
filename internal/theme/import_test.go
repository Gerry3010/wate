package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const ghostty = `palette = 0=#45475a
palette = 1=#f38ba8
palette = 2=#a6e3a1
palette = 3=#f9e2af
palette = 4=#89b4fa
palette = 5=#f5c2e7
palette = 6=#94e2d5
palette = 7=#bac2de
palette = 8=#585b70
palette = 12=#aeccfc
background = #1e1e2e
foreground = #cdd6f4
cursor-color = #f5e0dc
cursor-text = #1e1e2e
selection-background = #f5e0dc
selection-foreground = #1e1e2e
`

const alacritty = `# Colors (Foo Bar)
[colors.bright]
black = '#585b70'
blue = '#aeccfc'
[colors.cursor]
cursor = '#f5e0dc'
text = '#1e1e2e'
[colors.normal]
black = '#45475a'
blue = '#89b4fa'
cyan = '#94e2d5'
green = '#a6e3a1'
magenta = '#f5c2e7'
red = '#f38ba8'
white = '#bac2de'
yellow = '#f9e2af'
[colors.primary]
background = '#1e1e2e'
foreground = '#cdd6f4'
`

func TestImportGhosttyAndAlacritty(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Catppuccin Mocha")
	os.WriteFile(src, []byte(ghostty), 0o644)
	id, err := ImportFile(src, filepath.Join(dir, "themes"))
	if err != nil || id != "catppuccin-mocha" {
		t.Fatalf("ghostty import: %q %v", id, err)
	}
	r, err := Load(id, filepath.Join(dir, "themes"))
	if err != nil {
		t.Fatal(err)
	}
	if r.XTerm["brightBlue"] != "#aeccfc" || r.XTerm["brightRed"] != "#f38ba8" /* filled from normal */ || r.XTerm["cursor"] != "#f5e0dc" {
		t.Fatalf("ghostty colours: %v", r.XTerm)
	}
	if r.Theme.Name != "Catppuccin Mocha" || r.CSSVars["accent"] != "#89b4fa" {
		t.Fatalf("meta: %+v", r.Theme)
	}

	src2 := filepath.Join(dir, "Foo Bar.toml")
	os.WriteFile(src2, []byte(alacritty), 0o644)
	id, err = ImportFile(src2, filepath.Join(dir, "themes"))
	if err != nil || id != "foo-bar" {
		t.Fatalf("alacritty import: %q %v", id, err)
	}
	r, _ = Load(id, filepath.Join(dir, "themes"))
	if r.XTerm["blue"] != "#89b4fa" || r.XTerm["brightBlue"] != "#aeccfc" || r.XTerm["background"] != "#1e1e2e" {
		t.Fatalf("alacritty colours: %v", r.XTerm)
	}

	os.WriteFile(filepath.Join(dir, "junk.txt"), []byte("hello"), 0o644)
	if _, err := ImportFile(filepath.Join(dir, "junk.txt"), dir); err == nil {
		t.Fatal("junk must be rejected")
	}
	if Slug("  Tokyo Night (Storm)! ") != "tokyo-night-storm" {
		t.Fatal("slug")
	}
}

func TestImportDataAndListInfo(t *testing.T) {
	dir := t.TempDir()
	ghostty := "background = 101010\nforeground = f0f0f0\n"
	for i := 0; i < 16; i++ {
		ghostty += fmt.Sprintf("palette = %d=#%02x0000\n", i, 0x10*i+0xf)
	}
	id, err := ImportData("My Paste Theme", []byte(ghostty), dir)
	if err != nil || id != "my-paste-theme" {
		t.Fatalf("ImportData: %v %q", err, id)
	}
	r, err := Load(id, dir)
	if err != nil || r.Theme.Name != "My Paste Theme" || r.Theme.Colors.Red != "#1f0000" {
		t.Fatalf("Load: %v %+v", err, r.Theme)
	}
	if _, err := ImportData("", []byte(ghostty), dir); err == nil {
		t.Error("empty name should fail")
	}
	if _, err := ImportData("x", []byte("nothing useful"), dir); err == nil {
		t.Error("unknown format should fail")
	}
	infos := ListInfo(dir)
	var user, builtin int
	for _, i := range infos {
		if i.Builtin {
			builtin++
		} else {
			user++
			if i.ID != id || i.Name != "My Paste Theme" {
				t.Errorf("user info: %+v", i)
			}
		}
	}
	if user != 1 || builtin < 5 {
		t.Errorf("list: %d user, %d builtin", user, builtin)
	}
	if err := DeleteUser("catppuccin-mocha", dir); err == nil {
		t.Error("deleting a built-in theme must fail")
	}
	if err := DeleteUser(id, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(id, dir); err == nil {
		t.Error("user theme still loads after delete")
	}
}
