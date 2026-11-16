package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Gerry3010/wate/internal/config"
)

// Window backups: one per window, kept apart from session.json.
//
// session.json is what the next start reopens, and a window the user closes leaves it for
// good. A backup is the same window's tabs (layout, directories, the Claude session in each
// pane; no terminal text) and stays when the window closes, so a window closed by mistake,
// or a start that went wrong, can still be brought back from the tab bar's dropdown. It
// follows the window as it changes: one backup per window, the latest state of it.
//
// As with session.json, Go does not read the tabs. The frontend sends the blob and is the
// one that knows what is in it.

// backupKeep is how many backups are kept at most, and backupMaxAge how long the backup of a
// closed window stays. A window that is open keeps its backup whatever its age.
const (
	backupKeep   = 20
	backupMaxAge = 30 * 24 * time.Hour
)

// WindowBackup is one window's backup as the dropdown sees it.
type WindowBackup struct {
	Key     string `json:"key"`
	SavedAt string `json:"saved_at"`
	Data    string `json:"data"`
	// Open says the window is open right now; Current, that it is the window asking.
	Open    bool `json:"open"`
	Current bool `json:"current"`
}

type backupEntry struct {
	Key     string `json:"key"`
	SavedAt string `json:"saved_at"`
	Data    string `json:"data"`
}

type backupFile struct {
	Version int           `json:"version"`
	Windows []backupEntry `json:"windows"`
}

func backupPath() string { return filepath.Join(config.StateDir(), "backups.json") }

func readBackups() backupFile {
	var f backupFile
	data, err := os.ReadFile(backupPath())
	if err != nil || json.Unmarshal(data, &f) != nil {
		return backupFile{Version: 1}
	}
	f.Version = 1
	return f
}

func writeBackups(f backupFile) error {
	p := backupPath()
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

// pruneBackups drops the backups of closed windows that are older than backupMaxAge, then the
// oldest closed ones beyond backupKeep. open holds the state keys of the open windows.
func pruneBackups(list []backupEntry, open map[string]bool, now time.Time) []backupEntry {
	sort.SliceStable(list, func(i, j int) bool { return list[i].SavedAt > list[j].SavedAt })
	out := list[:0]
	for _, b := range list {
		if !open[b.Key] {
			at, err := time.Parse(time.RFC3339, b.SavedAt)
			if err != nil || now.Sub(at) > backupMaxAge {
				continue
			}
		}
		out = append(out, b)
	}
	for i := len(out) - 1; len(out) > backupKeep && i >= 0; i-- {
		if !open[out[i].Key] {
			out = append(out[:i], out[i+1:]...)
		}
	}
	return out
}

func (s *StateService) openKeys() map[string]bool {
	if s.windows == nil {
		return map[string]bool{}
	}
	return s.windows.openKeys()
}

func (s *StateService) secondary() bool {
	return s.windows != nil && s.windows.cfg != nil && s.windows.cfg.Secondary
}

// SaveBackup stores this window's backup. The frontend sends one only when it changed and the
// window has tabs: a window on its way out has none, and must not empty its backup.
func (s *StateService) SaveBackup(ctx context.Context, data string) error {
	key := stateKey(ctx, s.windows)
	if key == "" || s.secondary() {
		return nil
	}
	if !json.Valid([]byte(data)) {
		return errors.New("backup is not valid JSON")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := readBackups()
	entry := backupEntry{Key: key, SavedAt: s.now().Format(time.RFC3339), Data: data}
	found := false
	for i := range f.Windows {
		if f.Windows[i].Key == key {
			f.Windows[i] = entry
			found = true
			break
		}
	}
	if !found {
		f.Windows = append(f.Windows, entry)
	}
	f.Windows = pruneBackups(f.Windows, s.openKeys(), s.now())
	return writeBackups(f)
}

// Backups lists the backups, newest first.
func (s *StateService) Backups(ctx context.Context) []WindowBackup {
	if s.secondary() {
		return nil
	}
	key := stateKey(ctx, s.windows)
	open := s.openKeys()
	s.mu.Lock()
	f := readBackups()
	s.mu.Unlock()
	sort.SliceStable(f.Windows, func(i, j int) bool { return f.Windows[i].SavedAt > f.Windows[j].SavedAt })
	out := make([]WindowBackup, 0, len(f.Windows))
	for _, b := range f.Windows {
		out = append(out, WindowBackup{Key: b.Key, SavedAt: b.SavedAt, Data: b.Data, Open: open[b.Key], Current: b.Key == key})
	}
	return out
}

// RestoreBackup opens a window with the tabs of a backup. It is a new window with a slot of its
// own: the backup stays as it is, and the window it came from (open or not) is not touched.
func (s *StateService) RestoreBackup(key string) error {
	if s.secondary() {
		return errors.New("this wate is a second instance; restore backups in the first one")
	}
	s.mu.Lock()
	var data string
	for _, b := range readBackups().Windows {
		if b.Key == key {
			data = b.Data
		}
	}
	if data == "" {
		s.mu.Unlock()
		return fmt.Errorf("no backup for window %s", key)
	}
	slot := "win-" + randomKey()
	f := readState()
	f.Windows = append(f.Windows, WindowBlob{Key: slot, Data: data})
	err := writeState(f)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if _, err := s.open(OpenWindowOptions{Restore: "backup", StateKey: slot, Track: true, Persist: true}); err != nil {
		s.Remove(slot)
		return err
	}
	return nil
}

// DeleteBackup forgets a backup.
func (s *StateService) DeleteBackup(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := readBackups()
	out := f.Windows[:0]
	for _, b := range f.Windows {
		if b.Key != key {
			out = append(out, b)
		}
	}
	f.Windows = out
	return writeBackups(f)
}

func (s *StateService) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

func (s *StateService) open(o OpenWindowOptions) (WindowID, error) {
	if s.openWindow != nil {
		return s.openWindow(o)
	}
	if s.windows == nil {
		return "", errors.New("no windows")
	}
	return s.windows.Open(o)
}
