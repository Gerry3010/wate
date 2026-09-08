package opener

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0, 1, 2, 3, 0xff}, 0o644)
	os.WriteFile(filepath.Join(dir, "notes"), []byte("plain words\n"), 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)

	cases := []struct {
		raw       string
		kind      string
		text      bool
		line, col int
	}{
		{"main.go", "file", true, 0, 0},
		{"./main.go:12", "file", true, 12, 0},
		{"main.go:12:5", "file", true, 12, 5},
		{"(main.go:3)", "file", true, 3, 0},
		{"main.go,", "file", true, 0, 0},
		{filepath.Join(dir, "main.go"), "file", true, 0, 0},
		{"blob.bin", "file", false, 0, 0},
		{"notes", "file", true, 0, 0},
		{"sub", "dir", false, 0, 0},
		{"sub/", "dir", false, 0, 0},
		{"nope.txt", "missing", false, 0, 0},
		{"", "missing", false, 0, 0},
	}
	for _, c := range cases {
		got := Resolve(dir, c.raw)
		if got.Kind != c.kind || got.Text != c.text || got.Line != c.line || got.Col != c.col {
			t.Errorf("%q: got %+v", c.raw, got)
		}
	}
}

func TestResolveHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := Resolve("/", "~"); got.Path != home || got.Kind != "dir" {
		t.Fatalf("~ → %+v", got)
	}
}
