// Package shell installs wate's shell integration snippets and prepares the
// environment so zsh picks them up automatically (ZDOTDIR trick, as kitty does).
package shell

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
)

//go:embed wate.zsh
var zshSnippet []byte

//go:embed zshenv
var zshenvTemplate string

//go:embed wate.bash
var bashSnippet []byte

//go:embed wate.fish
var fishSnippet []byte

// Install writes the snippets below dir (e.g. ~/.config/wate/shell). Idempotent.
func Install(dir string) error {
	zshDir := filepath.Join(dir, "zsh")
	if err := os.MkdirAll(zshDir, 0o755); err != nil {
		return err
	}
	files := map[string][]byte{
		filepath.Join(dir, "wate.zsh"):   zshSnippet,
		filepath.Join(dir, "wate.bash"):  bashSnippet,
		filepath.Join(dir, "wate.fish"):  fishSnippet,
		filepath.Join(zshDir, ".zshenv"): []byte(strings.ReplaceAll(zshenvTemplate, "__WATE_SHELL_DIR__", dir)),
	}
	for p, content := range files {
		if cur, err := os.ReadFile(p); err == nil && string(cur) == string(content) {
			continue
		}
		if err := os.WriteFile(p, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Env returns extra environment entries for spawning the given shell so the
// integration loads without the user editing rc files (zsh only; bash/fish need a source line).
func Env(shellPath, dir string) []string {
	if filepath.Base(shellPath) != "zsh" {
		return nil
	}
	orig := os.Getenv("ZDOTDIR")
	env := []string{"ZDOTDIR=" + filepath.Join(dir, "zsh")}
	if orig != "" {
		env = append(env, "WATE_ZDOTDIR_ORIG="+orig)
	}
	return env
}
