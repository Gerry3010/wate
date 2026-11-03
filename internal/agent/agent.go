// Package agent tracks Claude Code sessions running inside wate panes: which panes
// run one, and whether it is busy, waiting for the user, or done.
package agent

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
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

// Phase is where a session stands as far as the root agent's rules are concerned. It is finer
// than Status where that matters: a permission prompt and Claude Code's "still waiting for your
// input" reminder both turn Status to waiting, but only the first has somebody stuck. It is also
// left alone when the user merely looks at the pane — a prompt glanced at is still unanswered.
type Phase string

const (
	PhaseUnknown    Phase = ""           // only process detection sees the session
	PhaseWorking    Phase = "working"    // a prompt is being worked on
	PhasePermission Phase = "permission" // blocked on the user: a permission prompt, a dialog
	PhaseDone       Phase = "done"       // the turn is over
)

// Session is the state wate keeps per pane.
type Session struct {
	PaneID    string `json:"pane_id"`
	TabID     string `json:"tab_id"`
	Status    Status `json:"status"`
	SessionID string `json:"session_id"`
	// Title names the session like `claude --resume` does (summary or first prompt).
	Title   string `json:"title"`
	Cwd     string `json:"cwd"`
	Message string `json:"message"`
	// Asking is set when Claude's last word was a question for the user — an AskUserQuestion
	// still open, or a turn that ended on "?". It lasts until the next prompt, so a question
	// stays findable after the pane has been looked at.
	Asking    bool      `json:"asking"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Phase and PhaseSince come from hooks only; SeenAt is the last time the user looked at the
	// pane. How long a session has been left alone is UnattendedFor.
	Phase      Phase     `json:"phase"`
	PhaseSince time.Time `json:"phase_since"`
	SeenAt     time.Time `json:"seen_at"`
	// Source is "hook" when hooks deliver precise events, "proc" when only process detection sees it.
	Source string `json:"source"`
	// WindowID is filled in when the session is handed to the frontend, not tracked here: a
	// tab can move to another window while its session runs, so the tracker must not cache it.
	WindowID string `json:"window_id"`
	// TranscriptPath is Claude Code's session transcript (from hooks, or guessed from cwd).
	TranscriptPath string `json:"transcript_path"`
	// Context is the context-window usage read from the transcript (zero when unknown).
	Context Context `json:"context"`
	// ContextPercent is Context.Tokens / Context.Window in percent (0 when unknown).
	ContextPercent int `json:"context_percent"`
	// transcriptSize is the file size at the last context read (skip unchanged files).
	transcriptSize int64
	// contextReadAt throttles re-reading a transcript that grows with every token.
	contextReadAt time.Time
	// titleTried/titleTriedSize remember the last attempt at reading a title: a transcript
	// that has none yet is only worth re-reading once it has grown.
	titleTried     bool
	titleTriedSize int64
	// transcriptTriedAt keeps the search for a missing transcript off every tick.
	transcriptTriedAt time.Time
	// transcriptGuessed is set while TranscriptPath is a guess rather than a hook's word.
	transcriptGuessed bool
}

// setTranscript points s at path and forgets whatever was read from the previous one.
func (s *Session) setTranscript(path string, guessed bool) {
	s.TranscriptPath, s.transcriptGuessed = path, guessed
	s.transcriptSize, s.titleTried = -1, false
	s.Title, s.Context, s.ContextPercent = "", Context{}, 0
}

// Transcript re-reads are paced: an active session appends constantly, and the sidebar's
// context bar does not need to follow every token.
const (
	contextInterval   = 4 * time.Second
	contextGrowth     = 8 * 1024
	transcriptRetry   = 30 * time.Second
	titleRetryMinimum = 4 * 1024
)

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
	SessionID  string `json:"session_id"`
	Cwd        string `json:"cwd"`
	Message    string `json:"message"`
	Title      string `json:"title"`
	HookEvent  string `json:"hook_event_name"`
	Prompt     string `json:"prompt"`
	Transcript string `json:"transcript_path"`
	// Source says why a session started: startup, resume, clear or compact.
	Source string `json:"source"`
	// NotificationType tells a prompt that blocks Claude from a reminder that it is idle.
	NotificationType string `json:"notification_type"`
}

// notificationPhase is the phase a Notification hook leaves the session in; ok is false for a
// notification that says nothing about it (a finished login).
func notificationPhase(p hookPayload) (phase Phase, ok bool) {
	switch p.NotificationType {
	case "permission_prompt", "elicitation_dialog", "agent_needs_input":
		return PhasePermission, true
	case "idle_prompt", "agent_completed":
		return PhaseDone, true
	case "":
		// Older Claude Code sends no type: the idle reminder is the one that says so.
		if strings.Contains(strings.ToLower(p.Message), "waiting for your input") {
			return PhaseDone, true
		}
		return PhasePermission, true
	}
	return "", false
}

// setPhase moves s to phase, restarting the clock only on an actual change.
func (t *Tracker) setPhase(s *Session, phase Phase) {
	if s.Phase != phase {
		s.Phase = phase
		s.PhaseSince = t.now()
	}
}

// UnattendedFor is how long the session has sat in its phase without the user looking at it.
func (s Session) UnattendedFor(now time.Time) time.Duration {
	since := s.PhaseSince
	if s.SeenAt.After(since) {
		since = s.SeenAt
	}
	if since.IsZero() {
		return 0
	}
	return now.Sub(since)
}

// Hook applies a Claude Code hook event to the pane's session.
func (t *Tracker) Hook(paneID, tabID, event string, data []byte) {
	var p hookPayload
	_ = json.Unmarshal(data, &p)
	// Whether Claude ended on a question is in the transcript. Read outside the lock: it is a
	// file read, and the poller should not wait for it.
	asking := false
	if event == "Stop" || event == "Notification" {
		asking = ReadAsking(t.transcriptFor(paneID, p.Transcript))
	}
	t.mu.Lock()
	s := t.get(paneID, tabID)
	s.Source = "hook"
	if p.SessionID != "" {
		s.SessionID = p.SessionID
	}
	if p.Cwd != "" {
		s.Cwd = p.Cwd
	}
	var emit []Session
	if p.Transcript != "" {
		if p.Transcript != s.TranscriptPath {
			// Whatever was read from the old path was a guess (the newest transcript in the
			// directory, which may be another session's) and must not outlive it.
			s.setTranscript(p.Transcript, false)
		}
		s.transcriptGuessed = false
		emit = t.dropGuesses(paneID, p.Transcript)
	}
	changed := true
	switch event {
	case "SessionStart":
		// A compaction starts the session over in Claude Code's terms only; mid-turn, the
		// turn goes on.
		if p.Source == "compact" {
			break
		}
		s.Status = StatusRunning
		s.Message = ""
		s.Asking = false
		s.StartedAt = t.now()
		// A session that has just started sits at its prompt, and so does every session wate
		// resumes after a restart: it is working only once a prompt comes in.
		t.setPhase(s, PhaseDone)
	case "UserPromptSubmit":
		s.Status = StatusRunning
		s.Asking = false
		t.setPhase(s, PhaseWorking)
		// Something Claude Code put in the prompt slot itself, with nothing readable in it,
		// leaves the last real prompt standing.
		if m := promptMessage(p.Prompt); m != "" {
			s.Message = truncate(m, 120)
		}
	case "Notification":
		s.Status = StatusWaiting
		s.Message = firstNonEmpty(p.Message, p.Title, "Claude is waiting for you")
		s.Asking = asking
		if phase, ok := notificationPhase(p); ok {
			t.setPhase(s, phase)
		}
	case "PostToolUse":
		// A tool ran, so whatever blocked it was answered — possibly from somewhere that never
		// focused the pane (Remote Control). Every other tool call is none of the tracker's
		// business: they come by the dozen and change nothing.
		if s.Phase != PhasePermission {
			changed = false
			break
		}
		s.Status = StatusRunning
		s.Message = ""
		t.setPhase(s, PhaseWorking)
	case "Stop":
		s.Status = StatusDone
		s.Message = firstNonEmpty(s.Message, "Finished")
		s.Asking = asking
		t.setPhase(s, PhaseDone)
	case "SessionEnd":
		delete(t.sessions, paneID)
		s.Status = StatusIdle
	default:
		changed = false
	}
	if changed {
		s.UpdatedAt = t.now()
		emit = append(emit, *s)
	}
	t.mu.Unlock()
	if t.OnChange != nil {
		for _, c := range emit {
			t.OnChange(c)
		}
	}
}

// dropGuesses takes path away from every session but paneID's that only guessed it: a hook has
// just said whose it is. They have to wait before guessing again.
func (t *Tracker) dropGuesses(paneID, path string) []Session {
	var out []Session
	for id, s := range t.sessions {
		if id == paneID || !s.transcriptGuessed || s.TranscriptPath != path {
			continue
		}
		s.setTranscript("", false)
		s.transcriptTriedAt = t.now()
		s.UpdatedAt = t.now()
		out = append(out, *s)
	}
	return out
}

// RefreshContexts reads each session's transcript: its title once, and the context usage
// whenever the file grew. home locates transcripts for sessions without hook data; window (0 = auto)
// is the configured context size.
func (t *Tracker) RefreshContexts(home string, window int) {
	t.mu.Lock()
	var changed []Session
	now := t.now()
	for _, s := range t.sessions {
		if s.TranscriptPath == "" {
			// Scanning the project directory for a transcript that is not there yet is not
			// worth doing twice a second.
			if now.Sub(s.transcriptTriedAt) < transcriptRetry {
				continue
			}
			s.transcriptTriedAt = now
			path := t.guessTranscript(home, s)
			if path == "" {
				continue
			}
			s.setTranscript(path, true)
		}
		st, err := os.Stat(s.TranscriptPath)
		if err != nil {
			continue
		}
		dirty := false
		// A transcript whose early entries hold no usable title will not grow one out of thin
		// air: try again only once it has grown, not on every tick.
		if s.Title == "" && (!s.titleTried || st.Size()-s.titleTriedSize >= titleRetryMinimum) {
			s.titleTried, s.titleTriedSize = true, st.Size()
			if title := ReadTitle(s.TranscriptPath); title != "" {
				s.Title = title
				dirty = true
			}
		}
		grown := st.Size() - s.transcriptSize
		// s.transcriptSize < 0 means a fresh (or newly handed over) transcript: read it now.
		if s.transcriptSize < 0 || grown >= contextGrowth || (grown != 0 && now.Sub(s.contextReadAt) >= contextInterval) {
			s.transcriptSize = st.Size()
			s.contextReadAt = now
			if c, ok := ReadContext(s.TranscriptPath); ok {
				c.Window = WindowFor(c.Model, window, c.Tokens)
				if pct := c.Percent(); c != s.Context || pct != s.ContextPercent {
					s.Context, s.ContextPercent = c, pct
					dirty = true
				}
			}
		}
		if dirty {
			changed = append(changed, *s)
		}
	}
	t.mu.Unlock()
	if t.OnChange != nil {
		for _, s := range changed {
			t.OnChange(s)
		}
	}
}

// guessTranscript looks for the transcript of a session no hook has named one for. A wrong
// guess shows another session's title and context in the sidebar and tells the root agent
// about another session's context window, so this only guesses where it can hardly go wrong:
//   - never a transcript another session already has, whether a hook named it or a guess took it;
//   - never while another session in the same directory has none either, since nothing tells
//     the two apart;
//   - only a file written since the session was first seen. Anything older belongs to a
//     session that is not running, or to one that has not said anything yet.
//
// Called with t.mu held.
func (t *Tracker) guessTranscript(home string, s *Session) string {
	taken := map[string]bool{}
	for _, o := range t.sessions {
		switch {
		case o == s:
		case o.TranscriptPath != "":
			taken[o.TranscriptPath] = true
		case o.Cwd == s.Cwd:
			return ""
		}
	}
	return FindTranscript(home, s.Cwd, s.StartedAt, taken)
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
		// No guess before the session's own hook has had time to name its transcript: after a
		// wate restart every pane resumes at once, and a guess in the first seconds took a
		// neighbour's transcript.
		s.transcriptTriedAt = s.StartedAt
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

// transcriptFor is the transcript a hook is about: the one it names, else the one known for
// the pane.
func (t *Tracker) transcriptFor(paneID, named string) string {
	if named != "" {
		return named
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.sessions[paneID]; ok {
		return s.TranscriptPath
	}
	return ""
}

// Acknowledge clears waiting/done once the user looked at the pane. The phase stays: a
// permission prompt the user only glanced at is still unanswered, it has just been seen.
func (t *Tracker) Acknowledge(paneID string) {
	t.mu.Lock()
	s, ok := t.sessions[paneID]
	if ok {
		s.SeenAt = t.now()
	}
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

var (
	reminderRe = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	tagRe      = regexp.MustCompile(`</?[a-zA-Z][\w-]*>`)
	spaceRe    = regexp.MustCompile(`\s+`)
)

// promptMessage is what a submitted prompt says, for the sidebar and the notifications.
//
// Not everything that arrives as a prompt was typed. Claude Code hands a finished background
// task to the session as a <task-notification> block, a slash command as <command-name> and
// friends, and system notes as <system-reminder> — and a notification that read
// "<task-notification> <task-id>ab01…" told nobody anything. Each says what it is about in
// plain words somewhere; that is what is kept.
func promptMessage(prompt string) string {
	s := strings.TrimSpace(reminderRe.ReplaceAllString(prompt, ""))
	switch {
	case strings.HasPrefix(s, "<task-notification>"):
		if sum := tagText(s, "summary"); sum != "" {
			return sum
		}
		if st := tagText(s, "status"); st != "" {
			return "Background task " + st
		}
		return "Background task finished"
	case strings.HasPrefix(s, "<command-name>"):
		return strings.TrimSpace(tagText(s, "command-name") + " " + tagText(s, "command-args"))
	}
	return strings.TrimSpace(spaceRe.ReplaceAllString(tagRe.ReplaceAllString(s, " "), " "))
}

// tagText is the text between <name> and </name>, on one line.
func tagText(s, name string) string {
	open, close := "<"+name+">", "</"+name+">"
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(spaceRe.ReplaceAllString(rest[:j], " "))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
