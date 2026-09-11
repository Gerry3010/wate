package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAndEnv(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shell")
	if err := Install(dir); err != nil {
		t.Fatal(err)
	}
	if err := Install(dir); err != nil {
		t.Fatal("second install must be a no-op:", err)
	}
	zshenv, err := os.ReadFile(filepath.Join(dir, "zsh", ".zshenv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(zshenv), filepath.Join(dir, "wate.zsh")) || strings.Contains(string(zshenv), "__WATE_SHELL_DIR__") {
		t.Fatalf("zshenv not templated: %s", zshenv)
	}
	for _, f := range []string{"wate.zsh", "wate.bash", "wate.fish"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatal(err)
		}
	}
	// Word-wise keys: every snippet honours WATE_WORD_KEYS and binds both modifier sets.
	for f, want := range map[string][]string{
		"wate.zsh":  {"WATE_WORD_KEYS", "backward-kill-word", "^[[1;5D", "^[[1;3D", "^H"},
		"wate.bash": {"WATE_WORD_KEYS", "backward-kill-word", "\\e[1;5D", "\\e[1;3D", "\\C-h"},
		"wate.fish": {"WATE_WORD_KEYS", "backward-kill-word", "1\\;5D", "1\\;3D"},
	} {
		body, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range want {
			if !strings.Contains(string(body), w) {
				t.Errorf("%s does not bind %q", f, w)
			}
		}
	}
	t.Setenv("ZDOTDIR", "")
	env := Env("/bin/zsh", dir)
	if len(env) != 1 || env[0] != "ZDOTDIR="+filepath.Join(dir, "zsh") {
		t.Fatalf("env = %v", env)
	}
	t.Setenv("ZDOTDIR", "/home/x/.zsh")
	if env := Env("/usr/bin/zsh", dir); len(env) != 2 || env[1] != "WATE_ZDOTDIR_ORIG=/home/x/.zsh" {
		t.Fatalf("env = %v", env)
	}
	if Env("/bin/bash", dir) != nil {
		t.Fatal("bash gets no env injection")
	}
}
