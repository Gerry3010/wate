package app

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Gerry3010/wate/internal/agent"
	"github.com/Gerry3010/wate/internal/config"
	"github.com/Gerry3010/wate/internal/ctl"
)

// rootRound is a restart service with a root agent in "p-root" and one other session.
func rootRound(t *testing.T) *RestartService {
	t.Helper()
	s := restartIn(t)
	p := &PtyService{}
	if err := p.claimRoot("p-root"); err != nil {
		t.Fatal(err)
	}
	s.agents = &AgentService{pty: p}
	s.listSessions = func() []agent.Session {
		return []agent.Session{{PaneID: "p-root", Title: "root"}, {PaneID: "p-1", Title: "work"}}
	}
	return s
}

func TestTheRootAgentsRoundCountsItAsGo(t *testing.T) {
	s := rootRound(t)
	st, err := s.announceForRoot("new build", 5)
	if err != nil {
		t.Fatal(err)
	}
	if st.By != "root" || !st.Relaunch || st.Agreed != 1 || st.Unanswered != 1 {
		t.Fatalf("status: %+v", st)
	}
	for _, r := range st.Rows {
		if r.Pane == "p-root" && r.Verdict != "go" {
			t.Errorf("the root agent was not counted as go: %+v", r)
		}
	}
	if _, err := s.announceForRoot("again", 5); err == nil {
		t.Error("a second round replaced the running one")
	}
	if _, err := s.cancelForRoot(); err != nil {
		t.Fatalf("the root agent could not call off its own round: %v", err)
	}
	if _, pending := readRestart(); pending {
		t.Fatal("the round is still there")
	}
}

func TestTheRootAgentCannotCallOffTheUsersRound(t *testing.T) {
	s := rootRound(t)
	s.Announce("", 5, true)
	if _, err := s.cancelForRoot(); err == nil {
		t.Fatal("the root agent called off the user's round")
	}
	if _, err := s.announceForRoot("mine", 5); err == nil {
		t.Fatal("the root agent took over the user's round")
	}
	if _, pending := readRestart(); !pending {
		t.Fatal("the user's round is gone")
	}
}

func TestARestartIsCalledOffWhenTheNewWateWouldNotRun(t *testing.T) {
	s := rootRound(t)
	s.Announce("", 5, true)
	s.beforeRelaunch = func(RestartRecord) error { return errors.New("the installed wate does not run") }
	s.Proceed(true)
	if s.Relaunch() {
		t.Fatal("restarting into a wate that does not run")
	}
	if _, pending := readRestart(); pending {
		t.Error("the called-off round is still pending")
	}

	var seen RestartRecord
	s.Announce("ok now", 5, true)
	s.beforeRelaunch = func(rec RestartRecord) error { seen = rec; return nil }
	s.Proceed(true)
	if !s.Relaunch() || seen.Reason != "ok now" {
		t.Fatalf("relaunch %v, the check saw %+v", s.Relaunch(), seen)
	}
}

func TestTheManifestNamesSessionsByTheirIDs(t *testing.T) {
	reply := `[{"id":"tab-1","title":"ÖPG","color":"blue","index":1,"active":true,"panes":[` +
		`{"id":"p-1","kind":"terminal","cwd":"/w/opg","command":"claude"},{"id":"p-2","kind":"editor","cwd":""}]}]`
	sessions := map[string]RootSession{"p-1": {
		Session:      agent.Session{PaneID: "p-1", SessionID: "abc-123", Title: "Fix the login", Cwd: "/w/opg"},
		OpenedByRoot: true, RemoteControl: true,
	}}
	mw, err := manifestWindow(1, reply, sessions)
	if err != nil {
		t.Fatal(err)
	}
	if len(mw.Tabs) != 1 || mw.Tabs[0].Title != "ÖPG" || mw.Tabs[0].Color != "blue" || mw.Tabs[0].Index != 1 {
		t.Fatalf("tab: %+v", mw.Tabs)
	}
	p := mw.Tabs[0].Panes[0]
	if p.SessionID != "abc-123" || p.SessionTitle != "Fix the login" || !p.OpenedByRoot || !p.RemoteControl || p.Cwd != "/w/opg" {
		t.Fatalf("pane: %+v", p)
	}
	if e := mw.Tabs[0].Panes[1]; e.Kind != "editor" || e.SessionID != "" {
		t.Fatalf("editor pane: %+v", e)
	}
}

func TestOnlyAFreshManifestReachesTheRootAgent(t *testing.T) {
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	now := time.Date(2026, 11, 26, 1, 0, 0, 0, time.UTC)
	write := func(at time.Time) {
		data, _ := json.Marshal(RestartManifest{WrittenAt: at.Format(time.RFC3339), RequestedBy: "root", Reason: "new build"})
		if err := os.WriteFile(manifestPath(), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := freshManifest(now); ok {
		t.Fatal("a manifest out of nothing")
	}
	write(now.Add(-time.Minute))
	if m, ok := freshManifest(now); !ok || m.RequestedBy != "root" || m.Reason != "new build" {
		t.Fatalf("fresh: %+v %v", m, ok)
	}
	write(now.Add(-manifestFresh - time.Minute))
	if _, ok := freshManifest(now); ok {
		t.Fatal("an old manifest was taken for this restart")
	}
}

func TestOnlyTheRootAgentMayRestartOrResume(t *testing.T) {
	r, p := rootIn(t, map[string]string{"root": "t0", "p1": "t1"}, config.Root{Enabled: true})
	c := NewCtlService(p, nil, nil, nil, nil)
	c.Root = r
	c.Restart = restartIn(t)
	for _, cmd := range []string{"restart-root", "restart-cancel", "restart-manifest", "session-resume"} {
		if resp := c.handle(ctl.Request{Cmd: cmd, Token: "tok-p1"}); resp.OK {
			t.Errorf("%s let through for an ordinary session", cmd)
		}
	}
	if resp := c.handle(ctl.Request{Cmd: "restart-root", Text: "new build", Token: "tok-root"}); !resp.OK {
		t.Fatalf("the root agent could not ask for a restart: %+v", resp)
	}
	if rec, ok := readRestart(); !ok || rec.By != "root" || !rec.Relaunch || rec.Reason != "new build" {
		t.Fatalf("record: %+v", rec)
	}
}
