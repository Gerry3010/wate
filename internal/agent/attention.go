package agent

import (
	"strconv"
	"time"
)

// Rules are the thresholds that wake the root agent.
type Rules struct {
	// Waiting is how long a permission prompt or a question may sit unattended.
	Waiting time.Duration
	// ContextPercent is the context usage worth hearing about (0 = never).
	ContextPercent int
}

// Kinds of attention events.
const (
	EventPermission = "permission" // blocked on a prompt nobody answered
	EventQuestion   = "question"   // the turn ended on a question nobody answered
	EventContext    = "context"    // the context window is nearly full
)

// Event is something about a session the root agent should hear of.
type Event struct {
	Kind           string    `json:"kind"`
	PaneID         string    `json:"pane_id"`
	TabID          string    `json:"tab_id"`
	SessionID      string    `json:"session_id"`
	Title          string    `json:"title"`
	Cwd            string    `json:"cwd"`
	Message        string    `json:"message"`
	Since          time.Time `json:"since"`
	ContextPercent int       `json:"context_percent"`
	TranscriptPath string    `json:"transcript_path"`
}

// Watch turns the session table into events, each one once: a session stuck for an hour is
// one event, not one per tick. A prompt counts again only once it is a new prompt (a new phase);
// the context alarm re-arms once usage has dropped below the threshold (a compaction).
type Watch struct {
	raised map[string]bool
	full   map[string]bool
}

func NewWatch() *Watch {
	return &Watch{raised: map[string]bool{}, full: map[string]bool{}}
}

// Check returns the events sessions call for at now. skip leaves panes out: the root agent's
// own, and the one the user is looking at.
func (w *Watch) Check(sessions []Session, now time.Time, r Rules, skip func(paneID string) bool) []Event {
	var out []Event
	raised := map[string]bool{}
	full := map[string]bool{}
	for _, s := range sessions {
		if skip != nil && skip(s.PaneID) {
			// Out of sight is not forgotten: what was raised stays raised.
			for _, kind := range []string{EventPermission, EventQuestion} {
				if k := episode(s, kind); w.raised[k] {
					raised[k] = true
				}
			}
			if w.full[contextKey(s)] {
				full[contextKey(s)] = true
			}
			continue
		}
		if kind := stuck(s); kind != "" {
			k := episode(s, kind)
			if w.raised[k] {
				raised[k] = true
			} else if r.Waiting > 0 && s.UnattendedFor(now) >= r.Waiting {
				raised[k] = true
				out = append(out, eventFor(kind, s, s.PhaseSince))
			}
		}
		if r.ContextPercent > 0 && s.ContextPercent >= r.ContextPercent {
			k := contextKey(s)
			full[k] = true
			if !w.full[k] {
				out = append(out, eventFor(EventContext, s, now))
			}
		}
	}
	w.raised, w.full = raised, full
	return out
}

// stuck is the kind of wait s is in, if any.
func stuck(s Session) string {
	switch {
	case s.Phase == PhasePermission:
		return EventPermission
	case s.Phase == PhaseDone && s.Asking:
		return EventQuestion
	}
	return ""
}

func episode(s Session, kind string) string {
	return s.PaneID + "|" + kind + "|" + strconv.FormatInt(s.PhaseSince.UnixNano(), 10)
}

func contextKey(s Session) string {
	if s.SessionID != "" {
		return s.SessionID
	}
	return "pane:" + s.PaneID
}

func eventFor(kind string, s Session, since time.Time) Event {
	return Event{
		Kind: kind, PaneID: s.PaneID, TabID: s.TabID, SessionID: s.SessionID, Title: s.Title,
		Cwd: s.Cwd, Message: s.Message, Since: since, ContextPercent: s.ContextPercent,
		TranscriptPath: s.TranscriptPath,
	}
}
