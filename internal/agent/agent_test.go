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

func TestPromptsSayWhatTheyAreAbout(t *testing.T) {
	for _, c := range []struct{ name, prompt, want string }{
		{"typed", "fix the bug", "fix the bug"},
		{"background task", "<task-notification>\n<task-id>ab016483a3ee82815</task-id>\n" +
			"<tool-use-id>toolu_01MLj7</tool-use-id>\n<output-file>/tmp/x.output</output-file>\n" +
			"<status>completed</status>\n<summary>Background command \"npm test\" completed (exit code 0)</summary>\n" +
			"</task-notification>", `Background command "npm test" completed (exit code 0)`},
		{"background task without a summary", "<task-notification><task-id>x</task-id><status>failed</status></task-notification>", "Background task failed"},
		{"slash command", "<command-name>/compact</command-name>\n<command-message>compact</command-message>\n<command-args>keep the tests</command-args>", "/compact keep the tests"},
		{"system note before the prompt", "<system-reminder>\nToday is Friday.\n</system-reminder>\nship it", "ship it"},
		{"only a system note", "<system-reminder>nothing for you</system-reminder>", ""},
		{"other wrappers", "<bash-input>ls -la</bash-input>", "ls -la"},
	} {
		if got := promptMessage(c.prompt); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestANoteInThePromptSlotKeepsTheLastPrompt(t *testing.T) {
	tr := NewTracker()
	tr.Hook("p1", "t1", "UserPromptSubmit", []byte(`{"prompt":"fix the bug"}`))
	tr.Hook("p1", "t1", "UserPromptSubmit", []byte(`{"prompt":"<system-reminder>x</system-reminder>"}`))
	if s := tr.List()[0]; s.Message != "fix the bug" {
		t.Errorf("message became %q", s.Message)
	}
}

func TestTheTranscriptAHookNamesReplacesAGuess(t *testing.T) {
	tr := NewTracker()
	tr.Hook("p1", "t1", "SessionStart", []byte(`{"transcript_path":"/guessed.jsonl"}`))
	tr.mu.Lock()
	s := tr.sessions["p1"]
	s.Title, s.ContextPercent = "somebody else's session", 70
	tr.mu.Unlock()
	tr.Hook("p1", "t1", "UserPromptSubmit", []byte(`{"transcript_path":"/real.jsonl","prompt":"x"}`))
	if s := tr.List()[0]; s.Title != "" || s.ContextPercent != 0 || s.TranscriptPath != "/real.jsonl" {
		t.Errorf("what was read from the guess survived: %+v", s)
	}
}

// clock is a tracker clock the test moves by hand.
func clock(tr *Tracker) func(time.Duration) time.Time {
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }
	return func(d time.Duration) time.Time { now = now.Add(d); return now }
}

func TestAnIdleReminderIsNotAPermissionPrompt(t *testing.T) {
	for _, c := range []struct {
		name, payload string
		want          Phase
	}{
		{"permission", `{"notification_type":"permission_prompt","message":"Claude needs your permission to use Bash"}`, PhasePermission},
		{"dialog", `{"notification_type":"elicitation_dialog"}`, PhasePermission},
		{"idle", `{"notification_type":"idle_prompt","message":"Claude is waiting for your input"}`, PhaseDone},
		{"idle without a type", `{"message":"Claude is waiting for your input"}`, PhaseDone},
		{"permission without a type", `{"message":"Claude needs your permission to use Bash"}`, PhasePermission},
		{"login", `{"notification_type":"auth_success"}`, PhaseDone},
	} {
		tr := NewTracker()
		tr.Hook("p1", "t1", "UserPromptSubmit", []byte(`{"prompt":"x"}`))
		tr.Hook("p1", "t1", "Stop", nil)
		tr.Hook("p1", "t1", "Notification", []byte(c.payload))
		if s := tr.List()[0]; s.Phase != c.want || s.Status != StatusWaiting {
			t.Errorf("%s: phase %q status %q, want phase %q", c.name, s.Phase, s.Status, c.want)
		}
	}
}

func TestLookingAtAPromptResetsTheClockButNotThePhase(t *testing.T) {
	tr := NewTracker()
	tick := clock(tr)
	tr.Hook("p1", "t1", "UserPromptSubmit", []byte(`{"prompt":"x"}`))
	tick(time.Minute)
	tr.Hook("p1", "t1", "Notification", []byte(`{"notification_type":"permission_prompt"}`))
	now := tick(5 * time.Minute)
	if got := tr.List()[0].UnattendedFor(now); got != 5*time.Minute {
		t.Fatalf("unattended %v, want 5m", got)
	}
	tr.Acknowledge("p1")
	now = tick(2 * time.Minute)
	s := tr.List()[0]
	if s.Phase != PhasePermission || s.UnattendedFor(now) != 2*time.Minute {
		t.Fatalf("after a glance: phase %q, unattended %v", s.Phase, s.UnattendedFor(now))
	}
	// A second identical notification does not restart the clock.
	tr.Hook("p1", "t1", "Notification", []byte(`{"notification_type":"permission_prompt"}`))
	if got := tr.List()[0].PhaseSince; !got.Equal(time.Unix(1060, 0)) {
		t.Fatalf("phase clock restarted: %v", got)
	}
}

func TestAToolRunningEndsThePermissionPhase(t *testing.T) {
	tr := NewTracker()
	changes := 0
	tr.OnChange = func(Session) { changes++ }
	tr.Hook("p1", "t1", "UserPromptSubmit", []byte(`{"prompt":"x"}`))
	tr.Hook("p1", "t1", "PostToolUse", nil)
	if changes != 1 {
		t.Fatalf("an ordinary tool call reported a change (%d)", changes)
	}
	tr.Hook("p1", "t1", "Notification", []byte(`{"notification_type":"permission_prompt"}`))
	tr.Hook("p1", "t1", "PostToolUse", nil)
	if s := tr.List()[0]; s.Phase != PhaseWorking || s.Status != StatusRunning {
		t.Fatalf("answered elsewhere, still blocked: %+v", s)
	}
}

func TestASessionIsWorkingOnlyOnceAPromptComesIn(t *testing.T) {
	tr := NewTracker()
	tick := clock(tr)
	phase := func() Phase { return tr.List()[0].Phase }
	// What every pane does after a wate restart: Claude Code is resumed and waits for a prompt.
	tr.Hook("p1", "t1", "SessionStart", []byte(`{"source":"resume","session_id":"abc"}`))
	if s := tr.List()[0]; s.Phase != PhaseDone || s.Status != StatusRunning {
		t.Fatalf("resumed: phase %q status %q", s.Phase, s.Status)
	}
	tick(time.Hour)
	if ev := NewWatch().Check(tr.List(), tr.now(), Rules{Waiting: time.Minute, ContextPercent: 90}, nil); len(ev) != 0 {
		t.Errorf("a resumed session left alone is not news: %+v", ev)
	}
	tr.Hook("p1", "t1", "UserPromptSubmit", []byte(`{"prompt":"x"}`))
	if phase() != PhaseWorking {
		t.Fatalf("prompted: %q", phase())
	}
	// Compacting mid-turn starts the session over in name only.
	since := tr.List()[0].PhaseSince
	tick(time.Minute)
	tr.Hook("p1", "t1", "SessionStart", []byte(`{"source":"compact"}`))
	if s := tr.List()[0]; s.Phase != PhaseWorking || !s.PhaseSince.Equal(since) {
		t.Fatalf("compacted: phase %q since %v", s.Phase, s.PhaseSince)
	}
	tr.Hook("p1", "t1", "UserPromptSubmit", []byte(`{"prompt":"y"}`))
	tr.Hook("p1", "t1", "SessionStart", []byte(`{"source":"clear"}`))
	if phase() != PhaseDone {
		t.Fatalf("cleared: %q", phase())
	}
	tr.Hook("p2", "t1", "SessionStart", []byte(`{"source":"startup"}`))
	tr.Hook("p3", "t1", "SessionStart", []byte(`{}`))
	for _, s := range tr.List()[1:] {
		if s.Phase != PhaseDone {
			t.Errorf("%s started: %q", s.PaneID, s.Phase)
		}
	}
}
