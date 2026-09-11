package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadContext(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	lines := `{"type":"user","message":{"role":"user","content":"hi"}}
{"type":"assistant","message":{"model":"claude-sonnet-5","usage":{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000,"output_tokens":5}}}
{"type":"attachment","attachment":{"type":"x"}}
{"type":"assistant","message":{"model":"claude-sonnet-5","usage":{"input_tokens":56,"cache_creation_input_tokens":655,"cache_read_input_tokens":291057,"output_tokens":2020}}}
{"type":"last-prompt","lastPrompt":"/compact"}
`
	if err := os.WriteFile(p, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	c, ok := ReadContext(p)
	if !ok || c.Tokens != 291768 || c.Model != "claude-sonnet-5" {
		t.Fatalf("got %+v ok=%v", c, ok)
	}
	if _, ok := ReadContext(filepath.Join(dir, "missing.jsonl")); ok {
		t.Error("missing file should not be ok")
	}
	_ = os.WriteFile(p, []byte(`{"type":"user"}`+"\n"), 0o600)
	if _, ok := ReadContext(p); ok {
		t.Error("no usage should not be ok")
	}
}

func TestWindowFor(t *testing.T) {
	cases := []struct {
		model            string
		configured, seen int
		want             int
	}{
		{"claude-sonnet-5", 0, 50_000, 200_000},
		{"claude-fable-5-1", 0, 50_000, 1_000_000},
		{"claude-sonnet-4-5[1m]", 0, 0, 1_000_000},
		{"claude-sonnet-5", 0, 291_768, 1_000_000},
		{"claude-sonnet-5", 400_000, 10, 400_000},
		{"claude-fable-5-1", 0, 1_200_000, 2_000_000},
	}
	for _, c := range cases {
		if got := WindowFor(c.model, c.configured, c.seen); got != c.want {
			t.Errorf("WindowFor(%q,%d,%d) = %d, want %d", c.model, c.configured, c.seen, got, c.want)
		}
	}
	if (Context{Tokens: 291768, Window: 1_000_000}).Percent() != 29 {
		t.Error("percent")
	}
}

func TestFindTranscriptAndRefresh(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/x/Sync-Projekte/wate"
	dir := filepath.Join(home, ".claude", "projects", "-home-x-Sync-Projekte-wate")
	_ = os.MkdirAll(dir, 0o755)
	old := filepath.Join(dir, "old.jsonl")
	_ = os.WriteFile(old, []byte(`{"type":"assistant","message":{"model":"m","usage":{"input_tokens":1,"cache_read_input_tokens":9}}}`+"\n"), 0o600)
	newer := filepath.Join(dir, "new.jsonl")
	_ = os.WriteFile(newer, []byte(`{"type":"assistant","message":{"model":"m","usage":{"input_tokens":1,"cache_read_input_tokens":99}}}`+"\n"), 0o600)
	os.Chtimes(old, fixedTime(1), fixedTime(1))
	os.Chtimes(newer, fixedTime(2), fixedTime(2))
	if got := FindTranscript(home, cwd); got != newer {
		t.Fatalf("FindTranscript = %q", got)
	}
	if FindTranscript(home, "/nowhere") != "" {
		t.Error("unknown cwd should yield nothing")
	}

	tr := NewTracker()
	var changes []Session
	tr.OnChange = func(s Session) { changes = append(changes, s) }
	tr.Observe("p1", "t1", true, cwd)
	tr.RefreshContexts(home, 0)
	if len(changes) != 2 || changes[1].Context.Tokens != 100 || changes[1].ContextPercent != 0 || changes[1].Context.Window != 200_000 {
		t.Fatalf("changes: %+v", changes)
	}
	// Unchanged file → no event; grown file → event.
	tr.RefreshContexts(home, 0)
	if len(changes) != 2 {
		t.Fatal("unchanged transcript re-emitted")
	}
	// A trickle of new tokens waits for the pacing (a streaming session appends constantly).
	appendLine := func(reads int, pad int) {
		f, _ := os.OpenFile(newer, os.O_APPEND|os.O_WRONLY, 0o600)
		fmt.Fprintf(f, `{"type":"assistant","pad":"%s","message":{"model":"m","usage":{"input_tokens":1,"cache_read_input_tokens":%d}}}`+"\n", strings.Repeat("x", pad), reads)
		f.Close()
	}
	appendLine(50_000, 0)
	tr.RefreshContexts(home, 0)
	if len(changes) != 2 {
		t.Fatalf("a small append should wait for the next interval: %+v", changes[len(changes)-1])
	}
	// Once it has grown by a chunk, the context is re-read straight away.
	appendLine(149_999, contextGrowth)
	tr.RefreshContexts(home, 0)
	if len(changes) != 3 || changes[2].ContextPercent != 75 {
		t.Fatalf("after growth: %+v", changes[len(changes)-1])
	}
	// Hooks hand over the real transcript path.
	tr.Hook("p1", "t1", "UserPromptSubmit", []byte(`{"transcript_path":"`+old+`","prompt":"x"}`))
	tr.RefreshContexts(home, 0)
	if last := changes[len(changes)-1]; last.TranscriptPath != old || last.Context.Tokens != 10 {
		t.Fatalf("hook transcript: %+v", last)
	}
}

func fixedTime(n int) time.Time { return time.Unix(1_700_000_000+int64(n)*60, 0) }
