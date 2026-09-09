// Package agent tracks Claude Code sessions running inside wate panes: which panes
// run one, and whether it is busy, waiting for the user, or done.
package agent

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

// Status of a Claude session in a pane.
type Status string

const (
	StatusIdle    Status = "idle"    // no claude process
	StatusRunning Status = "running" // claude is working
	StatusWaiting Status = "waiting" // claude asked something (permission, input)
	StatusDone    Status = "done"    // claude finished its turn
)

// Session is the state wate keeps per pane.
type Session struct {
	PaneID    string    `json:"pane_id"`
	TabID     string    `json:"tab_id"`
	Status    Status    `json:"status"`
	SessionID string    `json:"session_id"`
	Cwd       string    `json:"cwd"`
	Message   string    `json:"message"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Source is "hook" when hooks deliver precise events, "proc" when only process detection sees it.
	Source string `json:"source"`
	// TranscriptPath is Claude Code's session transcript (from hooks, or guessed from cwd).
	TranscriptPath string `json:"transcript_path"`
	// Context is the context-window usage read from the transcript (zero when unknown).
	Context Context `json:"context"`
	// ContextPercent is Context.Tokens / Context.Window in percent (0 when unknown).
	ContextPercent int `json:"context_percent"`
	// transcriptSize is the file size at the last context read (skip unchanged files).
	transcriptSize int64
}

// Tracker is the in-memory session table.
type Tracker struct {
	mu       sync.Mutex
	sessions map[string]*Session
	now      func() time.Time
	// OnChange is called (outside the lock) whenever a session changes.
	OnChange func(Session)
}

func NewTracker() *Tracker {
	return &Tracker{sessions: map[string]*Session{}, now: time.Now}
}

// hookPayload is the subset of Claude Code's hook JSON we care about.
type hookPayload struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Message   string `json:"message"`
	Title     string `json:"title"`
	HookEvent string `json:"hook_event_name"`
	Prompt    string `json:"prompt"`
	Transcript string `json:"transcript_path"`
}

// Hook applies a Claude Code hook event to the pane's session.
func (t *Tracker) Hook(paneID, tabID, event string, data []byte) {
	var p hookPayload
	_ = json.Unmarshal(data, &p)
	t.mu.Lock()
	s := t.get(paneID, tabID)
	s.Source = "hook"
	if p.SessionID != "" {
		s.SessionID = p.SessionID
	}
	if p.Cwd != "" {
		s.Cwd = p.Cwd
	}
	if p.Transcript != "" && p.Transcript != s.TranscriptPath {
		s.TranscriptPath = p.Transcript
		s.transcriptSize = -1
	}
	switch event {
	case "SessionStart":
		s.Status = StatusRunning
		s.Message = ""
		s.StartedAt = t.now()
	case "UserPromptSubmit":
		s.Status = StatusRunning
		s.Message = truncate(p.Prompt, 120)
	case "Notification":
		s.Status = StatusWaiting
		s.Message = firstNonEmpty(p.Message, p.Title, "Claude is waiting for you")
	case "Stop":
		s.Status = StatusDone
		s.Message = firstNonEmpty(s.Message, "Finished")
	case "SessionEnd":
		delete(t.sessions, paneID)
		s.Status = StatusIdle
	default:
		t.mu.Unlock()
		return
	}
	s.UpdatedAt = t.now()
	snapshot := *s
	t.mu.Unlock()
	if t.OnChange != nil {
		t.OnChange(snapshot)
	}
}

// RefreshContexts re-reads the transcript of every session whose file grew and reports changed
// context usage. home locates transcripts for sessions without hook data; window (0 = auto)
// is the configured context size.
func (t *Tracker) RefreshContexts(home string, window int) {
	t.mu.Lock()
	var changed []Session
	for _, s := range t.sessions {
		if s.TranscriptPath == "" {
			s.TranscriptPath = FindTranscript(home, s.Cwd)
			s.transcriptSize = -1
			if s.TranscriptPath == "" {
				continue
			}
		}
		st, err := os.Stat(s.TranscriptPath)
		if err != nil || st.Size() == s.transcriptSize {
			continue
		}
		s.transcriptSize = st.Size()
		c, ok := ReadContext(s.TranscriptPath)
		if !ok {
			continue
		}
		c.Window = WindowFor(c.Model, window, c.Tokens)
		pct := c.Percent()
		if c == s.Context && pct == s.ContextPercent {
			continue
		}
		s.Context, s.ContextPercent = c, pct
		changed = append(changed, *s)
	}
	t.mu.Unlock()
	if t.OnChange != nil {
		for _, s := range changed {
			t.OnChange(s)
		}
	}
}

// Observe reports process-based detection: claude is (not) running in the pane.
// It never downgrades a hook-driven status while the process is still alive.
func (t *Tracker) Observe(paneID, tabID string, running bool, cwd string) {
	t.mu.Lock()
	s, exists := t.sessions[paneID]
	var changed *Session
	switch {
	case running && !exists:
		s = t.get(paneID, tabID)
		s.Status = StatusRunning
		s.Source = "proc"
		s.Cwd = cwd
		s.StartedAt = t.now()
		s.UpdatedAt = s.StartedAt
		changed = s
	case running && exists && s.Cwd == "" && cwd != "":
		s.Cwd = cwd
	case !running && exists:
		delete(t.sessions, paneID)
		gone := *s
		gone.Status = StatusIdle
		gone.UpdatedAt = t.now()
		changed = &gone
	}
	var snapshot Session
	if changed != nil {
		snapshot = *changed
	}
	t.mu.Unlock()
	if changed != nil && t.OnChange != nil {
		t.OnChange(snapshot)
	}
}

// Acknowledge clears waiting/done once the user looked at the pane.
func (t *Tracker) Acknowledge(paneID string) {
	t.mu.Lock()
	s, ok := t.sessions[paneID]
	if !ok || (s.Status != StatusWaiting && s.Status != StatusDone) {
		t.mu.Unlock()
		return
	}
	s.Status = StatusRunning
	s.Message = ""
	s.UpdatedAt = t.now()
	snapshot := *s
	t.mu.Unlock()
	if t.OnChange != nil {
		t.OnChange(snapshot)
	}
}

// Forget drops a pane (closed).
func (t *Tracker) Forget(paneID string) {
	t.mu.Lock()
	delete(t.sessions, paneID)
	t.mu.Unlock()
}

// List returns all sessions, oldest first.
func (t *Tracker) List() []Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Session, 0, len(t.sessions))
	for _, s := range t.sessions {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

func (t *Tracker) get(paneID, tabID string) *Session {
	s, ok := t.sessions[paneID]
	if !ok {
		s = &Session{PaneID: paneID, TabID: tabID, Status: StatusIdle, StartedAt: t.now()}
		t.sessions[paneID] = s
	}
	if tabID != "" {
		s.TabID = tabID
	}
	return s
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
