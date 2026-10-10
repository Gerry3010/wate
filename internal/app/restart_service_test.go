package app

import (
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Every test gets its own state directory, because the pending restart lives in a file.
func restartIn(t *testing.T) *RestartService {
	t.Helper()
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	return NewRestartService(nil, nil)
}

func TestNothingIsPendingUntilSomebodyAsks(t *testing.T) {
	s := restartIn(t)
	if st := s.Status(); st.Pending {
		t.Error("a restart was pending before anyone asked for one")
	}
	if !s.MayQuit() {
		t.Error("quitting was held back with no restart in sight")
	}
	if s.TellPane("pane-1") != "" {
		t.Error("an agent was told about a restart nobody asked for")
	}
}

func TestAnAnnouncementHoldsTheQuitBack(t *testing.T) {
	s := restartIn(t)
	s.Announce("installing the new build", 5)
	if !s.Status().Pending {
		t.Fatal("the announcement did not stick")
	}
	if s.MayQuit() {
		t.Error("wate would have quit in the middle of arranging a restart")
	}
	// The dialog's own button is the only thing that lets it through.
	s.mu.Lock()
	s.allowed = true
	s.mu.Unlock()
	if !s.MayQuit() {
		t.Error("the button did not release the quit")
	}
}

func TestCancellingPutsEverythingBack(t *testing.T) {
	s := restartIn(t)
	s.Announce("never mind", 5)
	s.Cancel()
	if s.Status().Pending {
		t.Error("a cancelled restart was still pending")
	}
	if !s.MayQuit() {
		t.Error("a cancelled restart still held the quit back")
	}
}

func TestAnAgentIsToldOnceAndThenLeftAlone(t *testing.T) {
	s := restartIn(t)
	s.Announce("the new build", 5)
	first := s.TellPane("pane-1")
	if first == "" {
		t.Fatal("the first hook was told nothing")
	}
	// The text has to carry both ways of answering: an agent without the tools still has a shell.
	if !strings.Contains(first, "wate_restart_respond") || !strings.Contains(first, "wate ctl restart-respond") {
		t.Errorf("the notice does not say how to answer:\n%s", first)
	}
	if !strings.Contains(first, "the new build") {
		t.Error("the notice does not say why")
	}
	// Every tool call fires a hook; being told again on each of them would be nagging.
	for i := 0; i < 5; i++ {
		if again := s.TellPane("pane-1"); again != "" {
			t.Fatalf("told again straight away (attempt %d)", i+2)
		}
	}
	// A different session has not heard it yet.
	if s.TellPane("pane-2") == "" {
		t.Error("a second session was never told")
	}
}

func TestTheRemindersWaitForTheDeadline(t *testing.T) {
	s := restartIn(t)
	s.Announce("soon", 5)
	s.TellPane("pane-1")
	// Pull the deadline in to under a minute: now the second telling is due.
	rec, _ := readRestart()
	rec.Deadline = time.Now().Add(30 * time.Second).Format(time.RFC3339)
	if err := writeRestart(rec); err != nil {
		t.Fatal(err)
	}
	if s.TellPane("pane-1") == "" {
		t.Error("no reminder as the deadline came up")
	}
	if s.TellPane("pane-1") != "" {
		t.Error("told a third time")
	}
}

func TestAnAnswerStopsTheTelling(t *testing.T) {
	s := restartIn(t)
	s.Announce("the new build", 5)
	if err := s.Respond("pane-1", "wait", 7, "mid-migration"); err != nil {
		t.Fatal(err)
	}
	if s.TellPane("pane-1") != "" {
		t.Error("a session that has answered was asked again")
	}
	rec, _ := readRestart()
	a := rec.Answers["pane-1"]
	if a.Verdict != "wait" || a.Minutes != 7 || a.Note != "mid-migration" {
		t.Errorf("answer = %+v", a)
	}
}

func TestOnlyGoOrWait(t *testing.T) {
	s := restartIn(t)
	s.Announce("the new build", 5)
	if err := s.Respond("pane-1", "maybe", 0, ""); err == nil {
		t.Error("a verdict nobody understands was accepted")
	}
	if err := s.Respond("", "go", 0, ""); err == nil {
		t.Error("an answer from nowhere was accepted")
	}
}

func TestAnsweringWithNothingPendingSaysSo(t *testing.T) {
	s := restartIn(t)
	if err := s.Respond("pane-1", "go", 0, ""); err == nil {
		t.Error("an answer was recorded with no question asked")
	}
}

func TestTheDeadlineOnlyChangesWhatIsShown(t *testing.T) {
	s := restartIn(t)
	s.Announce("the new build", 5)
	rec, _ := readRestart()
	rec.Deadline = time.Now().Add(-time.Minute).Format(time.RFC3339)
	if err := writeRestart(rec); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if !st.Expired {
		t.Error("an elapsed deadline was not reported")
	}
	// And it still has not decided anything by itself.
	if !st.Pending {
		t.Error("the deadline cancelled the restart on its own")
	}
	if s.MayQuit() {
		t.Error("the deadline let the quit through without the user")
	}
}

func TestARecordFromALastRunIsDropped(t *testing.T) {
	s := restartIn(t)
	s.Announce("a restart that already happened", 5)
	// A fresh service in the same state directory is the next start of wate.
	next := NewRestartService(nil, nil)
	if err := next.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	if next.Status().Pending {
		t.Error("the new run was greeted by the old run's restart")
	}
	if !next.MayQuit() {
		t.Error("and it held the quit back")
	}
}

func TestASignalOutranksAPendingRestart(t *testing.T) {
	// A terminal that cannot be closed from a script is worse than a restart that finishes
	// early, so Allow — which is what the signal handler calls — always wins.
	s := restartIn(t)
	s.Announce("the new build", 5)
	if s.MayQuit() {
		t.Fatal("the restart was not holding anything back to begin with")
	}
	s.Allow()
	if !s.MayQuit() {
		t.Error("a signal did not get through")
	}
}
