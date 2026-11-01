package app

import (
	"strings"
	"testing"
	"time"

	"github.com/Gerry3010/wate/internal/agent"
	"github.com/Gerry3010/wate/internal/config"
	"github.com/Gerry3010/wate/internal/ctl"
)

// rootIn builds a RootService over panes (pane → tab) with "root" seated as the root agent
// and tokens "tok-<pane>".
func rootIn(t *testing.T, panes map[string]string, root config.Root) (*RootService, *PtyService) {
	t.Helper()
	p := panesIn(t, panes)
	for pane := range panes {
		p.paneOfToken["tok-"+pane], p.tokenOfPane[pane] = pane, "tok-"+pane
	}
	if _, ok := panes["root"]; ok {
		if err := p.claimRoot("root"); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Defaults()
	cfg.Root = root
	r := NewRootService(&AgentService{}, p, func() config.Config { return cfg })
	return r, p
}

func TestRootHearsOfEachWaitOnceAndOnlyWhenItRuns(t *testing.T) {
	r, p := rootIn(t, map[string]string{"root": "t0", "p1": "t1"}, config.Root{Enabled: true, WaitingMinutes: 10})
	start := time.Unix(1000, 0)
	r.now = func() time.Time { return start.Add(20 * time.Minute) }
	sessions := []agent.Session{
		{PaneID: "p1", Phase: agent.PhasePermission, PhaseSince: start},
		{PaneID: "root", Phase: agent.PhasePermission, PhaseSince: start},
	}
	r.check(sessions)
	got := r.Attention()
	if len(got) != 1 || got[0].PaneID != "p1" || got[0].Kind != agent.EventPermission {
		t.Fatalf("pending: %+v", got)
	}
	r.check(sessions)
	if got := r.Attention(); len(got) != 0 {
		t.Fatalf("handed over twice: %+v", got)
	}
	// No root agent, nothing piles up.
	p.releaseRoot("root")
	r2, _ := rootIn(t, map[string]string{"p1": "t1"}, config.Root{Enabled: true, WaitingMinutes: 10})
	r2.now = r.now
	r2.check(sessions)
	if got := r2.Attention(); len(got) != 0 {
		t.Fatalf("queued without a root agent: %+v", got)
	}
}

func TestPendingIsCapped(t *testing.T) {
	r, _ := rootIn(t, map[string]string{"root": "t0"}, config.Root{Enabled: true, ContextPercent: 50})
	var many []agent.Session
	for i := 0; i < maxPending+20; i++ {
		many = append(many, agent.Session{PaneID: "p" + string(rune('a'+i%26)) + strings.Repeat("x", i/26), SessionID: "s" + strings.Repeat("y", i), ContextPercent: 90})
	}
	r.check(many)
	if got := r.Attention(); len(got) != maxPending {
		t.Fatalf("kept %d", len(got))
	}
}

func TestOnlyTheRootTokenOpensTheRootCommands(t *testing.T) {
	r, p := rootIn(t, map[string]string{"root": "t0", "p1": "t1"}, config.Root{Enabled: true})
	c := NewCtlService(p, nil, nil, nil, nil)
	c.Root = r
	for _, req := range []ctl.Request{
		{Cmd: "attention"},
		{Cmd: "attention", Pane: "root"},
		{Cmd: "attention", Pane: "root", Token: "tok-p1"},
		{Cmd: "sessions", Token: "root"},
		{Cmd: "notify", Name: "x", Token: "tok-p1"},
	} {
		if resp := c.handle(req); resp.OK || !strings.Contains(resp.Error, "only the root agent") {
			t.Errorf("%+v let through: %+v", req, resp)
		}
	}
	if resp := c.handle(ctl.Request{Cmd: "attention", Token: "tok-root"}); !resp.OK {
		t.Fatalf("the root agent was refused: %+v", resp)
	}
}

func TestRootMayOpenOnlySoManySessions(t *testing.T) {
	r, p := rootIn(t, map[string]string{"root": "t0", "a": "t1", "b": "t2"}, config.Root{Enabled: true, MaxSessions: 2})
	now := time.Unix(1000, 0)
	r.now = func() time.Time { return now }
	r.Opened("a")
	if err := r.mayOpen(); err != nil {
		t.Fatalf("one of two: %v", err)
	}
	r.Opened("b")
	if err := r.mayOpen(); err == nil {
		t.Fatal("a third let through")
	}
	// A session that ended frees its place — once it had time to start at all.
	delete(p.byPane, "b")
	delete(p.tabOf, "b")
	if err := r.mayOpen(); err == nil {
		t.Fatal("a pane still spawning was taken for gone")
	}
	now = now.Add(time.Minute)
	if err := r.mayOpen(); err != nil {
		t.Fatalf("after one ended: %v", err)
	}
	r2, _ := rootIn(t, map[string]string{"root": "t0"}, config.Root{Enabled: true, MaxSessions: 0})
	if err := r2.mayOpen(); err == nil {
		t.Fatal("max_sessions = 0 must switch opening off")
	}
}

func TestRootIsRemindedOfAWatcherThatStopped(t *testing.T) {
	r, _ := rootIn(t, map[string]string{"root": "t0", "p1": "t1"}, config.Root{Enabled: true})
	now := time.Unix(1000, 0)
	r.now = func() time.Time { return now }
	if r.Hint("tok-p1", "Stop", "w") != "" {
		t.Fatal("another pane got the root agent's hint")
	}
	if r.Hint("tok-root", "Notification", "w") != "" {
		t.Fatal("a hint on an event whose output nobody reads")
	}
	if h := r.Hint("tok-root", "UserPromptSubmit", "wate ctl watch --once"); !strings.Contains(h, "wate ctl watch --once") {
		t.Fatalf("no hint for a watcher that never ran: %q", h)
	}
	now = now.Add(30 * time.Second)
	if r.Hint("tok-root", "PostToolUse", "w") != "" {
		t.Fatal("hinted again within two minutes")
	}
	r.Attention()
	now = now.Add(5 * time.Minute)
	r.Attention()
	if r.Hint("tok-root", "Stop", "w") != "" {
		t.Fatal("hinted while the watcher was asking")
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/bin/wate":       "/usr/bin/wate",
		"/Applications/wa te": "'/Applications/wa te'",
		"/tmp/it's":           `'/tmp/it'\''s'`,
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestRootSessionIsContinuedWhileItsTranscriptIsThere(t *testing.T) {
	dir := t.TempDir()
	has := map[string]bool{}
	exists := func(id string) bool { return has[id] }
	first, resume, err := rootSession(dir, exists)
	if err != nil || resume || first == "" {
		t.Fatalf("fresh: %q %v %v", first, resume, err)
	}
	// Started but never got as far as a transcript: start fresh again.
	second, resume, _ := rootSession(dir, exists)
	if resume || second == first {
		t.Fatalf("no transcript yet, still resumed: %q %v", second, resume)
	}
	has[second] = true
	third, resume, _ := rootSession(dir, exists)
	if !resume || third != second {
		t.Fatalf("transcript there, not resumed: %q %v", third, resume)
	}
}

func TestRootHostIsTheWindowThatSaysSo(t *testing.T) {
	blobs := []WindowBlob{
		{Key: "w-1", Data: `{"key":"w-1","active":0,"tabs":[]}`},
		{Key: "w-2", Data: `{"key":"w-2","active":0,"tabs":[],"root":true}`},
		{Key: "w-3", Data: `not json`},
	}
	if got := RootHost(blobs); got != "w-2" {
		t.Fatalf("host %q", got)
	}
	if got := RootHost(blobs[:1]); got != "" {
		t.Fatalf("no window said so, got %q", got)
	}
}
