package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentStateRoundTrip(t *testing.T) {
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	s := &AgentStateService{}

	const id = "11111111-2222-3333-4444-555555555555"
	if err := s.Set(SavedAgentSession{SessionID: id, Label: "Perf-Jagd", Cwd: "/work", Favorite: true}); err != nil {
		t.Fatalf("set: %v", err)
	}
	got := s.List()
	if len(got.Sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(got.Sessions))
	}
	if e := got.Sessions[0]; e.Label != "Perf-Jagd" || !e.Favorite || e.Cwd != "/work" || e.SeenAt == "" {
		t.Fatalf("round trip lost something: %+v", e)
	}

	// Cwd and Title are only known while a session is live; a later write must not blank them.
	if err := s.Set(SavedAgentSession{SessionID: id, Label: "Perf-Jagd", Favorite: false}); err != nil {
		t.Fatalf("set again: %v", err)
	}
	if e := s.List().Sessions[0]; e.Cwd != "/work" || e.Favorite {
		t.Fatalf("partial update went wrong: %+v", e)
	}

	// Neither named nor favourited is nothing worth remembering.
	if err := s.Set(SavedAgentSession{SessionID: id}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if n := len(s.List().Sessions); n != 0 {
		t.Fatalf("want the entry dropped, got %d", n)
	}
}

func TestAgentStateCollapsed(t *testing.T) {
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	s := &AgentStateService{}
	if err := s.SetCollapsed("favorites", true); err != nil {
		t.Fatalf("collapse: %v", err)
	}
	if !s.List().Collapsed["favorites"] {
		t.Fatal("collapsed state did not survive")
	}
	if s.List().Collapsed["sessions"] {
		t.Fatal("an untouched section must not read as collapsed")
	}
}

func TestDeleteTranscriptOnlyTouchesClaudesOwnFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s := &AgentStateService{}

	const id = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	dir := filepath.Join(home, ".claude", "projects", "-work")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(live, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteTranscript(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Fatal("transcript should be gone")
	}

	// Anything that is not a session id must not reach the file system at all.
	bystander := filepath.Join(home, "precious.jsonl")
	if err := os.WriteFile(bystander, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"../../precious",
		"/etc/passwd",
		"precious",
		"",
		"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/../../../precious",
	} {
		if err := s.DeleteTranscript(bad); err == nil {
			t.Fatalf("%q was accepted as a session id", bad)
		}
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Fatal("a file outside Claude's projects was touched")
	}
}

func TestUnderTranscriptRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".claude", "projects")

	if err := underTranscriptRoot(filepath.Join(root, "-work", "a.jsonl")); err != nil {
		t.Fatalf("a real transcript was rejected: %v", err)
	}
	for _, bad := range []string{
		"",
		filepath.Join(home, "elsewhere.jsonl"),
		filepath.Join(root, "-work", "notes.txt"),
		filepath.Join(root+"-evil", "a.jsonl"), // prefix match must not be a substring match
	} {
		if err := underTranscriptRoot(bad); err == nil {
			t.Fatalf("%q was allowed", bad)
		}
	}
}
