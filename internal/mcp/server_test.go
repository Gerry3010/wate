package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gerry3010/wate/internal/ctl"
)

// sockPath is a short path to listen on.
//
// Not t.TempDir(): that spells the test's name into the directory, and a Unix socket's path
// is capped at around a hundred characters. On macOS the temporary root eats half of that on
// its own, so a descriptive test name is enough to turn a listen into "invalid argument" —
// which is exactly how this was found, on CI and not here.
func sockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "wate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "w.sock")
}

func TestSizeUnderstandsNamesAndFractions(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		bad  bool
	}{
		{in: "", want: 0.5},
		{in: "half", want: 0.5},
		{in: "Half", want: 0.5},
		{in: "1/2", want: 0.5},
		{in: "third", want: 1.0 / 3.0},
		{in: "two-thirds", want: 2.0 / 3.0},
		{in: " 0.4 ", want: 0.4},
		{in: "0", bad: true},
		{in: "1", bad: true},
		{in: "120%", bad: true},
		{in: "wide", bad: true},
	}
	for _, c := range cases {
		got, err := size(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("size(%q) = %v, want a refusal", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("size(%q): %v", c.in, err)
			continue
		}
		if diff := got - c.want; diff > 0.001 || diff < -0.001 {
			t.Errorf("size(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDirTakesTheWordsAModelWouldUse(t *testing.T) {
	for _, w := range []string{"beside", "right", "row", "width", "HORIZONTAL"} {
		if got := dirOf(w, "col"); got != "row" {
			t.Errorf("dirOf(%q) = %q, want row", w, got)
		}
	}
	for _, w := range []string{"below", "down", "col", "column", "height", "vertical"} {
		if got := dirOf(w, "row"); got != "col" {
			t.Errorf("dirOf(%q) = %q, want col", w, got)
		}
	}
	if got := dirOf("sideways-ish", "row"); got != "row" {
		t.Errorf("an unknown word changed the default: %q", got)
	}
}

// fakeWate stands in for a running wate on a socket of its own.
func fakeWate(t *testing.T) (sock string, seen *[]ctl.Request) {
	t.Helper()
	got := make([]ctl.Request, 0, 4)
	sock = sockPath(t)
	srv, err := ctl.Listen(sock, func(r ctl.Request) ctl.Response {
		if r.Cmd == "ping" {
			return ctl.Response{OK: true, Data: "pong"}
		}
		got = append(got, r)
		return ctl.Response{OK: true, Data: "pane-new"}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return sock, &got
}

func TestEveryRequestSaysWhoIsAsking(t *testing.T) {
	sock, seen := fakeWate(t)
	t.Setenv("WATE_SOCKET", sock)
	t.Setenv("WATE_PANE_ID", "pane-7")
	t.Setenv("WATE_PANE_TOKEN", "tok-7")

	out, err := send(ctl.Request{Cmd: "pane-split", Dir: "col", Ratio: 0.25})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if out != "pane-new" {
		t.Errorf("answer = %q", out)
	}
	if len(*seen) != 1 {
		t.Fatalf("%d requests reached wate", len(*seen))
	}
	r := (*seen)[0]
	// The pane and the token are filled in from the environment, never by the caller: a tool
	// argument naming a pane is the *target*, and must not be able to pose as the asker.
	if r.Pane != "pane-7" || r.Token != "tok-7" {
		t.Errorf("pane/token = %q/%q, want pane-7/tok-7", r.Pane, r.Token)
	}
	if r.Dir != "col" || r.Ratio != 0.25 {
		t.Errorf("dir/ratio = %q/%v", r.Dir, r.Ratio)
	}
}

func TestTheTargetStaysTheTarget(t *testing.T) {
	sock, seen := fakeWate(t)
	t.Setenv("WATE_SOCKET", sock)
	t.Setenv("WATE_PANE_ID", "pane-7")
	t.Setenv("WATE_PANE_TOKEN", "tok-7")

	if _, err := send(ctl.Request{Cmd: "pane-read", Target: "pane-9", Lines: 5}); err != nil {
		t.Fatalf("send: %v", err)
	}
	r := (*seen)[0]
	if r.Target != "pane-9" {
		t.Errorf("target = %q, want pane-9", r.Target)
	}
	if r.Pane != "pane-7" {
		t.Errorf("the target overwrote the asker: pane = %q", r.Pane)
	}
}

func TestOutsideAPaneItSaysSoInsteadOfGuessing(t *testing.T) {
	t.Setenv("WATE_PANE_ID", "")
	t.Setenv("WATE_SOCKET", "")
	t.Setenv("WATE_PANE_TOKEN", "")
	if _, err := send(ctl.Request{Cmd: "pane-list"}); err == nil {
		t.Fatal("a server outside a pane pretended it could work")
	}
}

func TestAnErrorFromWateComesBackAsOne(t *testing.T) {
	sock := sockPath(t)
	srv, err := ctl.Listen(sock, func(r ctl.Request) ctl.Response {
		if r.Cmd == "ping" {
			return ctl.Response{OK: true, Data: "pong"}
		}
		return ctl.Response{Error: "pane \"x\" has not been opened up for read"}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	t.Setenv("WATE_SOCKET", sock)
	t.Setenv("WATE_PANE_ID", "pane-7")
	t.Setenv("WATE_PANE_TOKEN", "")

	_, err = send(ctl.Request{Cmd: "pane-read", Target: "x"})
	if err == nil || err.Error() != "pane \"x\" has not been opened up for read" {
		t.Fatalf("err = %v, want wate's own words", err)
	}
}

func TestWaitingForUserSpotsWhatOnlyTheUserCanAnswer(t *testing.T) {
	yes := []string{
		"[sudo] password for gerry: ",
		"gerry@host's password:",
		"Enter passphrase for key '/home/gerry/.ssh/id_ed25519':",
		"Verification code:",
		// ssh states the authenticity line and then asks on the next one; it is the asking
		// line that is left hanging, and the only one worth matching.
		"The authenticity of host 'srv (1.2.3.4)' can't be established.\nAre you sure you want to continue connecting (yes/no/[fingerprint])?",
	}
	for _, s := range yes {
		if !waitingForUser(s) {
			t.Errorf("missed a prompt that needs the user: %q", s)
		}
	}
	no := []string{
		"",
		"$ ls -la",
		"total 48\ndrwxr-xr-x 1 gerry gerry 4096 Oct 10 03:00 .",
		"Compiling wate v0.1.0",
		// The command that was typed is not the prompt it will produce.
		"$ grep -n password config.yml",
		"$ sudo systemctl restart nginx",
		// Nor is output that merely mentions one.
		"config.yml:12:  password: hunter2",
	}
	for _, s := range no {
		if waitingForUser(s) {
			t.Errorf("saw a prompt in ordinary output: %q", s)
		}
	}
}

func TestOnlyTheEndOfTheScreenCounts(t *testing.T) {
	// A password prompt answered ten minutes ago is still somewhere in the scrollback, and
	// mistaking it for a live one would stop every command after it.
	old := "[sudo] password for gerry:\n" + strings.Repeat("output line\n", 20)
	if waitingForUser(old) {
		t.Error("an old prompt far up the screen was taken for a live one")
	}
}

// wateWithout answers like a wate in which `gone` does not exist: pane-status refuses it,
// a split hands back a fresh id, everything else succeeds.
func wateWithout(t *testing.T, gone string) (sock string, splits *int) {
	t.Helper()
	n := 0
	sock = sockPath(t)
	srv, err := ctl.Listen(sock, func(r ctl.Request) ctl.Response {
		switch {
		case r.Cmd == "ping":
			return ctl.Response{OK: true, Data: "pong"}
		case r.Cmd == "pane-status" && r.Target == gone:
			return ctl.Response{Error: `no such pane "` + gone + `"`}
		case r.Cmd == "pane-status":
			return ctl.Response{OK: true, Data: `{"name":""}`}
		case r.Cmd == "pane-split":
			n++
			return ctl.Response{OK: true, Data: "pane-fresh"}
		}
		return ctl.Response{OK: true, Data: ""}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	t.Setenv("WATE_SOCKET", sock)
	t.Setenv("WATE_PANE_ID", "pane-7")
	t.Setenv("WATE_PANE_TOKEN", "tok-7")
	return sock, &n
}

func TestAWorkPaneThatIsGoneIsOpenedAgain(t *testing.T) {
	// This server outlives the panes it opens: the user can close one, and a wate restart
	// takes them all. Holding on to the id regardless is how every later run ended up failing
	// on a pane that had not existed for hours, without ever trying to open a new one.
	_, splits := wateWithout(t, "pane-old")
	workPane = "pane-old"
	t.Cleanup(func() { workPane = "" })

	got, err := workPaneFor("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "pane-fresh" {
		t.Errorf("ran in %q, want a newly opened pane", got)
	}
	if *splits != 1 {
		t.Errorf("%d panes opened, want exactly one", *splits)
	}
	// And the new one is kept, so the next call does not open yet another.
	if got, err := workPaneFor(""); err != nil || got != "pane-fresh" {
		t.Errorf("second call = %q, %v", got, err)
	}
	if *splits != 1 {
		t.Errorf("%d panes opened after two calls, want one", *splits)
	}
}

func TestANamedPaneIsNeverSecondGuessed(t *testing.T) {
	// Naming a pane is the user's or the agent's own decision; if it is wrong, the refusal
	// should say so rather than a surprise pane appearing beside it.
	_, splits := wateWithout(t, "pane-old")
	workPane = ""
	t.Cleanup(func() { workPane = "" })
	if got, err := workPaneFor("pane-old"); err != nil || got != "pane-old" {
		t.Errorf("workPaneFor(named) = %q, %v", got, err)
	}
	if *splits != 0 {
		t.Errorf("%d panes opened for a named pane, want none", *splits)
	}
}
