package app

import (
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/Gerry3010/wate/internal/agent"
)

func TestTheNotificationSaysWhatAndWhere(t *testing.T) {
	for _, c := range []struct{ message, cwd, want string }{
		{"fix the bug", "/home/g/work/wate", "fix the bug — ~/work/wate"},
		{"", "/home/g/work", "~/work"},
		{"fix the bug", "", "fix the bug"},
		{"fix the bug", "/home/g", "fix the bug — ~"},
		// Only the home directory itself, not a sibling that merely starts with its name.
		{"x", "/home/gerald/work", "x — /home/gerald/work"},
	} {
		if got := notificationBody(c.message, c.cwd, "/home/g"); got != c.want {
			t.Errorf("(%q, %q): got %q, want %q", c.message, c.cwd, got, c.want)
		}
	}
}

func TestAClickOnlyGoesWhereThePaneIs(t *testing.T) {
	n := &NoticeService{windows: NewWindowService(nil), now: time.Now}
	click := func(action string, info map[string]interface{}) {
		n.onDesktopClick(notifications.NotificationResult{Response: notifications.NotificationResponse{
			ID: summaryID, ActionIdentifier: action, UserInfo: info,
		}})
	}
	// Neither a pane wate no longer knows nor some other action may get anywhere near a window;
	// both have to come back quietly instead of focusing whatever happens to be there.
	click(notifications.DefaultActionIdentifier, map[string]interface{}{"pane": "gone", "tab": "t1"})
	click("snooze", map[string]interface{}{"pane": "p1"})
	click(notifications.DefaultActionIdentifier, nil)
}

func TestTheRootAgentIsNewsOnlyWhenStuck(t *testing.T) {
	for _, c := range []struct {
		name string
		s    agent.Session
		want bool
	}{
		{"another session finished", agent.Session{PaneID: "p1", Status: agent.StatusDone}, true},
		{"another session idle", agent.Session{PaneID: "p1", Status: agent.StatusWaiting, Phase: agent.PhaseDone}, true},
		{"root finished", agent.Session{PaneID: "root", Status: agent.StatusDone, Phase: agent.PhaseDone}, false},
		{"root idle reminder", agent.Session{PaneID: "root", Status: agent.StatusWaiting, Phase: agent.PhaseDone}, false},
		{"root on a prompt", agent.Session{PaneID: "root", Status: agent.StatusWaiting, Phase: agent.PhasePermission}, true},
	} {
		if got := worthNotifying(c.s, "root"); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestABellEntryNamesTheSession(t *testing.T) {
	n := claudeNotice(agent.Session{PaneID: "p1", Status: agent.StatusDone, Title: "Fix the login", Message: "run the tests", Cwd: "/home/g/work"}, "/home/g")
	if n.Title != "Fix the login" || n.Kind != "finished" || n.Body != "Finished: run the tests — ~/work" || n.Key != "claude:p1" {
		t.Errorf("got %+v", n)
	}
	n = claudeNotice(agent.Session{PaneID: "p2", Status: agent.StatusWaiting, Message: "Claude needs your permission to use Bash"}, "/home/g")
	if n.Title != "Claude Code" || n.Kind != "waiting" || !strings.HasPrefix(n.Body, "Waiting for you: Claude needs") {
		t.Errorf("got %+v", n)
	}
}
