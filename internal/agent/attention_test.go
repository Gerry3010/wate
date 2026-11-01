package agent

import (
	"testing"
	"time"
)

func TestWatchRaisesEachWaitOnce(t *testing.T) {
	start := time.Unix(1000, 0)
	rules := Rules{Waiting: 10 * time.Minute, ContextPercent: 85}
	blocked := Session{PaneID: "p1", Phase: PhasePermission, PhaseSince: start}
	question := Session{PaneID: "p2", Phase: PhaseDone, Asking: true, PhaseSince: start}
	finished := Session{PaneID: "p3", Phase: PhaseDone, PhaseSince: start}
	working := Session{PaneID: "p4", Phase: PhaseWorking, PhaseSince: start}
	all := []Session{blocked, question, finished, working}

	w := NewWatch()
	if got := w.Check(all, start.Add(9*time.Minute), rules, nil); len(got) != 0 {
		t.Fatalf("before the threshold: %+v", got)
	}
	got := w.Check(all, start.Add(10*time.Minute), rules, nil)
	if len(got) != 2 || got[0].Kind != EventPermission || got[0].PaneID != "p1" ||
		got[1].Kind != EventQuestion || got[1].PaneID != "p2" || !got[0].Since.Equal(start) {
		t.Fatalf("at the threshold: %+v", got)
	}
	if got := w.Check(all, start.Add(time.Hour), rules, nil); len(got) != 0 {
		t.Fatalf("raised again: %+v", got)
	}
	// A new prompt is a new wait.
	again := blocked
	again.PhaseSince = start.Add(time.Hour)
	if got := w.Check([]Session{again}, start.Add(time.Hour+10*time.Minute), rules, nil); len(got) != 1 {
		t.Fatalf("a new prompt went unnoticed: %+v", got)
	}
}

func TestWatchCountsFromTheLastLook(t *testing.T) {
	start := time.Unix(1000, 0)
	s := Session{PaneID: "p1", Phase: PhasePermission, PhaseSince: start, SeenAt: start.Add(8 * time.Minute)}
	w := NewWatch()
	if got := w.Check([]Session{s}, start.Add(12*time.Minute), Rules{Waiting: 10 * time.Minute}, nil); len(got) != 0 {
		t.Fatalf("glanced at four minutes ago, raised anyway: %+v", got)
	}
	if got := w.Check([]Session{s}, start.Add(18*time.Minute), Rules{Waiting: 10 * time.Minute}, nil); len(got) != 1 {
		t.Fatalf("ten minutes since the glance, nothing: %+v", got)
	}
}

func TestWatchLeavesSkippedPanesAlone(t *testing.T) {
	start := time.Unix(1000, 0)
	s := Session{PaneID: "root", Phase: PhasePermission, PhaseSince: start, ContextPercent: 95}
	w := NewWatch()
	skip := func(p string) bool { return p == "root" }
	if got := w.Check([]Session{s}, start.Add(time.Hour), Rules{Waiting: time.Minute, ContextPercent: 85}, skip); len(got) != 0 {
		t.Fatalf("skipped pane raised: %+v", got)
	}
	// Raised while in view of nobody, then looked at: the look does not make it raise twice.
	w = NewWatch()
	other := Session{PaneID: "p1", Phase: PhasePermission, PhaseSince: start}
	w.Check([]Session{other}, start.Add(time.Hour), Rules{Waiting: time.Minute}, nil)
	w.Check([]Session{other}, start.Add(time.Hour), Rules{Waiting: time.Minute}, func(string) bool { return true })
	if got := w.Check([]Session{other}, start.Add(2*time.Hour), Rules{Waiting: time.Minute}, nil); len(got) != 0 {
		t.Fatalf("raised again after being in view: %+v", got)
	}
}

func TestWatchContextAlarmReArmsAfterACompaction(t *testing.T) {
	now := time.Unix(1000, 0)
	rules := Rules{ContextPercent: 85}
	s := Session{PaneID: "p1", SessionID: "abc", ContextPercent: 90}
	w := NewWatch()
	if got := w.Check([]Session{s}, now, rules, nil); len(got) != 1 || got[0].Kind != EventContext {
		t.Fatalf("full context: %+v", got)
	}
	if got := w.Check([]Session{s}, now, rules, nil); len(got) != 0 {
		t.Fatalf("raised twice: %+v", got)
	}
	s.ContextPercent = 30
	w.Check([]Session{s}, now, rules, nil)
	s.ContextPercent = 88
	if got := w.Check([]Session{s}, now, rules, nil); len(got) != 1 {
		t.Fatalf("after a compaction the alarm stayed off: %+v", got)
	}
}

func TestWatchWithoutRulesIsQuiet(t *testing.T) {
	start := time.Unix(1000, 0)
	s := Session{PaneID: "p1", Phase: PhasePermission, PhaseSince: start, ContextPercent: 99}
	if got := NewWatch().Check([]Session{s}, start.Add(24*time.Hour), Rules{}, nil); len(got) != 0 {
		t.Fatalf("zero rules raised: %+v", got)
	}
}
