package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Gerry3010/wate/internal/ctl"
)

const (
	// remoteControlPoll and remoteControlPolls bound the wait for Remote Control to connect
	// after /remote-control: it takes about a second.
	remoteControlPoll  = 250 * time.Millisecond
	remoteControlPolls = 24
)

// RemoteControl is what the root agent hears back from switching Remote Control on.
type RemoteControl struct {
	Pane string `json:"pane"`
	On   bool   `json:"on"`
	URL  string `json:"url,omitempty"`
	// Already is set when it was on before and nothing was typed.
	Already bool   `json:"already,omitempty"`
	Note    string `json:"note,omitempty"`
}

// remoteControl switches Remote Control on in the Claude Code session in pane, the way the
// sidebar's menu does: by typing /remote-control at its prompt, and only where promptIsFree
// allows it. Once it is typed, the wait for the connection tells the root agent how it went.
func remoteControl(ops promptOps, pane, name string) (RemoteControl, error) {
	out := RemoteControl{Pane: pane}
	pid := ops.pid(pane)
	if pid == 0 {
		return out, fmt.Errorf("no Claude Code session runs in pane %s", pane)
	}
	st, err := ops.state(pid)
	if err != nil {
		return out, fmt.Errorf("Claude Code in pane %s does not say whether it is at its prompt (%v); the user can switch Remote Control on from the sidebar", pane, err)
	}
	if url := st.RemoteControlURL(); url != "" {
		out.On, out.URL, out.Already = true, url, true
		return out, nil
	}
	if err := promptIsFree(ops, pane, st); err != nil {
		return out, err
	}

	line := "/remote-control"
	if name != "" {
		line += " " + name
	}
	if err := submitLine(ops, pane, line); err != nil {
		return out, err
	}

	for range remoteControlPolls {
		ops.sleep(remoteControlPoll)
		st, err := ops.state(pid)
		if err != nil {
			continue
		}
		if url := st.RemoteControlURL(); url != "" {
			out.On, out.URL = true, url
			return out, nil
		}
		if st.Status == "waiting" {
			out.Note = fmt.Sprintf("/remote-control was sent and the session now shows a %s; the user has to answer it", firstNonEmpty(st.WaitingFor, "dialog"))
			return out, nil
		}
	}
	out.Note = "/remote-control was sent, but Remote Control has not connected yet; see wate_sessions in a moment"
	return out, nil
}

// remoteControlName checks a Remote Control name: one line, and nothing Claude Code's command
// line would take for an option.
func remoteControlName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		return "", errors.New("a Remote Control name is one line of text")
	case strings.HasPrefix(name, "-"):
		return "", errors.New("a Remote Control name must not start with '-'")
	}
	return name, nil
}

// switchRemoteControl is the root agent's session-remote-control.
func (c *CtlService) switchRemoteControl(pane, name string) ctl.Response {
	name, err := remoteControlName(name)
	if err != nil {
		return ctl.Response{Error: err.Error()}
	}
	switch _, ok := c.pty.TabForPane(pane); {
	case pane == "":
		return ctl.Response{Error: "session-remote-control needs a pane"}
	case pane == c.pty.RootPane():
		return ctl.Response{Error: "that is your own session, and nothing can be typed into it while you run; " +
			"the user can enter /remote-control in your panel"}
	case !ok:
		return ctl.Response{Error: "no such pane: " + pane}
	}
	rc, err := remoteControl(c.promptOps(c.Root.agents.ClaudePid), pane, name)
	if err != nil {
		return ctl.Response{Error: err.Error()}
	}
	return ctl.Response{OK: true, Data: rc}
}
