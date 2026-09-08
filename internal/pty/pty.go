// Package pty spawns and tracks pseudo-terminal sessions (one per terminal pane).
package pty

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/google/uuid"
)

// SpawnOptions describes the process to start inside a new PTY.
type SpawnOptions struct {
	// Command is argv; when empty the user's login shell is used.
	Command []string
	// Cwd is the working directory; empty means the current process' cwd.
	Cwd string
	// Env entries (KEY=VALUE) appended to the inherited environment.
	Env  []string
	Cols uint16
	Rows uint16
}

// Session is a running process attached to a PTY.
type Session struct {
	ID   string
	cmd  *exec.Cmd
	file *os.File

	done     chan struct{}
	exitCode int
	exitErr  error
}

// File returns the PTY master; read from it for output, write to it for input.
func (s *Session) File() *os.File { return s.file }

// Pid is the process id of the spawned child (the shell).
func (s *Session) Pid() int { return s.cmd.Process.Pid }

// Done is closed once the child has exited.
func (s *Session) Done() <-chan struct{} { return s.done }

// ExitCode is valid after Done is closed.
func (s *Session) ExitCode() int { return s.exitCode }

// Resize updates the PTY window size and signals the child with SIGWINCH.
func (s *Session) Resize(cols, rows uint16) error {
	return pty.Setsize(s.file, &pty.Winsize{Cols: cols, Rows: rows})
}

// Kill terminates the child (SIGHUP first, like a closed terminal) and closes the PTY.
func (s *Session) Kill() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(syscall.SIGHUP)
	}
	_ = s.file.Close()
}

// Cwd returns the child's current working directory (best effort, platform specific).
func (s *Session) Cwd() (string, error) { return processCwd(s.Pid()) }

// Manager owns all sessions.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewManager() *Manager {
	return &Manager{sessions: map[string]*Session{}}
}

// Spawn starts a new session.
func (m *Manager) Spawn(opts SpawnOptions) (*Session, error) {
	argv := opts.Command
	if len(argv) == 0 {
		argv = []string{DefaultShell()}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = opts.Cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=wate")
	cmd.Env = append(cmd.Env, opts.Env...)

	cols, rows := opts.Cols, opts.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, fmt.Errorf("spawn %v: %w", argv, err)
	}

	s := &Session{ID: uuid.NewString(), cmd: cmd, file: f, done: make(chan struct{})}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()

	go func() {
		err := cmd.Wait()
		s.exitErr = err
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			s.exitCode = ee.ExitCode()
		}
		_ = f.Close()
		m.mu.Lock()
		delete(m.sessions, s.ID)
		m.mu.Unlock()
		close(s.done)
	}()
	return s, nil
}

// Get looks up a live session.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Shutdown kills every session and waits (bounded by ctx) for them to exit.
func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.RLock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.RUnlock()
	for _, s := range all {
		s.Kill()
	}
	for _, s := range all {
		select {
		case <-s.Done():
		case <-ctx.Done():
			return
		}
	}
}

// DefaultShell returns $SHELL or a sensible fallback.
func DefaultShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	for _, c := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "/bin/sh"
}
