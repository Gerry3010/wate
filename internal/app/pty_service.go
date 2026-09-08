// Package app holds the Wails-bound services that make up wate's backend API.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/wate/internal/config"
	"github.com/Gerry3010/wate/internal/pty"
	"github.com/Gerry3010/wate/internal/shell"
	"github.com/Gerry3010/wate/internal/wsbridge"
)

// PtyService spawns terminal sessions for the frontend.
type PtyService struct {
	sessions *pty.Manager
	bridge   *wsbridge.Server
	cfg      func() config.Config

	mu     sync.RWMutex
	socket string
	// byPane maps WATE_PANE_ID → session id so the control socket can address panes.
	byPane map[string]string
	tabOf  map[string]string
}

func NewPtyService(cfg func() config.Config) *PtyService {
	return &PtyService{sessions: pty.NewManager(), cfg: cfg, byPane: map[string]string{}, tabOf: map[string]string{}}
}

func (p *PtyService) ServiceName() string { return "PtyService" }

func (p *PtyService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	b, err := wsbridge.New(p.sessions)
	if err != nil {
		return fmt.Errorf("wsbridge: %w", err)
	}
	p.bridge = b
	if err := shell.Install(shellDir()); err != nil {
		slog.Warn("shell integration not installed", "err", err)
	}
	return nil
}

func shellDir() string { return filepath.Join(config.Dir(), "shell") }

func (p *PtyService) ServiceShutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p.sessions.Shutdown(ctx)
	return p.bridge.Close()
}

// SpawnRequest comes from the frontend when a pane is created.
type SpawnRequest struct {
	// Command overrides the configured shell (used for the Claude launcher).
	Command []string `json:"command"`
	Cwd     string   `json:"cwd"`
	Cols    int      `json:"cols"`
	Rows    int      `json:"rows"`
	PaneID  string   `json:"paneId"`
	TabID   string   `json:"tabId"`
}

// SpawnResult tells the frontend where to connect.
type SpawnResult struct {
	ID    string `json:"id"`
	Pid   int    `json:"pid"`
	Port  int    `json:"port"`
	Token string `json:"token"`
	// URL is the ready-to-dial websocket address.
	URL string `json:"url"`
}

// Spawn starts a shell (or the given command) in a new PTY.
func (p *PtyService) Spawn(req SpawnRequest) (SpawnResult, error) {
	cfg := p.cfg()
	cmd := req.Command
	if len(cmd) == 0 {
		shell := cfg.General.Shell
		if shell == "" {
			shell = pty.DefaultShell()
		}
		cmd = append([]string{shell}, cfg.General.ShellArgs...)
	}
	cwd := req.Cwd
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		cwd, _ = os.UserHomeDir()
	}
	p.mu.RLock()
	socket := p.socket
	p.mu.RUnlock()
	env := []string{
		"WATE_PANE_ID=" + req.PaneID,
		"WATE_TAB_ID=" + req.TabID,
		"WATE_SOCKET=" + socket,
	}
	if cfg.General.ShellIntegration {
		env = append(env, shell.Env(cmd[0], shellDir())...)
	}
	s, err := p.sessions.Spawn(pty.SpawnOptions{
		Command: cmd,
		Cwd:     cwd,
		Cols:    uint16(req.Cols),
		Rows:    uint16(req.Rows),
		Env:     env,
	})
	if err != nil {
		return SpawnResult{}, err
	}
	p.mu.Lock()
	p.byPane[req.PaneID] = s.ID
	p.tabOf[req.PaneID] = req.TabID
	p.mu.Unlock()
	go func() {
		<-s.Done()
		p.mu.Lock()
		if p.byPane[req.PaneID] == s.ID {
			delete(p.byPane, req.PaneID)
			delete(p.tabOf, req.PaneID)
		}
		p.mu.Unlock()
	}()
	return SpawnResult{
		ID:    s.ID,
		Pid:   s.Pid(),
		Port:  p.bridge.Port(),
		Token: p.bridge.Token(),
		URL:   fmt.Sprintf("ws://127.0.0.1:%d/pty/%s?token=%s", p.bridge.Port(), s.ID, p.bridge.Token()),
	}, nil
}

// Cwd returns the session's current working directory (best effort).
func (p *PtyService) Cwd(id string) (string, error) {
	s, ok := p.sessions.Get(id)
	if !ok {
		return "", fmt.Errorf("no session %s", id)
	}
	return s.Cwd()
}

// Kill terminates a session (pane closed).
func (p *PtyService) Kill(id string) {
	if s, ok := p.sessions.Get(id); ok {
		s.Kill()
	}
}

// SetSocketPath records the control socket for new shells' environment.
func (p *PtyService) SetSocketPath(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.socket = path
}

// SessionForPane maps a WATE_PANE_ID to its session id.
func (p *PtyService) SessionForPane(paneID string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	id, ok := p.byPane[paneID]
	return id, ok
}

// WriteToPane feeds text to the pane's PTY as if typed.
func (p *PtyService) WriteToPane(paneID, text string) error {
	id, ok := p.SessionForPane(paneID)
	if !ok {
		return fmt.Errorf("no such pane %q", paneID)
	}
	s, ok := p.sessions.Get(id)
	if !ok {
		return fmt.Errorf("pane %q has no live session", paneID)
	}
	_, err := s.File().WriteString(text)
	return err
}

// Pid of the shell in a pane (for process-tree inspection).
func (p *PtyService) PidForPane(paneID string) (int, bool) {
	id, ok := p.SessionForPane(paneID)
	if !ok {
		return 0, false
	}
	s, ok := p.sessions.Get(id)
	if !ok {
		return 0, false
	}
	return s.Pid(), true
}

// PaneInfo describes a live pane for the agent poller.
type PaneInfo struct {
	PaneID    string
	TabID     string
	SessionID string
	Pid       int
}

// Panes lists every pane with a live shell.
func (p *PtyService) Panes() []PaneInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]PaneInfo, 0, len(p.byPane))
	for paneID, sid := range p.byPane {
		s, ok := p.sessions.Get(sid)
		if !ok {
			continue
		}
		out = append(out, PaneInfo{PaneID: paneID, TabID: p.tabOf[paneID], SessionID: sid, Pid: s.Pid()})
	}
	return out
}
