package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadProcState(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(pid, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, pid+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// As Claude Code 2.1 writes it, trimmed.
	write("41", `{"pid":41,"sessionId":"s-41","cwd":"/w","status":"shell","bridgeSessionId":"session_01ABC"}`)
	write("42", `{"pid":42,"sessionId":"s-42","status":"waiting","waitingFor":"permission prompt","bridgeSessionId":null}`)
	write("43", `{"pid":43,"status":"busy"}`)

	s, err := ReadProcState(home, 41)
	if err != nil || s.SessionID != "s-41" || !s.AtPrompt() {
		t.Fatalf("41: %+v %v", s, err)
	}
	if got := s.RemoteControlURL(); got != "https://claude.ai/code/session_01ABC" {
		t.Fatalf("url %q", got)
	}
	s, _ = ReadProcState(home, 42)
	if s.AtPrompt() || s.WaitingFor != "permission prompt" || s.RemoteControlURL() != "" {
		t.Fatalf("42: %+v", s)
	}
	if s, _ := ReadProcState(home, 43); s.AtPrompt() {
		t.Fatalf("a busy session at its prompt: %+v", s)
	}
	if _, err := ReadProcState(home, 44); err == nil {
		t.Fatal("no file, no error")
	}
	if s := (ProcState{Status: "idle"}); !s.AtPrompt() {
		t.Fatal("idle is at the prompt")
	}
}
