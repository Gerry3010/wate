package app

import (
	"os"
	"path/filepath"

	"github.com/Gerry3010/wate/internal/config"
)

// StateService persists the UI layout (tabs, splits, cwds, open files) between runs.
// The frontend owns the shape; Go only stores the JSON blob.
type StateService struct{}

func (StateService) ServiceName() string { return "StateService" }

func statePath() string { return filepath.Join(config.StateDir(), "session.json") }

// Load returns the saved session JSON ("" when none).
func (StateService) Load() string {
	b, err := os.ReadFile(statePath())
	if err != nil {
		return ""
	}
	return string(b)
}

// Save stores the session JSON atomically.
func (StateService) Save(data string) error {
	p := statePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
