// Package opener decides what a clicked path is (text file → editor, anything else →
// default application) and opens things outside of yate.
package opener

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Target is a resolved click target.
type Target struct {
	// Raw is the text as it appeared in the terminal.
	Raw string `json:"raw"`
	// Path is absolute; empty when Raw could not be resolved.
	Path string `json:"path"`
	// Kind is "file", "dir" or "missing".
	Kind string `json:"kind"`
	// Text reports whether the file looks editable as text.
	Text bool `json:"text"`
	Line int  `json:"line"`
	Col  int  `json:"col"`
}

var lineColRe = regexp.MustCompile(`^(.*?)(?::(\d+))?(?::(\d+))?:?$`)

// Resolve turns terminal text into a Target relative to cwd. Trailing ":line[:col]" is split off.
func Resolve(cwd, raw string) Target {
	t := Target{Raw: raw}
	text := strings.TrimRight(raw, ".,;)]}'\"")
	if m := lineColRe.FindStringSubmatch(text); m != nil {
		text = m[1]
		t.Line, _ = strconv.Atoi(m[2])
		t.Col, _ = strconv.Atoi(m[3])
	}
	// "(file.go:12)" style: also strip a leading bracket.
	text = strings.TrimLeft(text, "([{'\"")
	if text == "" {
		t.Kind = "missing"
		return t
	}
	if strings.HasPrefix(text, "~") {
		home, _ := os.UserHomeDir()
		text = filepath.Join(home, strings.TrimPrefix(text, "~"))
	}
	if !filepath.IsAbs(text) {
		text = filepath.Join(cwd, text)
	}
	t.Path = filepath.Clean(text)
	st, err := os.Stat(t.Path)
	switch {
	case err != nil:
		t.Kind = "missing"
	case st.IsDir():
		t.Kind = "dir"
	default:
		t.Kind = "file"
		t.Text = IsText(t.Path, st.Size())
	}
	return t
}

var textExt = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`md markdown txt rst adoc org go rs ts tsx js jsx mjs cjs json json5 jsonc yaml yml toml ini cfg conf
	env py rb php pl lua sh bash zsh fish ps1 c h cc cpp hpp cs java kt kts swift m mm dart scala sql html htm css scss sass less
	xml svg vue svelte astro tex bib csv tsv log gitignore dockerignore editorconfig makefile cmake gradle properties lock nix
	proto graphql gql hcl tf tfvars ex exs erl hs ml elm clj cljs edn zig nim v vala d f90 r jl`) {
		textExt[e] = true
	}
}

// IsText guesses whether the file is editable text: known extension, text/* MIME, or a NUL-free sample.
func IsText(path string, size int64) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if textExt[ext] || textExt[strings.ToLower(filepath.Base(path))] {
		return true
	}
	if size == 0 {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	buf = buf[:n]
	if bytes.IndexByte(buf, 0) >= 0 {
		return false
	}
	ct := http.DetectContentType(buf)
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	return utf8.Valid(buf)
}

// OpenExternal opens a path with the platform's default application.
func OpenExternal(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}
