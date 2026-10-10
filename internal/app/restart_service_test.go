package app

import (
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/wate/internal/config"
)

// Every test gets its own state directory, because the pending restart lives in a file.
func restartIn(t *testing.T) *RestartService {
	t.Helper()
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	return NewRestartService(nil, nil, func() config.Config {
		// No countdown by default in these tests: the ones about it set their own.
		return config.Config{Claude: config.Claude{RestartCountdown: 0}}
	})
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
	s.Announce("installing the new build", 5, true)
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
	s.Announce("never mind", 5, true)
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
	s.Announce("the new build", 5, true)
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
	s.Announce("soon", 5, true)
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
	s.Announce("the new build", 5, true)
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
	s.Announce("the new build", 5, true)
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
	s.Announce("the new build", 5, true)
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
	s.Announce("a restart that already happened", 5, true)
	// A fresh service in the same state directory is the next start of wate.
	next := NewRestartService(nil, nil, nil)
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
	s.Announce("the new build", 5, true)
	if s.MayQuit() {
		t.Fatal("the restart was not holding anything back to begin with")
	}
	s.Allow()
	if !s.MayQuit() {
		t.Error("a signal did not get through")
	}
}

func TestAWindowWithNoAgentsClosesWithoutBeingAsked(t *testing.T) {
	s := restartIn(t)
	if !s.ConfirmClose("w1") {
		t.Error("an empty window had to ask permission to close")
	}
}

func TestTheQuitButtonLetsExactlyOneCloseThrough(t *testing.T) {
	s := restartIn(t)
	s.allowCloseFor("w1")
	if !s.ConfirmClose("w1") {
		t.Fatal("the button did not let the close through")
	}
	// Spent. Another window, or another close later, is asked about again — otherwise one
	// "quit anyway" would silence the question for the rest of the run.
	s.mu.Lock()
	left := len(s.allowClose)
	s.mu.Unlock()
	if left != 0 {
		t.Errorf("%d windows left permanently allowed", left)
	}
	if !s.ConfirmClose("w2") {
		t.Error("an unrelated empty window was refused")
	}
}

func TestAnApprovedQuitDoesNotAskPerWindow(t *testing.T) {
	// The restart dialog's own button, or a signal, has already settled it.
	s := restartIn(t)
	s.Announce("the new build", 5, true)
	s.Allow()
	if !s.ConfirmClose("w1") {
		t.Error("a quit the user already approved was questioned again")
	}
}

// countingDown builds a service whose countdown is this many seconds.
func countingDown(t *testing.T, secs int) *RestartService {
	t.Helper()
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	return NewRestartService(nil, nil, func() config.Config {
		return config.Config{Claude: config.Claude{RestartCountdown: secs}}
	})
}

func TestWithNobodyLeftToWaitForItGoesByItself(t *testing.T) {
	// The user asked for the restart when they started the round; once every agent has said
	// go there is nothing left to decide, so it should not need a second press.
	s := countingDown(t, 10)
	st := s.Announce("the new build", 5, true)
	if st.GoAt == "" {
		t.Fatal("no countdown with nobody to wait for")
	}
	at, err := time.Parse(time.RFC3339, st.GoAt)
	if err != nil {
		t.Fatalf("go_at: %v", err)
	}
	if d := time.Until(at); d < 5*time.Second || d > 11*time.Second {
		t.Errorf("countdown is %v, want about ten seconds", d)
	}
}

func TestZeroSecondsMeansWaitForTheButton(t *testing.T) {
	s := countingDown(t, 0)
	if st := s.Announce("the new build", 5, true); st.GoAt != "" {
		t.Error("a countdown ran although it was switched off")
	}
}

func TestCancellingStopsTheCountdown(t *testing.T) {
	s := countingDown(t, 10)
	s.Announce("the new build", 5, true)
	s.Cancel()
	s.mu.Lock()
	running := s.countdown != nil
	s.mu.Unlock()
	if running {
		t.Error("the countdown kept running after the restart was called off")
	}
}

func TestTheCountdownActuallyFires(t *testing.T) {
	s := countingDown(t, 1)
	s.Announce("the new build", 5, true)
	// Proceed needs an application to quit; without one it only clears the record, which is
	// exactly the observable part here.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, pending := readRestart(); !pending {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the countdown never fired")
}
