package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "session.jsonl")
	data := ""
	for _, l := range lines {
		data += l + "\n"
	}
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadTitleFirstPrompt(t *testing.T) {
	p := writeTranscript(t,
		`{"type":"system","content":"boot"}`,
		`{"type":"user","message":{"role":"user","content":"<command-name>/compact</command-name>"}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","text":"nope"},{"type":"text","text":"Fix the tab bar\nand the panes"}]}}`,
		`{"type":"assistant","message":{"usage":{"input_tokens":10}}}`,
	)
	if got := ReadTitle(p); got != "Fix the tab bar" {
		t.Fatalf("got %q", got)
	}
}

func TestReadTitlePrefersSummary(t *testing.T) {
	p := writeTranscript(t,
		`{"type":"summary","summary":"Terminal restore and pane swap","leafUuid":"x"}`,
		`{"type":"user","message":{"role":"user","content":"whatever"}}`,
	)
	if got := ReadTitle(p); got != "Terminal restore and pane swap" {
		t.Fatalf("got %q", got)
	}
}

func TestReadTitleTruncates(t *testing.T) {
	long := "Please refactor the importer so that Warp tab groups end up as coloured wate tabs"
	p := writeTranscript(t, `{"type":"user","message":{"role":"user","content":"`+long+`"}}`)
	got := ReadTitle(p)
	if len([]rune(got)) != titleLen {
		t.Fatalf("length %d, want %d (%q)", len([]rune(got)), titleLen, got)
	}
}

func TestReadTitleMissingFile(t *testing.T) {
	if got := ReadTitle(filepath.Join(t.TempDir(), "nope.jsonl")); got != "" {
		t.Fatalf("got %q", got)
	}
}
