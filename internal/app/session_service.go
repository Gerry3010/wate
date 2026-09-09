package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Gerry3010/wate/internal/config"
	"github.com/Gerry3010/wate/internal/theme"
)

// SessionService stores named sessions (a set of tabs with layouts and cwds) as JSON files
// in <config dir>/sessions so they travel with the user's dotfiles. The frontend owns the
// tab format; Go only wraps it with a name and timestamp.
type SessionService struct{}

func (SessionService) ServiceName() string { return "SessionService" }

// SessionInfo is a list entry.
type SessionInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	SavedAt string `json:"saved_at"`
	Tabs    int    `json:"tabs"`
	Panes   int    `json:"panes"`
}

// SessionFile is the on-disk shape.
type SessionFile struct {
	Name    string          `json:"name"`
	SavedAt string          `json:"saved_at"`
	Session json.RawMessage `json:"session"`
}

func sessionsDir() string { return filepath.Join(config.Dir(), "sessions") }

// List returns all saved sessions, newest first.
func (SessionService) List() []SessionInfo {
	entries, err := os.ReadDir(sessionsDir())
	if err != nil {
		return nil
	}
	var out []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sessionsDir(), e.Name()))
		if err != nil {
			continue
		}
		var f SessionFile
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		info := SessionInfo{ID: strings.TrimSuffix(e.Name(), ".json"), Name: f.Name, SavedAt: f.SavedAt}
		var s struct {
			Tabs []struct {
				Panes []json.RawMessage `json:"panes"`
			} `json:"tabs"`
		}
		if json.Unmarshal(f.Session, &s) == nil {
			info.Tabs = len(s.Tabs)
			for _, t := range s.Tabs {
				info.Panes += len(t.Panes)
			}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SavedAt > out[j].SavedAt })
	return out
}

// Save stores a session under its name (overwriting a session with the same id) and returns the id.
func (SessionService) Save(name string, session string) (string, error) {
	name = strings.TrimSpace(name)
	id := theme.Slug(name)
	if id == "" {
		return "", fmt.Errorf("session needs a name")
	}
	if !json.Valid([]byte(session)) {
		return "", fmt.Errorf("session is not valid JSON")
	}
	f := SessionFile{Name: name, SavedAt: time.Now().Format(time.RFC3339), Session: json.RawMessage(session)}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(sessionsDir(), 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(sessionsDir(), id+".json")
	if err := os.WriteFile(p+".tmp", data, 0o600); err != nil {
		return "", err
	}
	return id, os.Rename(p+".tmp", p)
}

// Load returns the session JSON for an id.
func (SessionService) Load(id string) (string, error) {
	data, err := os.ReadFile(filepath.Join(sessionsDir(), filepath.Base(id)+".json"))
	if err != nil {
		return "", err
	}
	var f SessionFile
	if err := json.Unmarshal(data, &f); err != nil {
		return "", err
	}
	return string(f.Session), nil
}

// Delete removes a saved session.
func (SessionService) Delete(id string) error {
	return os.Remove(filepath.Join(sessionsDir(), filepath.Base(id)+".json"))
}
