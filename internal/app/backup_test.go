package app

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

// backupRun is a StateService as one run of wate sees it, on a clock the test moves.
func backupRun(now *time.Time) *StateService {
	return &StateService{clock: func() time.Time { return *now }}
}

func TestABackupFollowsItsWindowAndBeginsAGenerationEveryFewMinutes(t *testing.T) {
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	now := time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)
	s := backupRun(&now)
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
		t.Fatalf("within a few minutes the window's generation follows it: %+v", got)
	}

	// After backupEvery the next change begins a new generation; the old one stays.
	now = now.Add(backupEvery)
	if err := s.SaveBackup(ctx, `{"tabs":[1,2,3]}`); err != nil {
		t.Fatal(err)
	}
	got = s.Backups(ctx)
	if len(got) != 2 || got[0].Data != `{"tabs":[1,2,3]}` || got[1].Data != `{"tabs":[1,2]}` {
		t.Fatalf("generations: %+v", got)
	}

	// An unchanged state begins nothing.
	now = now.Add(backupEvery)
	if err := s.SaveBackup(ctx, `{"tabs":[1,2,3]}`); err != nil {
		t.Fatal(err)
	}
	if n := len(s.Backups(ctx)); n != 2 {
		t.Fatalf("an unchanged save added a generation: %d", n)
	}
}

func TestClosingAWindowSealsItsBackup(t *testing.T) {
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	now := time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)
	s := backupRun(&now)
	ctx := context.Background()
	if err := s.Save(ctx, `{"tabs":[1,2]}`); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveBackup(ctx, `{"tabs":[1,2]}`); err != nil {
		t.Fatal(err)
	}
	s.Remove("w-1")
	if s.Load(ctx) != "" {
		t.Fatal("the closed window's slot is still there")
	}
	// Whatever comes next under the same key, a minute later, is a generation of its own.
	now = now.Add(time.Minute)
	if err := s.SaveBackup(ctx, `{"tabs":[9]}`); err != nil {
		t.Fatal(err)
	}
	got := s.Backups(ctx)
	if len(got) != 2 || got[1].Data != `{"tabs":[1,2]}` {
		t.Fatalf("closing the window lost or reopened its backup: %+v", got)
	}
}

// The 25 Nov 2026 failure: the next start's window got the same key and its one empty tab
// overwrote the only backup of fourteen tabs within seconds.
func TestALaterRunNeverTouchesAnEarlierRunsBackup(t *testing.T) {
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	now := time.Date(2026, 11, 25, 23, 37, 0, 0, time.UTC)
	ctx := context.Background()
	before := backupRun(&now)
	if err := before.SaveBackup(ctx, `{"tabs":["fourteen tabs"]}`); err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Minute) // well within backupEvery
	after := backupRun(&now)
	if last := after.LastBackup(); last == nil || last.Data != `{"tabs":["fourteen tabs"]}` {
		t.Fatalf("the next start is not offered the last layout: %+v", last)
	}
	if err := after.SaveBackup(ctx, `{"tabs":["one tab"]}`); err != nil {
		t.Fatal(err)
	}
	got := after.Backups(ctx)
	if len(got) != 2 || got[1].Data != `{"tabs":["fourteen tabs"]}` {
		t.Fatalf("the earlier run's backup was touched: %+v", got)
	}
	// Its own backups are not what a start is offered.
	if last := after.LastBackup(); last == nil || last.Data != `{"tabs":["fourteen tabs"]}` {
		t.Fatalf("offered %+v", last)
	}
}

func TestAWindowKeepsSoManyGenerations(t *testing.T) {
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	now := time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)
	s := backupRun(&now)
	ctx := context.Background()
	for i := range backupGenerations + 5 {
		if err := s.SaveBackup(ctx, `{"tabs":[`+strconv.Itoa(i)+`]}`); err != nil {
			t.Fatal(err)
		}
		now = now.Add(backupEvery)
	}
	got := s.Backups(ctx)
	if len(got) != backupGenerations || got[0].Data != `{"tabs":[`+strconv.Itoa(backupGenerations+4)+`]}` {
		t.Fatalf("kept %d, newest %s", len(got), got[0].Data)
	}
}

func TestAVersionOneFileBecomesSealedGenerations(t *testing.T) {
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	if err := writeBackups(backupFile{Windows: []backupEntry{{Key: "w-1", SavedAt: "2026-11-16T19:00:00Z", Data: `{"tabs":["a"]}`}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 16, 19, 1, 0, 0, time.UTC)
	s := backupRun(&now)
	got := s.Backups(context.Background())
	if len(got) != 1 || got[0].ID == "" || got[0].Live {
		t.Fatalf("migrated: %+v", got)
	}
	if err := s.SaveBackup(context.Background(), `{"tabs":["b"]}`); err != nil {
		t.Fatal(err)
	}
	if got := s.Backups(context.Background()); len(got) != 2 {
		t.Fatalf("an old backup was written over: %+v", got)
	}
}

func TestRestoringABackupOpensANewWindowWithItsTabs(t *testing.T) {
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	var opened []OpenWindowOptions
	s := &StateService{openWindow: func(o OpenWindowOptions) (WindowID, error) {
		opened = append(opened, o)
		return "w9", nil
	}}
	if err := writeBackups(backupFile{Windows: []backupEntry{
		{ID: "b-old", Key: "win-old", SavedAt: "2026-11-16T19:00:00Z", Data: `{"tabs":["a"]}`, Sealed: true},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreBackup("b-gone"); err == nil {
		t.Fatal("restored a backup that does not exist")
	}
	if err := s.RestoreBackup("b-old"); err != nil {
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
	if got := readBackups().Windows; len(got) != 1 || got[0].ID != "b-old" {
		t.Fatalf("the backup changed: %+v", got)
	}

	// A window that cannot be opened leaves no slot behind to reopen on the next start.
	s.openWindow = func(OpenWindowOptions) (WindowID, error) { return "", errors.New("no display") }
	before := len(readState().Windows)
	if err := s.RestoreBackup("b-old"); err == nil {
		t.Fatal("no error")
	}
	if after := len(readState().Windows); after != before {
		t.Fatalf("slots %d -> %d", before, after)
	}

	if err := s.DeleteBackup("b-old"); err != nil || len(readBackups().Windows) != 0 {
		t.Fatalf("delete: %v %+v", err, readBackups().Windows)
	}
}

func TestOldBackupsGoButOpenWindowsKeepTheirNewest(t *testing.T) {
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

	// Beyond the cap the oldest generations of closed windows go first, never the newest of an
	// open one.
	list = nil
	for i := 0; i < backupKeep+5; i++ {
		list = append(list, backupEntry{Key: "w" + strconv.Itoa(i), SavedAt: at(time.Duration(i) * time.Hour)})
	}
	oldest := "w" + strconv.Itoa(backupKeep+4)
	got = pruneBackups(list, map[string]bool{oldest: true}, now)
	if len(got) != backupKeep || got[len(got)-1].Key != oldest {
		t.Fatalf("kept %d, last %s", len(got), got[len(got)-1].Key)
	}
}

func TestClosingTheLastWindowKeepsItsTabs(t *testing.T) {
	w := NewWindowService(nil)
	var dropped []string
	w.OnClosed = func(key string) { dropped = append(dropped, key) }
	add := func(id WindowID, key string) {
		w.windows[id] = &winEntry{id: id, boot: Bootstrap{WindowID: string(id), StateKey: key, Persist: true}}
		w.order = append(w.order, id)
	}
	add("w1", "w-1")
	add("w2", "win-b")

	// One of two windows closed: it is done with, its slot goes.
	w.forget("w2")
	if len(dropped) != 1 || dropped[0] != "win-b" {
		t.Fatalf("dropped %v", dropped)
	}
	// The last one closing ends wate: that is a quit, and its tabs stay for the next start.
	w.forget("w1")
	if len(dropped) != 1 {
		t.Fatalf("the last window's slot was dropped: %v", dropped)
	}
}
