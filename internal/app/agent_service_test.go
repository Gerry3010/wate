package app

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"
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
	a := &AgentService{windows: NewWindowService(nil)}
	click := func(action string, info map[string]interface{}) {
		a.onNotificationResponse(notifications.NotificationResult{Response: notifications.NotificationResponse{
			ID: "wate-p1", ActionIdentifier: action, UserInfo: info,
		}})
	}
	// Neither a pane wate no longer knows nor some other action may get anywhere near a window;
	// both have to come back quietly instead of focusing whatever happens to be there.
	click(notifications.DefaultActionIdentifier, map[string]interface{}{"pane": "gone", "tab": "t1"})
	click("snooze", map[string]interface{}{"pane": "p1"})
	click(notifications.DefaultActionIdentifier, nil)
}
