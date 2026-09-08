package agent

import (
	"testing"
	"time"
)

func TestHookLifecycle(t *testing.T) {
	tr := NewTracker()
	var events []Session
	tr.OnChange = func(s Session) { events = append(events, s) }

	tr.Hook("p1", "t1", "SessionStart", []byte(`{"session_id":"abc","cwd":"/work"}`))
	tr.Hook("p1", "t1", "UserPromptSubmit", []byte(`{"prompt":"fix the bug"}`))
	tr.Hook("p1", "t1", "Notification", []byte(`{"message":"Claude needs your permission to use Bash"}`))
	if s := tr.List()[0]; s.Status != StatusWaiting || s.SessionID != "abc" || s.Cwd != "/work" || s.Message == "" {
		t.Fatalf("after notification: %+v", s)
	}
	tr.Acknowledge("p1")
	if s := tr.List()[0]; s.Status != StatusRunning || s.Message != "" {
		t.Fatalf("after ack: %+v", s)
	}
	tr.Hook("p1", "t1", "Stop", nil)
	if s := tr.List()[0]; s.Status != StatusDone {
		t.Fatalf("after stop: %+v", s)
	}
	tr.Hook("p1", "t1", "SessionEnd", nil)
	if len(tr.List()) != 0 {
		t.Fatal("session should be gone")
	}
	if len(events) != 6 || events[len(events)-1].Status != StatusIdle {
		t.Fatalf("events: %d, last %+v", len(events), events[len(events)-1])
	}
}

func TestObserveDoesNotDowngradeHookStatus(t *testing.T) {
	tr := NewTracker()
	tr.Observe("p1", "t1", true, "/x")
	if s := tr.List()[0]; s.Status != StatusRunning || s.Source != "proc" {
		t.Fatalf("proc detect: %+v", s)
	}
	tr.Hook("p1", "t1", "Notification", []byte(`{"message":"?"}`))
	tr.Observe("p1", "t1", true, "/x")
	if s := tr.List()[0]; s.Status != StatusWaiting || s.Source != "hook" {
		t.Fatalf("observe must keep waiting: %+v", s)
	}
	tr.Observe("p1", "t1", false, "")
	if len(tr.List()) != 0 {
		t.Fatal("process gone → session gone")
	}
	tr.Observe("p2", "t1", false, "")
	if len(tr.List()) != 0 {
		t.Fatal("unknown pane not running must not create a session")
	}
}

func TestListOrderAndTruncate(t *testing.T) {
	tr := NewTracker()
	base := time.Unix(1000, 0)
	tr.now = func() time.Time { base = base.Add(time.Second); return base }
	tr.Observe("b", "t", true, "")
	tr.Observe("a", "t", true, "")
	l := tr.List()
	if l[0].PaneID != "b" || l[1].PaneID != "a" {
		t.Fatalf("order: %v", l)
	}
	if got := truncate("héllo wörld", 6); got != "héllo…" {
		t.Fatalf("truncate: %q", got)
	}
}
