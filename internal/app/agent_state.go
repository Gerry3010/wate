package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Gerry3010/wate/internal/config"
)

// SavedAgentSession is what wate remembers about a Claude Code session between runs: the name
// the user gave it and whether it is a favourite. Claude owns everything else; Cwd and Title
// are copies, kept only so a favourite still has something to show once its session has ended.
type SavedAgentSession struct {
	SessionID string `json:"session_id"`
	Label     string `json:"label"`
	Cwd       string `json:"cwd"`
	Title     string `json:"title"`
	Favorite  bool   `json:"favorite"`
	SeenAt    string `json:"seen_at"` // RFC3339
}

// AgentStateFile is the whole of claude-sessions.json.
type AgentStateFile struct {
	Sessions  []SavedAgentSession `json:"sessions"`
	Collapsed map[string]bool     `json:"collapsed"`
}

// Favourites outlive the sessions they point at, so this cannot grow without bound: a plain
// session that is neither renamed nor favourited is dropped once it is this old.
const agentStateKeep = 30 * 24 * time.Hour

// AgentStateService persists the sidebar's own notion of a session. It deliberately does not
// live in session.json, which is only written when general.restore_session is on.
type AgentStateService struct {
	mu sync.Mutex
}

func (*AgentStateService) ServiceName() string { return "AgentStateService" }

func agentStatePath() string { return filepath.Join(config.StateDir(), "claude-sessions.json") }

func readAgentState() AgentStateFile {
	var f AgentStateFile
	data, err := os.ReadFile(agentStatePath())
	if err == nil {
		_ = json.Unmarshal(data, &f)
	}
	if f.Collapsed == nil {
		f.Collapsed = map[string]bool{}
	}
	return f
}

func writeAgentState(f AgentStateFile) error {
	p := agentStatePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// List returns everything the sidebar remembers, newest first.
func (s *AgentStateService) List() AgentStateFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := readAgentState()
	sort.Slice(f.Sessions, func(i, j int) bool { return f.Sessions[i].SeenAt > f.Sessions[j].SeenAt })
	return f
}

// Set stores (or replaces) one session's entry. An entry that carries neither a label nor a
// favourite flag is nothing worth keeping, so it is removed instead.
func (s *AgentStateService) Set(e SavedAgentSession) error {
	if e.SessionID == "" {
		return fmt.Errorf("session id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := readAgentState()
	e.SeenAt = time.Now().Format(time.RFC3339)
	out := f.Sessions[:0]
	for _, old := range f.Sessions {
		if old.SessionID == e.SessionID {
			// Keep what the caller did not bother to send (the sidebar only knows cwd/title
			// while the session is live).
			if e.Cwd == "" {
				e.Cwd = old.Cwd
			}
			if e.Title == "" {
				e.Title = old.Title
			}
			continue
		}
		if keepAgentEntry(old) {
			out = append(out, old)
		}
	}
	if e.Label == "" && !e.Favorite {
		f.Sessions = out
		return writeAgentState(f)
	}
	f.Sessions = append(out, e)
	return writeAgentState(f)
}

// Remove forgets a session entirely.
func (s *AgentStateService) Remove(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := readAgentState()
	out := f.Sessions[:0]
	for _, e := range f.Sessions {
		if e.SessionID != sessionID {
			out = append(out, e)
		}
	}
	f.Sessions = out
	return writeAgentState(f)
}

// SetCollapsed remembers whether a sidebar section is folded away.
func (s *AgentStateService) SetCollapsed(section string, collapsed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := readAgentState()
	f.Collapsed[section] = collapsed
	return writeAgentState(f)
}

func keepAgentEntry(e SavedAgentSession) bool {
	if e.Favorite || e.Label != "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, e.SeenAt)
	return err == nil && time.Since(t) < agentStateKeep
}

// transcriptRoot is the only directory wate will delete a transcript from.
func transcriptRoot() string { return filepath.Join(homeDir(), ".claude", "projects") }

// underTranscriptRoot guards the one destructive thing the sidebar can do. Deleting a
// transcript reaches outside wate's own files, so the path has to be Claude's, not merely
// something a caller handed us.
func underTranscriptRoot(path string) error {
	if path == "" {
		return fmt.Errorf("no transcript")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	root := transcriptRoot()
	if root == "" || root == string(filepath.Separator) {
		return fmt.Errorf("no home directory")
	}
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return fmt.Errorf("refusing to delete outside %s: %s", root, abs)
	}
	if filepath.Ext(abs) != ".jsonl" {
		return fmt.Errorf("not a transcript: %s", abs)
	}
	return nil
}

// A transcript is named after its session, and a session id is a UUID. Matching that shape is
// what makes the lookup below safe: nothing a caller sends can climb out of the directory.
var sessionIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// TranscriptPath locates a session's transcript. Claude names the file after the session, so
// this is exact — unlike agent.FindTranscript, which picks the newest file in a project and is
// only good enough for reading a title.
func TranscriptPath(sessionID string) (string, error) {
	if !sessionIDRe.MatchString(sessionID) {
		return "", fmt.Errorf("not a session id: %q", sessionID)
	}
	matches, err := filepath.Glob(filepath.Join(transcriptRoot(), "*", sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no transcript for %s", sessionID)
	}
	return matches[0], nil
}

// DeleteTranscript throws away a session's transcript. This reaches outside wate's own files
// and cannot be undone, so the frontend asks first — and the path is checked here regardless.
func (s *AgentStateService) DeleteTranscript(sessionID string) error {
	path, err := TranscriptPath(sessionID)
	if err != nil {
		return err
	}
	if err := underTranscriptRoot(path); err != nil {
		return err
	}
	return os.Remove(path)
}
