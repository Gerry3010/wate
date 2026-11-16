package app

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestBackupFollowsTheWindowAndOutlivesItsSlot(t *testing.T) {
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	now := time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)
	s := &StateService{clock: func() time.Time { return now }}
	ctx := context.Background() // no windows: the slot is "w-1"

	if err := s.SaveBackup(ctx, `{"tabs":[1]}`); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if err := s.SaveBackup(ctx, `{"tabs":[1,2]}`); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveBackup(ctx, `not json`); err == nil {
		t.Fatal("took a backup that is not JSON")
	}
	got := s.Backups(ctx)
	if len(got) != 1 || got[0].Data != `{"tabs":[1,2]}` || got[0].Key != "w-1" || !got[0].Current {
		t.Fatalf("one backup per window, the latest: %+v", got)
	}
	if got[0].SavedAt != "2026-11-16T20:01:00Z" {
		t.Fatalf("saved at %s", got[0].SavedAt)
	}

	// Closing the window drops its slot in session.json; the backup stays.
	if err := s.Save(ctx, `{"tabs":[1,2]}`); err != nil {
		t.Fatal(err)
	}
	s.Remove("w-1")
	if s.Load(ctx) != "" {
		t.Fatal("the closed window's slot is still there")
	}
	if got := s.Backups(ctx); len(got) != 1 {
		t.Fatalf("closing the window took the backup with it: %+v", got)
	}
}

func TestRestoringABackupOpensANewWindowWithItsTabs(t *testing.T) {
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	var opened []OpenWindowOptions
	s := &StateService{openWindow: func(o OpenWindowOptions) (WindowID, error) {
		opened = append(opened, o)
		return "w9", nil
	}}
	if err := writeBackups(backupFile{Version: 1, Windows: []backupEntry{
		{Key: "win-old", SavedAt: "2026-11-16T19:00:00Z", Data: `{"tabs":["a"]}`},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreBackup("win-gone"); err == nil {
		t.Fatal("restored a backup that does not exist")
	}
	if err := s.RestoreBackup("win-old"); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 {
		t.Fatalf("opened %d windows", len(opened))
	}
	o := opened[0]
	if o.Restore != "backup" || !o.Persist || !o.Track || o.StateKey == "win-old" || o.StateKey == "" || o.Root {
		t.Fatalf("window options: %+v", o)
	}
	// The new window finds the backup's tabs in a slot of its own.
	var slot string
	for _, w := range readState().Windows {
		if w.Key == o.StateKey {
			slot = w.Data
		}
	}
	if slot != `{"tabs":["a"]}` {
		t.Fatalf("slot %s holds %q", o.StateKey, slot)
	}
	if got := readBackups().Windows; len(got) != 1 || got[0].Key != "win-old" {
		t.Fatalf("the backup changed: %+v", got)
	}

	// A window that cannot be opened leaves no slot behind to reopen on the next start.
	s.openWindow = func(OpenWindowOptions) (WindowID, error) { return "", errors.New("no display") }
	before := len(readState().Windows)
	if err := s.RestoreBackup("win-old"); err == nil {
		t.Fatal("no error")
	}
	if after := len(readState().Windows); after != before {
		t.Fatalf("slots %d -> %d", before, after)
	}

	if err := s.DeleteBackup("win-old"); err != nil || len(readBackups().Windows) != 0 {
		t.Fatalf("delete: %v %+v", err, readBackups().Windows)
	}
}

func TestOldBackupsGoButOpenWindowsKeepTheirs(t *testing.T) {
	now := time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)
	at := func(ago time.Duration) string { return now.Add(-ago).Format(time.RFC3339) }
	list := []backupEntry{
		{Key: "open-old", SavedAt: at(60 * 24 * time.Hour)},
		{Key: "closed-old", SavedAt: at(31 * 24 * time.Hour)},
		{Key: "closed-recent", SavedAt: at(time.Hour)},
		{Key: "broken", SavedAt: "yesterday"},
	}
	got := pruneBackups(list, map[string]bool{"open-old": true}, now)
	if len(got) != 2 || got[0].Key != "closed-recent" || got[1].Key != "open-old" {
		t.Fatalf("pruned to %+v", got)
	}

	// Beyond the cap the oldest closed windows go first, never an open one.
	list = nil
	open := map[string]bool{}
	for i := 0; i < backupKeep+5; i++ {
		k := "w" + strconv.Itoa(i)
		list = append(list, backupEntry{Key: k, SavedAt: at(time.Duration(i) * time.Hour)})
	}
	open["w24"] = true // the oldest of all
	got = pruneBackups(list, open, now)
	if len(got) != backupKeep {
		t.Fatalf("kept %d", len(got))
	}
	if got[len(got)-1].Key != "w24" || got[len(got)-2].Key != "w18" {
		t.Fatalf("kept the wrong ones: last two %s %s", got[len(got)-2].Key, got[len(got)-1].Key)
	}
}
