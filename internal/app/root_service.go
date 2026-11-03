package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Gerry3010/wate/internal/agent"
	"github.com/Gerry3010/wate/internal/config"
	"github.com/Gerry3010/wate/internal/ctl"
)

const (
	// rootNotificationPrefix marks the root agent's own notifications. Their ids do not name a
	// pane, so a click must not take the id for one.
	rootNotificationPrefix = "wate-root-"
	// rootTick is how often the session table is checked. The thresholds are minutes; five
	// seconds late is on time.
	rootTick = 5 * time.Second
	// maxPending caps what piles up while the root agent does not ask. The oldest go first: by
	// the time it asks again, its first look will be at the session table anyway.
	maxPending = 100
	// watcherStale is how long without a question from the watcher means it is not running. It
	// asks every couple of seconds while it waits.
	watcherStale = 15 * time.Second
	// hintEvery spaces out the reminder to restart it: a turn has a hook on every tool call.
	hintEvery = 2 * time.Minute
	// spawnGrace is how long a pane the root agent opened counts before its shell is seen.
	spawnGrace = 30 * time.Second
)

// RootService is wate's side of the root agent. It watches the session table for what deserves
// the root agent's attention, keeps that until the root agent asks, and carries out the few
// things only the root agent may ask for.
type RootService struct {
	agents *AgentService
	pty    *PtyService
	cfg    func() config.Config
	watch  *agent.Watch
	now    func() time.Time
	// procState is what Claude Code says about itself in a pane (AgentService.ProcState).
	procState func(pane string) (agent.ProcState, bool)

	mu       sync.Mutex
	pending  []agent.Event
	polledAt time.Time
	hintedAt time.Time
	// opened are the panes the root agent opened itself, and when, for [root] max_sessions.
	opened map[string]time.Time
}

func NewRootService(agents *AgentService, pty *PtyService, cfg func() config.Config) *RootService {
	return &RootService{
		agents: agents, pty: pty, cfg: cfg, watch: agent.NewWatch(), now: time.Now,
		procState: agents.ProcState, opened: map[string]time.Time{},
	}
}

// Run checks the session table until stop is closed.
func (r *RootService) Run(stop <-chan struct{}) {
	t := time.NewTicker(rootTick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			r.check(r.agents.List())
		}
	}
}

// check queues what sessions call for. With no root agent running nothing is checked, so
// nothing piles up for one that may never come — it looks at the whole table when it starts.
func (r *RootService) check(sessions []agent.Session) {
	cfg := r.cfg().Root
	root := r.pty.RootPane()
	if !cfg.Enabled || root == "" {
		return
	}
	rules := agent.Rules{
		Waiting:        time.Duration(cfg.WaitingMinutes) * time.Minute,
		ContextPercent: cfg.ContextPercent,
	}
	events := r.watch.Check(sessions, r.now(), rules, func(pane string) bool {
		return pane == root || (r.agents != nil && r.agents.InView(pane))
	})
	if len(events) == 0 {
		return
	}
	r.mu.Lock()
	r.pending = append(r.pending, events...)
	if over := len(r.pending) - maxPending; over > 0 {
		r.pending = r.pending[over:]
	}
	r.mu.Unlock()
}

// Attention hands over everything pending and empties the queue. Being asked is also how wate
// knows the watcher is running.
func (r *RootService) Attention() []agent.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.polledAt = r.now()
	out := r.pending
	r.pending = nil
	if out == nil {
		out = []agent.Event{}
	}
	return out
}

// RootSession is a session as the root agent is shown it.
type RootSession struct {
	agent.Session
	UnattendedMinutes int  `json:"unattended_minutes"`
	OpenedByRoot      bool `json:"opened_by_root"`
	IsRoot            bool `json:"is_root"`
	// RemoteControl is on when the session can be continued at RemoteControlURL as well.
	RemoteControl    bool   `json:"remote_control"`
	RemoteControlURL string `json:"remote_control_url,omitempty"`
}

// Sessions is the session table, every window's, for the root agent.
func (r *RootService) Sessions() []RootSession {
	return r.sessions(r.agents.List())
}

func (r *RootService) sessions(list []agent.Session) []RootSession {
	root := r.pty.RootPane()
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RootSession, 0, len(list))
	for _, s := range list {
		rs := RootSession{
			Session:           s,
			UnattendedMinutes: int(s.UnattendedFor(now) / time.Minute),
			OpenedByRoot:      !r.opened[s.PaneID].IsZero(),
			IsRoot:            s.PaneID == root,
		}
		if st, ok := r.procState(s.PaneID); ok {
			rs.RemoteControlURL = st.RemoteControlURL()
			rs.RemoteControl = rs.RemoteControlURL != ""
		}
		out = append(out, rs)
	}
	return out
}

// mayOpen says whether the root agent may open one more session.
func (r *RootService) mayOpen() error {
	limit := r.cfg().Root.MaxSessions
	if limit == 0 {
		return errors.New("opening sessions is switched off ([root] max_sessions = 0)")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for pane, at := range r.opened {
		// A pane only counts as gone once its shell had time to start: the window answers
		// before the spawn is through.
		if _, alive := r.pty.TabForPane(pane); !alive && now.Sub(at) > spawnGrace {
			delete(r.opened, pane)
		}
	}
	if len(r.opened) >= limit {
		return fmt.Errorf("%d sessions you opened are still running, which is the limit ([root] max_sessions); "+
			"wait for one to end or ask the user", len(r.opened))
	}
	return nil
}

// Opened records a pane the root agent opened.
func (r *RootService) Opened(pane string) {
	r.mu.Lock()
	r.opened[pane] = r.now()
	r.mu.Unlock()
}

// Notify puts up the root agent's notification; a click leads to pane, if it names one.
// One per pane: a newer word about the same session replaces the older.
func (r *RootService) Notify(title, body, pane string) {
	tab := ""
	id := rootNotificationPrefix + "general"
	if pane != "" {
		tab, _ = r.pty.TabForPane(pane)
		id = rootNotificationPrefix + pane
	}
	if title == "" {
		title = "Root agent"
	}
	r.agents.Notify(id, title, body, pane, tab)
}

// Hint is what the root agent is told on one of its own hooks, "" for nothing: that its
// watcher is not running. An idle root agent fires no hooks, so this catches a watcher that
// died while it was busy; the watcher ending is what wakes an idle one.
func (r *RootService) Hint(token, event, watchCommand string) string {
	if !ctl.HookTellsClaude(event) || !r.pty.IsRootToken(token) {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if now.Sub(r.polledAt) < watcherStale || (!r.hintedAt.IsZero() && now.Sub(r.hintedAt) < hintEvery) {
		return ""
	}
	r.hintedAt = now
	return "wate: your attention watcher is not running, so you will not hear about sessions that " +
		"need attention. Start it again as a background command: " + watchCommand
}

// firstLine is the start of s, for a notification body.
func firstLine(s string, n int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return string(r)
}

// RootHost is the saved window that hosted the root agent last time, "" when none did. Its
// tab is not saved (it is started afresh), so the window says so itself.
func RootHost(blobs []WindowBlob) string {
	for _, b := range blobs {
		var w struct {
			Root bool `json:"root"`
		}
		if json.Unmarshal([]byte(b.Data), &w) == nil && w.Root {
			return b.Key
		}
	}
	return ""
}
