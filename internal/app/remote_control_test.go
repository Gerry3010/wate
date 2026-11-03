package app

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Gerry3010/wate/internal/agent"
	"github.com/Gerry3010/wate/internal/config"
	"github.com/Gerry3010/wate/internal/ctl"
)

// fakeSession is a Claude Code session for remoteControl: its state before anything is typed,
// what it turns into once /remote-control has gone in, and its input box.
type fakeSession struct {
	before, after agent.ProcState
	input         string
	typed         []string
}

func (f *fakeSession) ops() remoteControlOps {
	return remoteControlOps{
		pid: func(pane string) int {
			if pane == "p1" {
				return 41
			}
			return 0
		},
		state: func(int) (agent.ProcState, error) {
			if len(f.typed) == 2 {
				return f.after, nil
			}
			return f.before, nil
		},
		input: func(string) (string, error) { return f.input, nil },
		write: func(_, text string) error {
			f.typed = append(f.typed, text)
			return nil
		},
		sleep: func(time.Duration) {},
	}
}

func TestRemoteControlIsTypedOnlyAtAnEmptyPrompt(t *testing.T) {
	idle := agent.ProcState{Status: "idle"}
	for _, tc := range []struct {
		name  string
		state agent.ProcState
		input string
		want  string
	}{
		{"turn running", agent.ProcState{Status: "busy"}, "empty", "busy: the session"},
		{"permission prompt", agent.ProcState{Status: "waiting", WaitingFor: "permission prompt"}, "empty", "waiting: the session in pane p1 shows a permission prompt"},
		{"user typing", idle, "typed", "input not empty"},
		{"no box on screen", idle, "none", "no empty prompt"},
		{"not saying", agent.ProcState{}, "empty", "does not say"},
	} {
		f := &fakeSession{before: tc.state, input: tc.input}
		_, err := remoteControl(f.ops(), "p1", "")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
		if len(f.typed) != 0 {
			t.Errorf("%s: typed %q", tc.name, f.typed)
		}
	}

	f := &fakeSession{before: idle, input: "empty"}
	if _, err := remoteControl(f.ops(), "p2", ""); err == nil || !strings.Contains(err.Error(), "no Claude Code") {
		t.Errorf("a pane without Claude Code: %v", err)
	}
	ops := f.ops()
	ops.state = func(int) (agent.ProcState, error) { return agent.ProcState{}, errors.New("gone") }
	if _, err := remoteControl(ops, "p1", ""); err == nil || len(f.typed) != 0 {
		t.Errorf("no state file: %v, typed %q", err, f.typed)
	}
}

func TestRemoteControlTypesTheCommandAndWaitsForTheLink(t *testing.T) {
	f := &fakeSession{
		before: agent.ProcState{Status: "shell"}, input: "empty",
		after: agent.ProcState{Status: "shell", BridgeSessionID: "session_01X"},
	}
	got, err := remoteControl(f.ops(), "p1", "nightly build")
	if err != nil {
		t.Fatal(err)
	}
	// The Enter goes on its own, or Claude Code takes it for part of a paste.
	if len(f.typed) != 2 || f.typed[0] != "/remote-control nightly build" || f.typed[1] != "\r" {
		t.Fatalf("typed %q", f.typed)
	}
	if !got.On || got.URL != "https://claude.ai/code/session_01X" || got.Already {
		t.Fatalf("got %+v", got)
	}
}

func TestRemoteControlThatIsOnAlreadyIsLeftAlone(t *testing.T) {
	// Typing /remote-control again would open its dialog.
	f := &fakeSession{before: agent.ProcState{Status: "busy", BridgeSessionID: "session_01X"}, input: "typed"}
	got, err := remoteControl(f.ops(), "p1", "")
	if err != nil || !got.On || !got.Already || got.URL == "" || len(f.typed) != 0 {
		t.Fatalf("%+v %v, typed %q", got, err, f.typed)
	}
}

func TestRemoteControlReportsWhatCameOfIt(t *testing.T) {
	f := &fakeSession{
		before: agent.ProcState{Status: "idle"}, input: "empty",
		after: agent.ProcState{Status: "waiting", WaitingFor: "dialog open"},
	}
	got, err := remoteControl(f.ops(), "p1", "")
	if err != nil || got.On || !strings.Contains(got.Note, "dialog open") {
		t.Fatalf("a dialog after typing: %+v %v", got, err)
	}
	f = &fakeSession{before: agent.ProcState{Status: "idle"}, input: "empty", after: agent.ProcState{Status: "idle"}}
	got, err = remoteControl(f.ops(), "p1", "")
	if err != nil || got.On || !strings.Contains(got.Note, "not connected yet") {
		t.Fatalf("no link yet: %+v %v", got, err)
	}
}

func TestRemoteControlName(t *testing.T) {
	for in, want := range map[string]string{"": "", "  wate  ": "wate", "nightly build": "nightly build"} {
		if got, err := remoteControlName(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"--dangerously-skip-permissions", "two\nlines", "tab\there"} {
		if _, err := remoteControlName(bad); err == nil {
			t.Errorf("%q let through", bad)
		}
	}
}

func TestRemoteControlIsTheRootAgentsAndNotForItself(t *testing.T) {
	r, p := rootIn(t, map[string]string{"root": "t0", "p1": "t1"}, config.Root{Enabled: true})
	c := NewCtlService(p, nil, nil, nil, nil)
	c.Root = r
	for req, want := range map[*ctl.Request]string{
		{Cmd: "session-remote-control", Target: "p1", Token: "tok-p1"}:                                   "only the root agent",
		{Cmd: "session-remote-control", Target: "root", Token: "tok-root"}:                               "your own session",
		{Cmd: "session-remote-control", Target: "nope", Token: "tok-root"}:                               "no such pane",
		{Cmd: "session-remote-control", Target: "", Token: "tok-root"}:                                   "needs a pane",
		{Cmd: "session-remote-control", Target: "p1", Name: "-x", Token: "tok-root"}:                     "must not start",
		{Cmd: "session-remote-control", Target: "p1", Token: "tok-root"}:                                 "no Claude Code session",
		{Cmd: "session-open", Data: []byte(`{"cwd":"/","remote_control_name":"-x"}`), Token: "tok-root"}: "must not start",
	} {
		if resp := c.handle(*req); resp.OK || !strings.Contains(resp.Error, want) {
			t.Errorf("%+v: %+v, want %q", *req, resp, want)
		}
	}
}

func TestSessionsSayWhetherRemoteControlIsOn(t *testing.T) {
	r, _ := rootIn(t, map[string]string{"root": "t0", "p1": "t1", "p2": "t2"}, config.Root{Enabled: true})
	r.procState = func(pane string) (agent.ProcState, bool) {
		if pane == "p1" {
			return agent.ProcState{Status: "idle", BridgeSessionID: "session_01X"}, true
		}
		return agent.ProcState{}, pane == "p2"
	}
	got := r.sessions([]agent.Session{{PaneID: "p1"}, {PaneID: "p2"}, {PaneID: "p3"}})
	if !got[0].RemoteControl || got[0].RemoteControlURL != "https://claude.ai/code/session_01X" {
		t.Errorf("p1: %+v", got[0])
	}
	if got[1].RemoteControl || got[1].RemoteControlURL != "" || got[2].RemoteControl {
		t.Errorf("p2, p3: %+v %+v", got[1], got[2])
	}
}

func TestSessionsStartWithRemoteControlWhenAskedTo(t *testing.T) {
	c := NewConfigService(filepath.Join(t.TempDir(), "config.toml"))
	got := c.claudeArgs("", "fix the build", "yate", "nightly")
	if len(got) < 2 || got[1] != "fix the build" {
		t.Fatalf("the prompt must come right after the command: %q", got)
	}
	i := slices.Index(got, "--remote-control")
	if i < 0 || i+1 >= len(got) || got[i+1] != "nightly" {
		t.Fatalf("no --remote-control nightly: %q", got)
	}
	if slices.Contains(c.claudeArgs("", "fix the build", "yate", ""), "--remote-control") {
		t.Fatal("Remote Control without being asked")
	}
}
