package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Gerry3010/wate/internal/config"
)

// Window backups, kept apart from session.json.
//
// session.json is what the next start reopens, and a window the user closes leaves it for
// good. A backup is the same window's tabs (layout, directories, the Claude session in each
// pane; no terminal text) and outlives the window, so a window closed by mistake, or a start
// that went wrong, can still be brought back from the tab bar's dropdown.
//
// Backups come in generations. A window writes its latest state into the generation it
// started in this run for as long as that one is younger than backupEvery; then the next one
// begins. Closing the window seals its generation, and nothing a later run does touches a
// generation of an earlier one, even when its window comes back under the same key. That is
// the point of the whole thing: on 25 Nov 2026 a start that came up with one empty tab
// overwrote the only backup of a window with fourteen tabs and thirty-five agents within
// seconds, because there was one backup per window and the newest state always won. Empty
// states are not written at all (the frontend, which can read the tabs, leaves them out).
//
// As with session.json, Go does not read the tabs. The frontend sends the blob and is the
// one that knows what is in it.

const (
	// backupEvery is how long one generation follows the window before the next one begins.
	backupEvery = 5 * time.Minute
	// backupGenerations is how many generations of one window are kept, so a run of bad
	// states cannot push out the last good one.
	backupGenerations = 12
	// backupKeep is how many generations are kept in all, and backupMaxAge how long those of
	// a closed window stay. An open window keeps its newest generation whatever its age.
	backupKeep   = 100
	backupMaxAge = 30 * 24 * time.Hour
)

// WindowBackup is one generation of a window's backup as the dropdown sees it.
type WindowBackup struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	SavedAt string `json:"saved_at"`
	Data    string `json:"data"`
	// Open says the window is open right now; Current, that it is the window asking. Live
	// marks the generation that window is still writing.
	Open    bool `json:"open"`
	Current bool `json:"current"`
	Live    bool `json:"live"`
}

type backupEntry struct {
	ID  string `json:"id"`
	Key string `json:"key"`
	// Run is the wate process that wrote it; Started, when the generation began.
	Run     string `json:"run,omitempty"`
	Started string `json:"started,omitempty"`
	SavedAt string `json:"saved_at"`
	// Sealed is set when its window closed or the next generation began.
	Sealed bool   `json:"sealed,omitempty"`
	Data   string `json:"data"`
}

type backupFile struct {
	Version int           `json:"version"`
	Windows []backupEntry `json:"windows"`
}

func backupPath() string { return filepath.Join(config.StateDir(), "backups.json") }

// readBackups reads backups.json. A version 1 file had one entry per window and no ids; each
// becomes a sealed generation of its own.
func readBackups() backupFile {
	var f backupFile
	data, err := os.ReadFile(backupPath())
	if err != nil || json.Unmarshal(data, &f) != nil {
		return backupFile{Version: 2}
	}
	for i := range f.Windows {
		if f.Windows[i].ID == "" {
			f.Windows[i].ID = "b-" + randomKey()
			f.Windows[i].Sealed = true
		}
	}
	f.Version = 2
	return f
}

func writeBackups(f backupFile) error {
	p := backupPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f.Version = 2
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

// pruneBackups keeps, newest first: at most backupGenerations per window, nothing of a closed
// window older than backupMaxAge, and backupKeep in all, dropping the oldest generations of
// closed windows first. open holds the state keys of the open windows; the newest generation
// of each stays whatever happens.
func pruneBackups(list []backupEntry, open map[string]bool, now time.Time) []backupEntry {
	sort.SliceStable(list, func(i, j int) bool { return list[i].SavedAt > list[j].SavedAt })
	perKey := map[string]int{}
	newest := map[int]bool{}
	var out []backupEntry
	for _, b := range list {
		if !open[b.Key] {
			at, err := time.Parse(time.RFC3339, b.SavedAt)
			if err != nil || now.Sub(at) > backupMaxAge {
				continue
			}
		}
		if perKey[b.Key] >= backupGenerations {
			continue
		}
		if perKey[b.Key] == 0 && open[b.Key] {
			newest[len(out)] = true
		}
		perKey[b.Key]++
		out = append(out, b)
	}
	for i := len(out) - 1; len(out) > backupKeep && i >= 0; i-- {
		if !newest[i] {
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

// runID names this wate process in the backups it writes.
func (s *StateService) runID() string {
	s.runOnce.Do(func() {
		if s.run == "" {
			s.run = randomKey()
		}
	})
	return s.run
}

// SaveBackup stores this window's backup in the generation it is writing, or begins the next
// one. The frontend sends one only when it changed and holds something worth keeping: a
// window on its way out, or one that has just come up empty, must not touch its backups.
func (s *StateService) SaveBackup(ctx context.Context, data string) error {
	key := stateKey(ctx, s.windows)
	if key == "" || s.secondary() {
		return nil
	}
	if !json.Valid([]byte(data)) {
		return errors.New("backup is not valid JSON")
	}
	run := s.runID()
	s.mu.Lock()
	defer s.mu.Unlock()
	f := readBackups()
	now := s.now()
	live, newest := -1, -1
	for i, b := range f.Windows {
		if b.Key != key {
			continue
		}
		if newest < 0 || b.SavedAt > f.Windows[newest].SavedAt {
			newest = i
		}
		if b.Run == run && !b.Sealed && (live < 0 || b.Started > f.Windows[live].Started) {
			live = i
		}
	}
	if newest >= 0 && f.Windows[newest].Data == data {
		// Nothing new: a restored window saving what it was restored from, or no change.
		return nil
	}
	if live >= 0 {
		started, err := time.Parse(time.RFC3339, f.Windows[live].Started)
		if err == nil && now.Sub(started) < backupEvery {
			f.Windows[live].Data = data
			f.Windows[live].SavedAt = now.Format(time.RFC3339)
			f.Windows = pruneBackups(f.Windows, s.openKeys(), now)
			return writeBackups(f)
		}
		f.Windows[live].Sealed = true
	}
	stamp := now.Format(time.RFC3339)
	f.Windows = append(f.Windows, backupEntry{
		ID: "b-" + randomKey(), Key: key, Run: run, Started: stamp, SavedAt: stamp, Data: data,
	})
	f.Windows = pruneBackups(f.Windows, s.openKeys(), now)
	return writeBackups(f)
}

// sealBackups closes the generation a window was writing: it closed, and whatever comes next
// under its key starts afresh.
func (s *StateService) sealBackups(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := readBackups()
	changed := false
	for i := range f.Windows {
		if f.Windows[i].Key == key && !f.Windows[i].Sealed {
			f.Windows[i].Sealed = true
			changed = true
		}
	}
	if !changed {
		return
	}
	if err := writeBackups(f); err != nil {
		slog.Debug("seal backups", "key", key, "err", err)
	}
}

// LastBackup is the newest backup an earlier run of wate left, for a start that came up
// without its tabs to offer; nil when there is none.
func (s *StateService) LastBackup() *WindowBackup {
	if s.secondary() {
		return nil
	}
	run := s.runID()
	s.mu.Lock()
	f := readBackups()
	s.mu.Unlock()
	var best *backupEntry
	for i, b := range f.Windows {
		if b.Run == run {
			continue
		}
		if best == nil || b.SavedAt > best.SavedAt {
			best = &f.Windows[i]
		}
	}
	if best == nil {
		return nil
	}
	return &WindowBackup{ID: best.ID, Key: best.Key, SavedAt: best.SavedAt, Data: best.Data}
}

// Backups lists the generations, newest first.
func (s *StateService) Backups(ctx context.Context) []WindowBackup {
	if s.secondary() {
		return nil
	}
	key := stateKey(ctx, s.windows)
	open := s.openKeys()
	run := s.runID()
	s.mu.Lock()
	f := readBackups()
	s.mu.Unlock()
	sort.SliceStable(f.Windows, func(i, j int) bool { return f.Windows[i].SavedAt > f.Windows[j].SavedAt })
	out := make([]WindowBackup, 0, len(f.Windows))
	for _, b := range f.Windows {
		out = append(out, WindowBackup{
			ID: b.ID, Key: b.Key, SavedAt: b.SavedAt, Data: b.Data,
			Open: open[b.Key], Current: b.Key == key, Live: b.Run == run && !b.Sealed && open[b.Key],
		})
	}
	return out
}

// RestoreBackup opens a window with the tabs of a backup generation (by id). It is a new
// window with a slot of its own: the backup stays as it is, and the window it came from (open
// or not) is not touched.
func (s *StateService) RestoreBackup(id string) error {
	if s.secondary() {
		return errors.New("this wate is a second instance; restore backups in the first one")
	}
	s.mu.Lock()
	var data string
	for _, b := range readBackups().Windows {
		if b.ID == id {
			data = b.Data
		}
	}
	if data == "" {
		s.mu.Unlock()
		return fmt.Errorf("no backup %s", id)
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

// DeleteBackup forgets one backup generation (by id).
func (s *StateService) DeleteBackup(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := readBackups()
	out := f.Windows[:0]
	for _, b := range f.Windows {
		if b.ID != id {
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
