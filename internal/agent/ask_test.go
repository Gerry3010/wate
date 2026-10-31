package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func transcript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const (
	prompt   = `{"type":"user","message":{"role":"user","content":"push it"}}`
	thinking = `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hm"}]}}`
)

func say(text string) string {
	return `{"type":"assistant","message":{"content":[{"type":"text","text":` + quote(text) + `}]}}`
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }

func TestAskingIsReadFromTheLastWord(t *testing.T) {
	for _, c := range []struct {
		name  string
		lines []string
		want  bool
	}{
		{"a statement", []string{prompt, say("Done, all tests pass.")}, false},
		{"a question", []string{prompt, say("Done. Shall I push it?")}, true},
		{"a question in bold", []string{prompt, say("Shall I push it?**")}, true},
		{"a question earlier in the turn", []string{prompt, say("Push it?"), say("Pushed.")}, false},
		{"text then thinking", []string{prompt, say("Which branch?"), thinking}, true},
		{"answered since", []string{say("Which branch?"), prompt}, false},
		{"a hook note after it", []string{prompt, say("Which branch?"), `{"type":"user","isMeta":true,"message":{"content":"hook said hi"}}`}, true},
		{"an open AskUserQuestion", []string{prompt, `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"AskUserQuestion","input":{}}]}}`}, true},
		{"a tool waiting for permission", []string{prompt, `{"type":"assistant","message":{"content":[{"type":"text","text":"Let me check?"},{"type":"tool_use","name":"Bash","input":{}}]}}`}, false},
		{"an answered AskUserQuestion", []string{`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"AskUserQuestion"}]}}`, `{"type":"user","message":{"content":[{"type":"tool_result"}]}}`}, false},
	} {
		if got := ReadAsking(transcript(t, c.lines...)); got != c.want {
			t.Errorf("%s: asking = %v, want %v", c.name, got, c.want)
		}
	}
	if ReadAsking("") || ReadAsking(filepath.Join(t.TempDir(), "missing.jsonl")) {
		t.Error("no transcript read as a question")
	}
}

func TestAQuestionLastsUntilTheNextPrompt(t *testing.T) {
	tr := NewTracker()
	path := transcript(t, prompt, say("Shall I push it?"))
	tr.Hook("p1", "t1", "UserPromptSubmit", []byte(`{"prompt":"push it","transcript_path":"`+path+`"}`))
	tr.Hook("p1", "t1", "Stop", nil)
	if !tr.List()[0].Asking {
		t.Fatal("a turn ending on a question was not marked")
	}
	// Looking at the pane is not answering it.
	tr.Acknowledge("p1")
	if !tr.List()[0].Asking {
		t.Error("looking at the pane dropped the question")
	}
	tr.Hook("p1", "t1", "UserPromptSubmit", []byte(`{"prompt":"yes"}`))
	if tr.List()[0].Asking {
		t.Error("the answer did not clear the question")
	}
}
