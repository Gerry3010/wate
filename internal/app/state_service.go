package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/Gerry3010/wate/internal/config"
)

// StateService persists the UI layout (tabs, splits, cwds, open files) between runs.
//
// Go knows that there are windows and nothing else about the shape: each window hands over an
// opaque blob and gets its own back. Teaching the backend to read the layout would be the
// easy way to merge several windows into one file, and the wrong one — see CLAUDE.md, "keep
// the backend UI-agnostic".
type StateService struct {
	windows *WindowService
	mu      sync.Mutex
}

func NewStateService(windows *WindowService) *StateService { return &StateService{windows: windows} }

func (*StateService) ServiceName() string { return "StateService" }

func statePath() string { return filepath.Join(config.StateDir(), "session.json") }

// WindowBlob is one window's saved session, as the frontend wrote it.
type WindowBlob struct {
	Key  string `json:"key"`
	Data string `json:"data"`
}

type stateFile struct {
	Version int          `json:"version"`
	Windows []WindowBlob `json:"windows"`
}

// readState reads session.json, migrating a v1 file (which has no version field at this
// level, because v1 *is* the frontend's blob) into a single window.
func readState() stateFile {
	data, err := os.ReadFile(statePath())
	if err != nil || len(data) == 0 {
		return stateFile{Version: 2}
	}
	var f stateFile
	if json.Unmarshal(data, &f) == nil && f.Version == 2 {
		return f
	}
	// Anything else is the old single-window file: hand it over whole, and let the frontend's
	// parseSession decide whether it is usable. Nothing is thrown away here.
	return stateFile{Version: 2, Windows: []WindowBlob{{Key: "w-1", Data: string(data)}}}
}

func writeState(f stateFile) error {
	p := statePath()
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

// stateKey is the slot a window writes to. Wails puts the calling window on the context.
func stateKey(ctx context.Context, windows *WindowService) string {
	if windows == nil {
		return "w-1"
	}
	return windows.Bootstrap(ctx).StateKey
}

// Load returns this window's saved session ("" when it has none).
func (s *StateService) Load(ctx context.Context) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := stateKey(ctx, s.windows)
	for _, w := range readState().Windows {
		if w.Key == key {
			return w.Data
		}
	}
	return ""
}

// LoadAll returns every window's saved session, in the order they were saved. Startup only.
func (s *StateService) LoadAll() []WindowBlob {
	s.mu.Lock()
	defer s.mu.Unlock()
	return readState().Windows
}

// Save stores this window's session in its own slot, leaving the other windows alone.
func (s *StateService) Save(ctx context.Context, data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := stateKey(ctx, s.windows)
	if key == "" {
		// A window on its way out: the context no longer resolves to one, and a slot keyed ""
		// would be a permanent empty entry.
		return nil
	}
	f := readState()
	for i := range f.Windows {
		if f.Windows[i].Key == key {
			f.Windows[i].Data = data
			return writeState(f)
		}
	}
	f.Windows = append(f.Windows, WindowBlob{Key: key, Data: data})
	return writeState(f)
}

// Remove forgets one window's slot — it was closed, so it should not reopen next time.
func (s *StateService) Remove(key string) {
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := readState()
	out := f.Windows[:0]
	for _, w := range f.Windows {
		if w.Key != key {
			out = append(out, w)
		}
	}
	f.Windows = out
	if err := writeState(f); err != nil {
		slog.Debug("forget window slot", "key", key, "err", err)
	}
	geometry.remove(key)
}

// Prune drops the slots of windows that are no longer saved, so a file cannot grow for ever.
func (s *StateService) Prune(keep []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := readState()
	out := f.Windows[:0]
	for _, w := range f.Windows {
		if contains(keep, w.Key) {
			out = append(out, w)
		}
	}
	f.Windows = out
	return writeState(f)
}
