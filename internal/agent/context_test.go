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
		// Claude Code gives these 1M from the first turn: 177k is 18 %, not 89 %.
		{"claude-opus-5-5", 0, 177_000, 1_000_000},
		{"claude-opus-5", 0, 50_000, 1_000_000},
		{"claude-opus-4-7", 0, 50_000, 1_000_000},
		{"claude-opus-4-8", 0, 50_000, 1_000_000},
		{"claude-sonnet-5", 0, 50_000, 1_000_000},
		{"claude-sonnet-5-5", 0, 50_000, 1_000_000},
		{"claude-haiku-5-5", 0, 50_000, 1_000_000},
		{"claude-fable-5-1", 0, 50_000, 1_000_000},
		{"claude-mythos-5", 0, 50_000, 1_000_000},
		// These start at 200k.
		{"claude-opus-4-6", 0, 50_000, 200_000},
		{"claude-opus-4-5", 0, 50_000, 200_000},
		{"claude-opus-4-20250514", 0, 50_000, 200_000},
		{"claude-sonnet-4-6", 0, 50_000, 200_000},
		{"claude-sonnet-4-5-20250929", 0, 50_000, 200_000},
		{"claude-haiku-4-5-20251001", 0, 50_000, 200_000},
		{"claude-3-5-sonnet-20241022", 0, 50_000, 200_000},
		{"some-gateway-model", 0, 50_000, 200_000},
		// ... unless picked with [1m], or grown past 200k anyway.
		{"claude-sonnet-4-5[1m]", 0, 0, 1_000_000},
		{"claude-sonnet-4-6", 0, 291_768, 1_000_000},
		// The setting wins.
		{"claude-opus-5-5", 200_000, 150_000, 200_000},
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
	if got := FindTranscript(home, cwd, time.Time{}, nil); got != newer {
		t.Fatalf("FindTranscript = %q", got)
	}
	if got := FindTranscript(home, cwd, time.Time{}, map[string]bool{newer: true}); got != old {
		t.Errorf("a taken transcript was not skipped: %q", got)
	}
	if got := FindTranscript(home, cwd, fixedTime(2), nil); got != "" {
		t.Errorf("nothing was written since, yet %q was found", got)
	}
	if FindTranscript(home, "/nowhere", time.Time{}, nil) != "" {
		t.Error("unknown cwd should yield nothing")
	}

	tr := NewTracker()
	tick := clock(tr)
	var changes []Session
	tr.OnChange = func(s Session) { changes = append(changes, s) }
	tr.Observe("p1", "t1", true, cwd)
	tr.RefreshContexts(home, 0)
	if len(changes) != 1 {
		t.Fatalf("guessed before the hook had its chance: %+v", changes[len(changes)-1])
	}
	tick(transcriptRetry)
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

// project makes Claude Code's project directory for cwd under home.
func project(t *testing.T, home, cwd string) string {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", strings.ReplaceAll(cwd, "/", "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// session writes a transcript with a prompt and a context reading, last written at mtime.
func session(t *testing.T, dir, name, prompt string, tokens int, mtime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name+".jsonl")
	body := fmt.Sprintf(`{"type":"user","message":{"role":"user","content":%q}}`+"\n"+
		`{"type":"assistant","message":{"model":"m","usage":{"input_tokens":1,"cache_read_input_tokens":%d}}}`+"\n", prompt, tokens)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAGuessNeverTakesAnotherSessionsTranscript(t *testing.T) {
	home := t.TempDir()
	dir := project(t, home, "/home/x")
	tr := NewTracker()
	tick := clock(tr)
	start := tr.now()
	// The root agent lives in ~ too, and its transcript is the one written last.
	session(t, dir, "stale", "last week's session", 50_000, start.Add(-time.Hour))
	root := session(t, dir, "root", "sort my sessions", 150_000, start.Add(time.Second))
	tr.Hook("root", "t0", "SessionStart", []byte(`{"cwd":"/home/x","transcript_path":"`+root+`"}`))
	tr.Observe("p40", "t1", true, "/home/x")
	tick(transcriptRetry)
	tr.RefreshContexts(home, 0)
	for _, s := range tr.List() {
		if s.PaneID == "p40" && (s.TranscriptPath != "" || s.Title != "" || s.ContextPercent != 0) {
			t.Fatalf("p40 took a transcript that is not its own: %+v", s)
		}
	}
	// Once the session has written something, there is nothing left to doubt.
	own := session(t, dir, "own", "fix the bug", 20_000, tr.now().Add(time.Second))
	tick(transcriptRetry)
	tr.RefreshContexts(home, 0)
	for _, s := range tr.List() {
		if s.PaneID == "p40" && (s.TranscriptPath != own || s.Title != "fix the bug" || s.ContextPercent != 10) {
			t.Fatalf("p40 after writing: %+v", s)
		}
	}
}

func TestTwoSessionsInOneDirectoryAreNotGuessedAt(t *testing.T) {
	home := t.TempDir()
	dir := project(t, home, "/w")
	tr := NewTracker()
	tick := clock(tr)
	session(t, dir, "a", "one of them", 100, tr.now().Add(time.Second))
	tr.Observe("p1", "t1", true, "/w")
	tr.Observe("p2", "t1", true, "/w")
	tick(transcriptRetry)
	tr.RefreshContexts(home, 0)
	for _, s := range tr.List() {
		if s.TranscriptPath != "" {
			t.Errorf("%s guessed %s", s.PaneID, s.TranscriptPath)
		}
	}
}

// After a wate restart all panes come back at once, and the poller sees some of them before
// their hooks arrive. None of them may end up with another one's transcript.
func TestARestartDoesNotHandOutTranscripts(t *testing.T) {
	home := t.TempDir()
	dir := project(t, home, "/w")
	tr := NewTracker()
	tick := clock(tr)
	var changes []Session
	tr.OnChange = func(s Session) { changes = append(changes, s) }
	full := session(t, dir, "p41", "a long session", 198_000, tr.now().Add(time.Second))
	tr.Observe("p22", "t1", true, "/w")
	tr.RefreshContexts(home, 0)
	tick(5 * time.Second)
	tr.Hook("p41", "t1", "SessionStart", []byte(`{"source":"resume","cwd":"/w","transcript_path":"`+full+`"}`))
	tick(transcriptRetry)
	tr.RefreshContexts(home, 0)
	for _, c := range changes {
		if c.PaneID == "p22" && (c.TranscriptPath != "" || c.ContextPercent != 0) {
			t.Fatalf("p22 was handed p41's transcript: %+v", c)
		}
	}
}

func TestAHookTakesBackAGuessedTranscript(t *testing.T) {
	home := t.TempDir()
	dir := project(t, home, "/w")
	tr := NewTracker()
	tick := clock(tr)
	var changes []Session
	tr.OnChange = func(s Session) { changes = append(changes, s) }
	path := session(t, dir, "s", "somebody's prompt", 180_000, tr.now().Add(time.Second))
	tr.Observe("p1", "t1", true, "/w")
	tick(transcriptRetry)
	tr.RefreshContexts(home, 0)
	if s := tr.List()[0]; s.TranscriptPath != path || s.ContextPercent != 90 {
		t.Fatalf("the only candidate was not guessed: %+v", s)
	}
	// A pane the poller had not seen yet turns out to own it.
	tr.Hook("p2", "t1", "UserPromptSubmit", []byte(`{"cwd":"/w","transcript_path":"`+path+`","prompt":"x"}`))
	var p1 Session
	for _, c := range changes {
		if c.PaneID == "p1" {
			p1 = c
		}
	}
	if p1.TranscriptPath != "" || p1.Title != "" || p1.ContextPercent != 0 {
		t.Fatalf("p1 kept the guess: %+v", p1)
	}
	tr.RefreshContexts(home, 0)
	for _, s := range tr.List() {
		if s.PaneID == "p1" && s.TranscriptPath != "" {
			t.Fatalf("p1 guessed again at once: %+v", s)
		}
	}
}
