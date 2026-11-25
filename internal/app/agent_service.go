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

	"github.com/Gerry3010/wate/internal/agent"
	"github.com/Gerry3010/wate/internal/config"
)

// AgentService tracks Claude Code sessions per pane and tells the frontend about them.
type AgentService struct {
	tracker *agent.Tracker
	pty     *PtyService
	ctl     *CtlService
	cfg     func() config.Config
	notices *NoticeService

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

func NewAgentService(pty *PtyService, ctl *CtlService, cfg func() config.Config, notices *NoticeService, windows *WindowService) *AgentService {
	a := &AgentService{
		tracker: agent.NewTracker(), pty: pty, ctl: ctl, cfg: cfg, notices: notices, windows: windows,
		focus: map[WindowID]windowFocus{}, stop: make(chan struct{}),
	}
	a.tracker.OnChange = a.onChange
	ctl.OnHook = func(ev HookEvent) { a.tracker.Hook(ev.Pane, ev.Tab, ev.Event, ev.Data) }
	if notices != nil {
		notices.focused = a.anyFocused
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
	key := claudeNoticeKey(s.PaneID)
	if s.Status != agent.StatusWaiting && s.Status != agent.StatusDone {
		// Back at work on something new, or gone: whatever it was waiting for is over. wate's
		// own question is not something new; it leaves an unread "finished" standing.
		if a.notices != nil && (s.Status == agent.StatusIdle || (s.Phase == agent.PhaseWorking && !s.Quiet)) {
			a.notices.resolve(key)
		}
		return
	}
	if s.Quiet && s.Phase != agent.PhasePermission {
		// The turn answered a question wate itself typed in (a restart round): nobody waits.
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
	if !a.cfg().Claude.Notify || a.notices == nil {
		return
	}
	a.notices.post(claudeNotice(s, homeDir()))
}

// claudeNotice is the bell's word on a session that waits or has finished. With thirty
// sessions in the list, "Claude Code finished" says nothing, so the title is the session's
// own and the state goes into the body.
func claudeNotice(s agent.Session, home string) Notice {
	n := Notice{Key: claudeNoticeKey(s.PaneID), Kind: "waiting", Title: firstNonEmpty(s.Title, "Claude Code"), Pane: s.PaneID}
	state := "Waiting for you"
	if s.Status == agent.StatusDone {
		n.Kind, state = "finished", "Finished"
	}
	body := state
	if s.Message != "" && s.Message != state && s.Message != n.Title {
		body += ": " + s.Message
	}
	n.Body = notificationBody(body, s.Cwd, home)
	return n
}

// claudeNoticeKey groups a session's status notices: one entry per pane in the bell.
func claudeNoticeKey(pane string) string { return "claude:" + pane }

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

// anyFocused reports whether one of wate's windows has the focus.
func (a *AgentService) anyFocused() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, f := range a.focus {
		if f.focused {
			return true
		}
	}
	return false
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

// Notify puts a notice in the bell (and, with wate in the background, on the desktop). A
// newer notice with the same key replaces the older; a click leads to pane, if it names one.
func (a *AgentService) Notify(key, title, body, pane string) {
	if a.notices == nil {
		return
	}
	kind := "agent"
	switch {
	case strings.HasPrefix(key, rootNotificationPrefix):
		kind = "root"
	case strings.HasPrefix(key, "wate:"):
		kind = "wate"
	}
	a.notices.post(Notice{Key: key, Kind: kind, Title: title, Body: body, Pane: pane})
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
	if windowFocused && a.notices != nil {
		a.notices.focusGained()
		a.notices.seen(paneID)
	}
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
