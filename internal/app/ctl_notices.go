package app

import (
	"errors"
	"strings"

	"github.com/Gerry3010/wate/internal/ctl"
)

var errNoNotices = errors.New("notifications are not set up")

// notify puts a notice in the bell. Any session may: it is how an agent says that something
// needs the user without typing into anybody's terminal. The root agent's notices keep their
// own look (and their own key, one per pane they are about).
func (c *CtlService) notify(r ctl.Request) ctl.Response {
	if c.Notices == nil {
		return ctl.Response{Error: errNoNotices.Error()}
	}
	title, body := strings.TrimSpace(r.Name), strings.TrimSpace(r.Text)
	if title == "" && body == "" {
		return ctl.Response{Error: "a notification needs a title or a body"}
	}
	if r.Target != "" {
		if _, ok := c.pty.TabForPane(r.Target); !ok {
			return ctl.Response{Error: "no such pane " + r.Target}
		}
	}
	if c.Root != nil && c.pty.IsRootToken(r.Token) {
		c.Root.Notify(title, body, r.Target)
		return ctl.Response{OK: true}
	}
	from := c.caller(r)
	if title == "" {
		title = "Claude Code"
	}
	n := Notice{Kind: "agent", Title: title, Body: body, Pane: targetOf(r, from), From: from}
	if from != "" {
		// One standing notice per session: its newer word replaces the older.
		n.Key = "agent:" + from
	}
	c.Notices.post(n)
	return ctl.Response{OK: true}
}

// noticeReader is what a caller may see of the bell: everything for the root agent and for
// the user's own shell outside any pane, else the notices about its pane or from it.
func (c *CtlService) noticeReader(r ctl.Request) func(Notice) bool {
	from := c.caller(r)
	if from == "" || c.pty.IsRootToken(r.Token) {
		return func(Notice) bool { return true }
	}
	return func(n Notice) bool { return n.Pane == from || n.From == from }
}

// notifications lists the bell: the unread notices, or all of them with "all".
func (c *CtlService) notifications(r ctl.Request) ctl.Response {
	if c.Notices == nil {
		return ctl.Response{Error: errNoNotices.Error()}
	}
	may := c.noticeReader(r)
	all := strings.TrimSpace(r.Name) == "all"
	return ctl.Response{OK: true, Data: c.Notices.listWhere(func(n Notice) bool {
		return may(n) && (all || !n.Read)
	})}
}

// notificationsRead marks notices as read: "all" (of those the caller may see), or by id.
func (c *CtlService) notificationsRead(r ctl.Request) ctl.Response {
	if c.Notices == nil {
		return ctl.Response{Error: errNoNotices.Error()}
	}
	words := strings.Fields(r.Text)
	if len(words) == 0 {
		return ctl.Response{Error: "say which: all, or the ids"}
	}
	may := c.noticeReader(r)
	ids := map[string]bool{}
	for _, w := range words {
		ids[w] = true
	}
	n := c.Notices.mark(func(n Notice) bool { return may(n) && (ids["all"] || ids[n.ID]) })
	return ctl.Response{OK: true, Data: map[string]int{"marked": n}}
}
