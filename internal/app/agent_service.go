package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

	windows *WindowService

	mu sync.Mutex
	// focus is per window: with two of them, a single pair would be last-writer-wins and the
	// unfocused window's blur would silence notifications for the focused one.
	focus map[WindowID]windowFocus
	stop  chan struct{}
}

type windowFocus struct {
	pane    string
	focused bool
}

func NewAgentService(pty *PtyService, ctl *CtlService, cfg func() config.Config, notify *notifications.NotificationService, windows *WindowService) *AgentService {
	a := &AgentService{
		tracker: agent.NewTracker(), pty: pty, ctl: ctl, cfg: cfg, notify: notify, windows: windows,
		focus: map[WindowID]windowFocus{}, stop: make(chan struct{}),
	}
	a.tracker.OnChange = a.onChange
	ctl.OnHook = func(ev HookEvent) { a.tracker.Hook(ev.Pane, ev.Tab, ev.Event, ev.Data) }
	if notify != nil {
		notify.OnNotificationResponse(a.onNotificationResponse)
	}
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
	// Pane → the Claude pid seen last time: checking that one process is still alive is two
	// small /proc reads, where the tree walk costs a directory listing per process.
	known := map[string]int{}
	for {
		select {
		case <-a.stop:
			return
		case <-t.C:
			seen := make(map[string]bool, len(known))
			for _, p := range a.pty.Panes() {
				seen[p.PaneID] = true
				pid := known[p.PaneID]
				if pid == 0 || !agent.IsLiveClaude(pid) {
					pid = 0
					if pids := agent.ClaudePidsUnder(p.Pid); len(pids) > 0 {
						pid = pids[0]
					}
					known[p.PaneID] = pid
				}
				cwd := ""
				if pid != 0 {
					cwd, _ = a.pty.Cwd(p.SessionID)
				}
				a.tracker.Observe(p.PaneID, p.TabID, pid != 0, cwd)
			}
			for pane := range known {
				if !seen[pane] {
					delete(known, pane)
				}
			}
			a.tracker.RefreshContexts(homeDir(), a.cfg().Claude.ContextWindow)
		}
	}
}

func (a *AgentService) onChange(s agent.Session) {
	// Stays a broadcast: the sidebar deliberately lists sessions from every window.
	application.Get().Event.Emit("agent:status", a.stamp(s))
	if s.Status != agent.StatusWaiting && s.Status != agent.StatusDone {
		return
	}
	if a.InView(s.PaneID) {
		// The user is looking at it; no need to shout.
		a.tracker.Acknowledge(s.PaneID)
		return
	}
	if !worthNotifying(s, a.pty.RootPane()) {
		return
	}
	if !a.cfg().Claude.Notify {
		return
	}
	title := "Claude Code is waiting"
	if s.Status == agent.StatusDone {
		title = "Claude Code finished"
	}
	a.Notify("wate-"+s.PaneID, title, notificationBody(s.Message, s.Cwd, homeDir()), s.PaneID, s.TabID)
}

// worthNotifying says whether a waiting or finished session is news. For the root agent it
// rarely is: it finishes a turn every time it has dealt with something, and a minute later
// Claude Code's idle reminder turns it to waiting. Only it being stuck on a prompt of its own
// is worth a word.
func worthNotifying(s agent.Session, rootPane string) bool {
	if s.PaneID == "" || s.PaneID != rootPane {
		return true
	}
	return s.Status == agent.StatusWaiting && s.Phase == agent.PhasePermission
}

// InView reports whether a focused window is showing this very pane.
func (a *AgentService) InView(pane string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, f := range a.focus {
		if f.focused && f.pane == pane {
			return true
		}
	}
	return false
}

// Notify puts up a desktop notification; a click on it leads to pane (if any). Notifications
// with the same id replace each other.
func (a *AgentService) Notify(id, title, body, pane, tab string) {
	if a.notify == nil {
		return
	}
	err := a.notify.SendNotification(notifications.NotificationOptions{
		ID:    id,
		Title: title,
		Body:  body,
		// Read back when it is clicked, to find the pane again.
		Data: map[string]interface{}{"pane": pane, "tab": tab},
	})
	if err != nil {
		slog.Debug("notification", "err", err)
	}
}

// notificationBody says what the session was doing and where: "<message> — ~/<dir>", leaving
// out whichever half is missing.
func notificationBody(message, cwd, home string) string {
	if home != "" && (cwd == home || strings.HasPrefix(cwd, home+string(filepath.Separator))) {
		cwd = "~" + cwd[len(home):]
	}
	var parts []string
	for _, p := range []string{message, cwd} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " — ")
}

// onNotificationResponse brings the pane a clicked notification is about to the front: its
// window, its tab, and the focus in it.
func (a *AgentService) onNotificationResponse(r notifications.NotificationResult) {
	if r.Error != nil || r.Response.ActionIdentifier != notifications.DefaultActionIdentifier {
		return
	}
	pane, _ := r.Response.UserInfo["pane"].(string)
	tab, _ := r.Response.UserInfo["tab"].(string)
	if pane == "" && !strings.HasPrefix(r.Response.ID, rootNotificationPrefix) {
		pane = strings.TrimPrefix(r.Response.ID, "wate-")
	}
	if pane == "" || a.windows == nil {
		return
	}
	// The pane may have moved tab or window since the notification went out; the window
	// service knows where it is now, and the frontend finds the pane by its id.
	win := a.windows.windowForTab(tab, pane)
	if win == "" {
		return
	}
	if err := a.windows.FocusPane(string(win), tab, pane); err != nil {
		slog.Debug("notification click", "pane", pane, "err", err)
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

// ClaudePid is the Claude Code process running in a pane, 0 when there is none.
func (a *AgentService) ClaudePid(pane string) int {
	if a.pty == nil {
		return 0
	}
	pid, ok := a.pty.PidForPane(pane)
	if !ok {
		return 0
	}
	if pids := agent.ClaudePidsUnder(pid); len(pids) > 0 {
		return pids[0]
	}
	return 0
}

// ProcState is what Claude Code says about itself in a pane; false when nothing runs there or
// it says nothing.
func (a *AgentService) ProcState(pane string) (agent.ProcState, bool) {
	pid := a.ClaudePid(pane)
	if pid == 0 {
		return agent.ProcState{}, false
	}
	s, err := agent.ReadProcState(homeDir(), pid)
	return s, err == nil
}

// List returns all known sessions, from every window (sidebar initial state).
func (a *AgentService) List() []agent.Session {
	list := a.tracker.List()
	for i := range list {
		list[i] = a.stamp(list[i])
	}
	return list
}

// stamp says which window a session is showing in right now. Looked up per hand-out rather
// than stored, because a tab can change windows while its session runs.
func (a *AgentService) stamp(s agent.Session) agent.Session {
	if a.windows != nil {
		s.WindowID = string(a.windows.windowForTab(s.TabID, s.PaneID))
	}
	return s
}

// SetFocus tells the backend which pane the user is looking at; waiting/done states are cleared.
// The window comes from the context, so each one reports only for itself.
func (a *AgentService) SetFocus(ctx context.Context, paneID string, windowFocused bool) {
	id := WindowID("")
	if a.windows != nil {
		id = WindowID(a.windows.Bootstrap(ctx).WindowID)
	}
	a.mu.Lock()
	a.focus[id] = windowFocus{pane: paneID, focused: windowFocused}
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
