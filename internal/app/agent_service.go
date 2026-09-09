package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/Gerry3010/wate/internal/agent"
	"github.com/Gerry3010/wate/internal/config"
)

// AgentService tracks Claude Code sessions per pane and tells the frontend about them.
type AgentService struct {
	tracker *agent.Tracker
	pty     *PtyService
	ctl     *CtlService
	cfg     func() config.Config
	notify  *notifications.NotificationService

	mu            sync.Mutex
	focusedPane   string
	windowFocused bool
	stop          chan struct{}
}

func NewAgentService(pty *PtyService, ctl *CtlService, cfg func() config.Config, notify *notifications.NotificationService) *AgentService {
	a := &AgentService{tracker: agent.NewTracker(), pty: pty, ctl: ctl, cfg: cfg, notify: notify, windowFocused: true, stop: make(chan struct{})}
	a.tracker.OnChange = a.onChange
	ctl.OnHook = func(ev HookEvent) { a.tracker.Hook(ev.Pane, ev.Tab, ev.Event, ev.Data) }
	return a
}

func (a *AgentService) ServiceName() string { return "AgentService" }

func (a *AgentService) ServiceStartup(context.Context, application.ServiceOptions) error {
	go a.poll()
	return nil
}

func (a *AgentService) ServiceShutdown() error {
	close(a.stop)
	return nil
}

// poll watches every pane's process tree so sessions show up even without hooks installed.
func (a *AgentService) poll() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-a.stop:
			return
		case <-t.C:
			for _, p := range a.pty.Panes() {
				running := agent.ClaudeRunningUnder(p.Pid)
				cwd := ""
				if running {
					cwd, _ = a.pty.Cwd(p.SessionID)
				}
				a.tracker.Observe(p.PaneID, p.TabID, running, cwd)
			}
			a.tracker.RefreshContexts(homeDir(), a.cfg().Claude.ContextWindow)
		}
	}
}

func (a *AgentService) onChange(s agent.Session) {
	application.Get().Event.Emit("agent:status", s)
	if s.Status != agent.StatusWaiting && s.Status != agent.StatusDone {
		return
	}
	a.mu.Lock()
	quiet := a.windowFocused && a.focusedPane == s.PaneID
	a.mu.Unlock()
	if quiet {
		// The user is looking at it; no need to shout.
		a.tracker.Acknowledge(s.PaneID)
		return
	}
	if !a.cfg().Claude.Notify || a.notify == nil {
		return
	}
	title := "Claude Code is waiting"
	if s.Status == agent.StatusDone {
		title = "Claude Code finished"
	}
	err := a.notify.SendNotification(notifications.NotificationOptions{
		ID:    "wate-" + s.PaneID,
		Title: title,
		Body:  fmt.Sprintf("%s — %s", s.Message, s.Cwd),
	})
	if err != nil {
		slog.Debug("notification", "err", err)
	}
}

// StopSessions ends every Claude Code session running in a pane and waits for it (bounded by
// ctx). wate is quitting: a SIGHUP from the closing PTY would cut the agent off mid-write,
// SIGTERM lets it flush its transcript and run its SessionEnd hooks.
func (a *AgentService) StopSessions(ctx context.Context) {
	var pids []int
	for _, p := range a.pty.Panes() {
		pids = append(pids, agent.ClaudePidsUnder(p.Pid)...)
	}
	if len(pids) == 0 {
		return
	}
	timeout := 3 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		if left := time.Until(dl); left < timeout {
			timeout = left
		}
	}
	signalled, left := agent.StopProcesses(pids, timeout)
	slog.Info("claude sessions stopped", "signalled", signalled, "still_running", left)
}

// List returns all known sessions (sidebar initial state).
func (a *AgentService) List() []agent.Session { return a.tracker.List() }

// SetFocus tells the backend which pane the user is looking at; waiting/done states are cleared.
func (a *AgentService) SetFocus(paneID string, windowFocused bool) {
	a.mu.Lock()
	a.focusedPane = paneID
	a.windowFocused = windowFocused
	a.mu.Unlock()
	if windowFocused && paneID != "" {
		a.tracker.Acknowledge(paneID)
	}
}

// Forget drops a closed pane.
func (a *AgentService) Forget(paneID string) { a.tracker.Forget(paneID) }

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}
