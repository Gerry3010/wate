package app

import (
	"strings"
	"testing"

	"github.com/Gerry3010/wate/internal/ctl"
)

// panesIn builds a PtyService that believes these panes exist in these tabs.
func panesIn(t *testing.T, m map[string]string) *PtyService {
	t.Helper()
	p := NewPtyService(nil, nil)
	for pane, tab := range m {
		p.tabOf[pane] = tab
		p.byPane[pane] = "session-" + pane
	}
	return p
}

func TestAPaneMayAlwaysActOnItself(t *testing.T) {
	a := NewAccessService(panesIn(t, map[string]string{"a": "t1"}))
	for _, want := range []Right{RightRead, RightWrite, RightManage} {
		if err := a.Allow("a", "a", want); err != nil {
			t.Errorf("own pane, %s: %v", want, err)
		}
	}
}

func TestOwningAPaneIsEnough(t *testing.T) {
	a := NewAccessService(panesIn(t, map[string]string{"agent": "t1", "side": "t1"}))
	a.Opened("agent", "side")
	for _, want := range []Right{RightRead, RightWrite, RightManage} {
		if err := a.Allow("agent", "side", want); err != nil {
			t.Errorf("opened pane, %s: %v", want, err)
		}
	}
	if !a.Owns("agent", "side") {
		t.Error("the pane that opened it is not its owner")
	}
}

func TestAnUngrantedPaneIsRefused(t *testing.T) {
	a := NewAccessService(panesIn(t, map[string]string{"agent": "t1", "mine": "t1"}))
	err := a.Allow("agent", "mine", RightRead)
	if err == nil {
		t.Fatal("a pane nobody opened up was readable")
	}
	// The message has to say how to fix it; a bare "denied" sends the agent round in circles.
	if !strings.Contains(err.Error(), "menu") {
		t.Errorf("unhelpful refusal: %v", err)
	}
}

func TestAGrantOpensExactlyWhatItNames(t *testing.T) {
	a := NewAccessService(panesIn(t, map[string]string{"agent": "t1", "mine": "t1"}))
	a.Grant("mine", Access{Read: true})
	if err := a.Allow("agent", "mine", RightRead); err != nil {
		t.Errorf("read was granted but refused: %v", err)
	}
	if err := a.Allow("agent", "mine", RightWrite); err == nil {
		t.Error("granting read also allowed writing")
	}
	if err := a.Allow("agent", "mine", RightManage); err == nil {
		t.Error("granting read also allowed managing")
	}
}

func TestRevokingTakesItBack(t *testing.T) {
	a := NewAccessService(panesIn(t, map[string]string{"agent": "t1", "mine": "t1"}))
	a.Grant("mine", Access{Read: true, Write: true})
	a.Grant("mine", Access{})
	if err := a.Allow("agent", "mine", RightRead); err == nil {
		t.Error("a revoked grant still allowed reading")
	}
	if got := a.Of("mine"); got != (Access{}) {
		t.Errorf("Of = %+v after revoking", got)
	}
}

func TestTheTabIsTheBoundary(t *testing.T) {
	a := NewAccessService(panesIn(t, map[string]string{"agent": "t1", "elsewhere": "t2"}))
	// Even a grant does not reach across tabs: the whole feature is scoped to one tab.
	a.Grant("elsewhere", Access{Read: true, Write: true, Manage: true})
	err := a.Allow("agent", "elsewhere", RightRead)
	if err == nil || !strings.Contains(err.Error(), "another tab") {
		t.Fatalf("err = %v, want a refusal naming the tab", err)
	}
}

func TestOwnershipDoesNotSurviveTheOwner(t *testing.T) {
	a := NewAccessService(panesIn(t, map[string]string{"agent": "t1", "side": "t1", "other": "t1"}))
	a.Opened("agent", "side")
	a.Forget("agent")
	if err := a.Allow("agent", "side", RightWrite); err == nil {
		t.Error("a pane kept its rights after the owner was gone")
	}
	// And a pane that is forgotten takes its own grant with it.
	a.Grant("other", Access{Read: true})
	a.Forget("other")
	if got := a.Of("other"); got != (Access{}) {
		t.Errorf("Of = %+v after the pane was forgotten", got)
	}
}

func TestAnUnknownPaneIsNotAnOpening(t *testing.T) {
	a := NewAccessService(panesIn(t, map[string]string{"agent": "t1"}))
	if err := a.Allow("agent", "ghost", RightRead); err == nil {
		t.Error("a pane that does not exist was readable")
	}
	if err := a.Allow("", "agent", RightRead); err == nil {
		t.Error("a caller with no pane was allowed in")
	}
}

func TestTypableKeepsTextAndDropsTheKeysThatAct(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ls -la", "ls -la"},
		{"ls -la\n", "ls -la"},
		{"ls\r\nrm -rf /\r\n", "lsrm -rf /"},
		{"a\tb", "ab"},
		{"\x1b[A", "[A"},               // the escape goes; the rest is just text on the prompt
		{"héllo wörld", "héllo wörld"}, // non-ASCII is text, not control
		{"\x00\x07", ""},
	}
	for _, c := range cases {
		if got := typable(c.in); got != c.want {
			t.Errorf("typable(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWhoIsAskingIsNotWhatIsBeingActedOn(t *testing.T) {
	// The pane a request *names* must never become the pane it comes *from*: if it did, every
	// rights check would pass, because a pane may always act on itself. That is exactly what
	// happened the first time this was wired up, and the unit tests above did not see it
	// because they call Allow directly.
	if got := targetOf(ctl.Request{Pane: "caller", Target: "victim"}, "caller"); got != "victim" {
		t.Errorf("target = %q, want the named pane", got)
	}
	if got := targetOf(ctl.Request{Pane: "caller"}, "caller"); got != "caller" {
		t.Errorf("target = %q, want the caller's own pane when none is named", got)
	}
	// And a caller resolved from a token still acts on the pane it named, not on itself.
	if got := targetOf(ctl.Request{Pane: "stale-claim", Target: "victim"}, "from-token"); got != "victim" {
		t.Errorf("target = %q, want the named pane", got)
	}
}

func TestEachRightStandsOnItsOwn(t *testing.T) {
	// Ticking Manage fills the other two in as well, but that happens in the menu: here each
	// one is simply what it says, so revoking one never quietly leaves another behind.
	a := NewAccessService(panesIn(t, map[string]string{"agent": "t1", "mine": "t1"}))
	a.Grant("mine", Access{Manage: true})
	if err := a.Allow("agent", "mine", RightManage); err != nil {
		t.Errorf("manage was refused: %v", err)
	}
	if err := a.Allow("agent", "mine", RightRead); err == nil {
		t.Error("a bare manage grant allowed reading the screen")
	}
	if err := a.Allow("agent", "mine", RightWrite); err == nil {
		t.Error("a bare manage grant allowed typing")
	}
}

func TestWritingDoesNotQuietlyGrantReading(t *testing.T) {
	// Reading is the one that carries a privacy cost: whatever is on that screen is whatever
	// the user typed. Nothing grants it as a side effect.
	a := NewAccessService(panesIn(t, map[string]string{"agent": "t1", "mine": "t1"}))
	a.Grant("mine", Access{Write: true})
	if err := a.Allow("agent", "mine", RightRead); err == nil {
		t.Error("granting write also allowed reading the screen")
	}
}
