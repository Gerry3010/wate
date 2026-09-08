package pty

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSpawnEchoAndExit(t *testing.T) {
	m := NewManager()
	s, err := m.Spawn(SpawnOptions{Command: []string{"/bin/sh", "-c", "echo hello-$YATE_TEST; exit 3"}, Env: []string{"YATE_TEST=ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Get(s.ID); !ok {
		t.Fatal("session not registered")
	}
	r := bufio.NewReader(s.File())
	line, _ := r.ReadString('\n')
	if !strings.Contains(line, "hello-ok") {
		t.Fatalf("unexpected output %q", line)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("child did not exit")
	}
	if s.ExitCode() != 3 {
		t.Fatalf("exit code = %d, want 3", s.ExitCode())
	}
	if _, ok := m.Get(s.ID); ok {
		t.Fatal("session still registered after exit")
	}
}

func TestCwdAndResize(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	s, err := m.Spawn(SpawnOptions{Command: []string{"/bin/sh"}, Cwd: dir, Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Kill()
	cwd, err := s.Cwd()
	if err != nil {
		t.Fatal(err)
	}
	real, _ := os.Readlink(dir)
	if cwd != dir && cwd != real && !strings.HasSuffix(cwd, strings.TrimPrefix(dir, "/private")) {
		t.Fatalf("cwd = %q, want %q", cwd, dir)
	}
	if err := s.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.Shutdown(ctx)
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not terminate session")
	}
}
