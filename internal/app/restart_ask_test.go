package app

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gerry3010/wate/internal/agent"
)

// prompts is a set of Claude Code sessions for the asking loop: what each one is doing, what
// its input box holds, and what got typed into it.
type prompts struct {
	mu     sync.Mutex
	status map[string]string // pane → Claude Code's status
	input  map[string]string // pane → "empty", "typed", "none"
	typed  map[string][]string
	broken bool // typing fails
}

func newPrompts(status map[string]string) *prompts {
	p := &prompts{status: status, input: map[string]string{}, typed: map[string][]string{}}
	for pane := range status {
		p.input[pane] = "empty"
	}
	return p
}

// pids are made up from the pane's position in a fixed list, so the state can find its pane.
var promptPanes = []string{"p-idle", "p-shell", "p-busy", "p-dialog", "p-typing", "p-other", "p-6", "p-7", "p-8", "p-9"}

func (p *prompts) ops() promptOps {
	return promptOps{
		pid: func(pane string) int {
			for i, name := range promptPanes {
				if name == pane {
					if _, ok := p.status[pane]; ok {
						return 100 + i
					}
				}
			}
			return 0
		},
		state: func(pid int) (agent.ProcState, error) {
			pane := promptPanes[pid-100]
			st := agent.ProcState{Status: p.status[pane]}
			if st.Status == "waiting" {
				st.WaitingFor = "permission prompt"
			}
			return st, nil
		},
		input: func(pane string) (string, error) { return p.input[pane], nil },
		write: func(pane, text string) error {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.broken {
				return errors.New("pane went away")
			}
			p.typed[pane] = append(p.typed[pane], text)
			return nil
		},
		sleep: func(time.Duration) {},
	}
}

func (p *prompts) got(pane string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.typed[pane]...)
}

// asking is a service with these sessions running, a round already started and the asking
// loop not running, so each test drives askIdle itself.
func asking(t *testing.T, p *prompts, start func(*RestartService)) *RestartService {
	t.Helper()
	s := restartIn(t)
	s.listSessions = func() []agent.Session {
		var out []agent.Session
		for _, pane := range promptPanes {
			if _, ok := p.status[pane]; ok {
				out = append(out, agent.Session{PaneID: pane, Title: pane})
			}
		}
		return out
	}
	start(s)
	s.prompt = p.ops()
	return s
}

func restarting(s *RestartService) { s.Announce("the new build", 5, true) }

func TestAnIdleAgentIsAskedAtItsPrompt(t *testing.T) {
	p := newPrompts(map[string]string{
		"p-idle": "idle", "p-shell": "shell", "p-busy": "busy", "p-dialog": "waiting", "p-typing": "idle",
	})
	p.input["p-typing"] = "typed"
	s := asking(t, p, restarting)

	if !s.askIdle() {
		t.Fatal("askIdle said nothing was pending")
	}
	for _, pane := range []string{"p-idle", "p-shell"} {
		got := p.got(pane)
		// The Enter goes on its own, or Claude Code takes it for part of a paste.
		if len(got) != 2 || got[1] != "\r" {
			t.Fatalf("%s: typed %q", pane, got)
		}
		line := got[0]
		if !strings.HasPrefix(line, "[wate] wate would like to restart") || strings.Contains(line, "\n") {
			t.Errorf("%s: the question is not one line from wate:\n%s", pane, line)
		}
		if !strings.Contains(line, "wate_restart_respond") || !strings.Contains(line, "background") {
			t.Errorf("%s: the question does not say how to answer, or to mind background work:\n%s", pane, line)
		}
	}
	// A turn in progress is left to the hook path; a dialog or somebody's half-written prompt
	// must not catch the keys.
	for _, pane := range []string{"p-busy", "p-dialog", "p-typing"} {
		if got := p.got(pane); len(got) != 0 {
			t.Errorf("%s: typed %q", pane, got)
		}
	}

	asked := map[string]bool{}
	for _, r := range s.Status().Rows {
		asked[r.Pane] = r.Asked
	}
	if !asked["p-idle"] || !asked["p-shell"] || asked["p-busy"] || asked["p-typing"] {
		t.Errorf("the rows say asked=%v", asked)
	}

	// Asked once is enough until the deadline comes up, by either way.
	s.askIdle()
	if got := p.got("p-idle"); len(got) != 2 {
		t.Errorf("asked again straight away: %q", got)
	}
	if s.TellPane("p-idle") != "" {
		t.Error("the hook told an agent that had just been asked at its prompt")
	}
	if told := s.TellPane("p-busy"); told == "" || strings.HasPrefix(told, "[wate]") {
		t.Errorf("the busy agent was not told by its hook the usual way:\n%s", told)
	}

	// Once the user has finished typing, that session is asked too.
	p.input["p-typing"] = "empty"
	s.askIdle()
	if got := p.got("p-typing"); len(got) != 2 {
		t.Errorf("the session was not asked once its input box was empty: %q", got)
	}
}

func TestAnAgentToldByItsHookIsNotAskedAgain(t *testing.T) {
	p := newPrompts(map[string]string{"p-idle": "idle"})
	s := asking(t, p, restarting)
	// It was told during its last tool call and has gone idle since without answering.
	if s.TellPane("p-idle") == "" {
		t.Fatal("the hook told nothing")
	}
	s.askIdle()
	if got := p.got("p-idle"); len(got) != 0 {
		t.Errorf("asked a second time long before the deadline: %q", got)
	}
}

func TestAnAnsweredAgentIsNotAsked(t *testing.T) {
	p := newPrompts(map[string]string{"p-idle": "idle"})
	s := asking(t, p, restarting)
	if err := s.Respond("p-idle", "go", 0, ""); err != nil {
		t.Fatal(err)
	}
	s.askIdle()
	if got := p.got("p-idle"); len(got) != 0 {
		t.Errorf("asked although it had answered: %q", got)
	}
}

func TestAnIdleAgentIsRemindedAsTheDeadlineComesUp(t *testing.T) {
	p := newPrompts(map[string]string{"p-idle": "idle"})
	s := asking(t, p, restarting)
	s.askIdle()

	rec, _ := readRestart()
	rec.Deadline = time.Now().Add(30 * time.Second).Format(time.RFC3339)
	if err := writeRestart(rec); err != nil {
		t.Fatal(err)
	}
	s.askIdle()
	if got := p.got("p-idle"); len(got) != 4 {
		t.Fatalf("no reminder as the deadline came up: %q", got)
	}
	s.askIdle()
	if got := p.got("p-idle"); len(got) != 4 {
		t.Errorf("reminded more than once: %q", got)
	}
}

func TestTypingThatFailsIsTriedAgain(t *testing.T) {
	p := newPrompts(map[string]string{"p-idle": "idle"})
	p.broken = true
	s := asking(t, p, restarting)
	s.askIdle()
	if rows := s.Status().Rows; len(rows) != 1 || rows[0].Asked {
		t.Fatalf("a question that never went in counts as asked: %+v", rows)
	}
	p.mu.Lock()
	p.broken = false
	p.mu.Unlock()
	s.askIdle()
	if got := p.got("p-idle"); len(got) != 2 {
		t.Errorf("not tried again: %q", got)
	}
}

func TestAClosingWindowOnlyTypesAtItsOwnAgents(t *testing.T) {
	p := newPrompts(map[string]string{"p-idle": "idle", "p-other": "idle"})
	s := asking(t, p, func(s *RestartService) {
		s.windows = NewWindowService(nil)
		for _, id := range []WindowID{"w1", "w2"} {
			s.windows.windows[id] = &winEntry{id: id}
		}
		s.windows.paneWindow["p-idle"] = "w1"
		s.windows.paneWindow["p-other"] = "w2"
		s.start("", 5, false, "w1", "")
	})
	s.askIdle()
	got := p.got("p-idle")
	if len(got) != 2 || !strings.Contains(got[0], "close this wate window") || strings.Contains(got[0], "restart,") {
		t.Errorf("the agent in the closing window was asked %q", got)
	}
	if other := p.got("p-other"); len(other) != 0 {
		t.Errorf("an agent in another window was asked about closing this one: %q", other)
	}
}

func TestTheAskingLoopRunsWithTheRound(t *testing.T) {
	p := newPrompts(map[string]string{"p-idle": "idle"})
	s := restartIn(t)
	s.listSessions = func() []agent.Session { return []agent.Session{{PaneID: "p-idle"}} }
	s.prompt = p.ops()

	s.Announce("the new build", 5, true)
	waitFor(t, "the idle agent to be asked", func() bool { return len(p.got("p-idle")) == 2 })
	s.Cancel()
	waitFor(t, "the loop to end with the round", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.asking
	})
	// A new round gets a loop of its own.
	s.Announce("the new build, again", 5, true)
	waitFor(t, "the agent to be asked in the new round", func() bool { return len(p.got("p-idle")) == 4 })
	s.Cancel()
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2*askEvery + time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waited in vain for %s", what)
}

func (p *prompts) askedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.typed)
}

func TestOnlyAFewIdleAgentsAreAskedAtOnce(t *testing.T) {
	status := map[string]string{}
	for _, pane := range []string{"p-idle", "p-shell", "p-other", "p-6", "p-7", "p-8", "p-9"} {
		status[pane] = "idle"
	}
	p := newPrompts(status)
	s := asking(t, p, restarting)

	s.askIdle()
	if n := p.askedCount(); n != askAtOnce {
		t.Fatalf("%d asked in the first pass, want %d", n, askAtOnce)
	}
	s.askIdle()
	if n := p.askedCount(); n != askAtOnce {
		t.Fatalf("%d asked before anyone answered, want still %d", n, askAtOnce)
	}
	// An answer makes room for the next one.
	var first string
	for _, pane := range promptPanes {
		if len(p.got(pane)) > 0 {
			first = pane
			break
		}
	}
	if err := s.Respond(first, "go", 0, ""); err != nil {
		t.Fatal(err)
	}
	s.askIdle()
	if n := p.askedCount(); n != askAtOnce+1 {
		t.Fatalf("%d asked after one answer, want %d", n, askAtOnce+1)
	}
	// One that keeps quiet for answerWithin stops holding up the rest.
	s.mu.Lock()
	for pane, at := range s.typedAt {
		s.typedAt[pane] = at.Add(-answerWithin)
	}
	s.mu.Unlock()
	s.askIdle()
	if n := p.askedCount(); n != len(status) {
		t.Fatalf("%d asked once the first ones had their time, want all %d", n, len(status))
	}
}

func TestWhenTimeIsUpTheUserDecides(t *testing.T) {
	p := newPrompts(map[string]string{"p-idle": "idle", "p-shell": "idle"})
	s := asking(t, p, restarting)
	var mu sync.Mutex
	var asked []RestartStatus
	s.onExpired = func(st RestartStatus) { mu.Lock(); asked = append(asked, st); mu.Unlock() }
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(asked) }

	if err := s.Respond("p-idle", "go", 0, ""); err != nil {
		t.Fatal(err)
	}
	rec, _ := readRestart()
	s.expire(rec.StartedAt, rec.Deadline)
	if count() != 1 || asked[0].Unanswered != 1 || !asked[0].Pending {
		t.Fatalf("time up with one agent silent: asked %+v", asked)
	}
	if _, ok := readRestart(); !ok {
		t.Fatal("the round was dropped at the deadline; it has to wait for the user")
	}

	// Waiting longer moves the deadline (the round asked for 5 minutes, this for 9, so the two
	// differ even within the same second); the old timer has nothing more to say.
	st := s.Extend(9)
	if st.Deadline == rec.Deadline || st.Expired {
		t.Errorf("deadline after extending: %s (was %s), expired %v", st.Deadline, rec.Deadline, st.Expired)
	}
	s.expire(rec.StartedAt, rec.Deadline)
	if count() != 1 {
		t.Error("a deadline that had been moved still asked the user")
	}

	// Everyone agreed by the time it runs out: the countdown has it, nobody needs asking.
	if err := s.Respond("p-shell", "go", 0, ""); err != nil {
		t.Fatal(err)
	}
	rec, _ = readRestart()
	s.expire(rec.StartedAt, rec.Deadline)
	if count() != 1 {
		t.Error("the user was asked although every agent had agreed")
	}
}
