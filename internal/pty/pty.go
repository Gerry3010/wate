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

	// Output hub: one reader, one subscriber at a time (see "output hub" below).
	hubMu   sync.Mutex
	cur     *subscriber
	backlog []byte
	eof     bool
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
	s.startPump()
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

// ---- output hub ---------------------------------------------------------
//
// One goroutine reads the PTY master, and exactly one subscriber at a time receives what it
// reads. This exists so a pane can move between windows: a second reader on the same master
// would split the byte stream unpredictably, and a reader that is simply abandoned is still
// blocked inside Read — it wakes on the next chunk, discovers its socket is gone, and returns
// having already consumed that chunk. Swapping the subscriber under a single reader loses
// nothing.

// backlogMax is how much output is kept while nobody is listening (a tab in flight between
// windows). Oldest goes first: a terminal cares about the end of the stream, not the start.
const backlogMax = 256 * 1024

type subscriber struct {
	ch   chan []byte
	gone chan struct{}
}

// Output is a subscription to a session's output.
type Output struct {
	// C carries the PTY's bytes and is closed when the child closes its side.
	C <-chan []byte
	// Gone is closed when another subscriber took over, so this one can stand down.
	Gone <-chan struct{}
}

// startPump begins the session's single reader.
func (s *Session) startPump() {
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := s.file.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				s.push(chunk)
			}
			if err != nil {
				s.hubMu.Lock()
				s.eof = true
				if s.cur != nil {
					close(s.cur.ch)
					s.cur = nil
				}
				s.hubMu.Unlock()
				return
			}
		}
	}()
}

// push hands a chunk to the current subscriber, waiting for it the way the old direct write
// to the socket did — the backpressure is deliberate. If the subscriber is replaced while we
// wait, the chunk goes to whoever replaced it instead of being dropped.
func (s *Session) push(chunk []byte) {
	for {
		s.hubMu.Lock()
		cur := s.cur
		if cur == nil {
			s.backlog = append(s.backlog, chunk...)
			if len(s.backlog) > backlogMax {
				s.backlog = append([]byte(nil), s.backlog[len(s.backlog)-backlogMax:]...)
			}
			s.hubMu.Unlock()
			return
		}
		s.hubMu.Unlock()
		select {
		case cur.ch <- chunk:
			return
		case <-cur.gone:
			// Taken over mid-send: hand the chunk to the new subscriber.
		}
	}
}

// Subscribe starts receiving this session's output, replacing whoever was receiving it. What
// arrived while nobody was listening is delivered first. The cancel function unsubscribes.
func (s *Session) Subscribe() (*Output, func()) {
	sub := &subscriber{ch: make(chan []byte, 64), gone: make(chan struct{})}
	s.hubMu.Lock()
	if s.cur != nil {
		close(s.cur.gone)
	}
	s.cur = sub
	backlog := s.backlog
	s.backlog = nil
	eof := s.eof
	s.hubMu.Unlock()

	if len(backlog) > 0 {
		sub.ch <- backlog // the channel is buffered and brand new, so this cannot block
	}
	if eof {
		s.hubMu.Lock()
		if s.cur == sub {
			close(sub.ch)
			s.cur = nil
		}
		s.hubMu.Unlock()
	}

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			s.hubMu.Lock()
			if s.cur == sub {
				close(sub.gone)
				s.cur = nil
			}
			s.hubMu.Unlock()
		})
	}
	return &Output{C: sub.ch, Gone: sub.gone}, cancel
}
