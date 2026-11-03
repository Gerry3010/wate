package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Gerry3010/wate/internal/agent"
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

// remoteControlOps is what switching Remote Control on needs from the rest of wate, kept apart
// so the decisions can be tested without a Claude Code to talk to.
type remoteControlOps struct {
	// pid is the Claude Code process in a pane, 0 for none.
	pid   func(pane string) int
	state func(pid int) (agent.ProcState, error)
	// input says what the session's input box holds: "empty", "typed" or "none" (see the
	// frontend's claudeInput).
	input func(pane string) (string, error)
	write func(pane, text string) error
	sleep func(time.Duration)
}

// remoteControl switches Remote Control on in the Claude Code session in pane, the way the
// sidebar's menu does: by typing /remote-control at its prompt. Typing into somebody's
// session is only harmless when nothing else can catch the keys, so it happens only while
// the session waits at its prompt (Claude Code's own word for it) and the input box is empty;
// otherwise the command would end up in the middle of what the user is writing, or answer a
// dialog. Once it is typed, the wait for the connection tells the root agent how it went.
func remoteControl(ops remoteControlOps, pane, name string) (RemoteControl, error) {
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
	switch {
	case st.AtPrompt():
	case st.Status == "busy":
		return out, fmt.Errorf("busy: the session in pane %s is in the middle of a turn; try again when it is done", pane)
	case st.Status == "waiting":
		return out, fmt.Errorf("waiting: the session in pane %s shows a %s; try again once the user has answered it", pane, firstNonEmpty(st.WaitingFor, "dialog"))
	default:
		return out, fmt.Errorf("Claude Code in pane %s does not say it is at its prompt (status %q); the user can switch Remote Control on from the sidebar", pane, st.Status)
	}
	in, err := ops.input(pane)
	if err != nil {
		return out, err
	}
	switch in {
	case "empty":
	case "typed":
		return out, fmt.Errorf("input not empty: there is text on the prompt in pane %s, the user may be typing; ask them, or try again later", pane)
	default:
		return out, fmt.Errorf("no empty prompt on screen in pane %s (a dialog, or Claude Code's bash mode?)", pane)
	}

	line := "/remote-control"
	if name != "" {
		line += " " + name
	}
	if err := ops.write(pane, line); err != nil {
		return out, err
	}
	ops.sleep(submitDelay)
	if err := ops.write(pane, "\r"); err != nil {
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

// remoteControlOps is the real thing: the pane's Claude Code process, its state file, and the
// window that draws its screen.
func (c *CtlService) remoteControlOps() remoteControlOps {
	return remoteControlOps{
		pid: c.Root.agents.ClaudePid,
		state: func(pid int) (agent.ProcState, error) {
			return agent.ReadProcState(homeDir(), pid)
		},
		input: func(pane string) (string, error) {
			return c.askPane(pane, "claude-input", map[string]any{})
		},
		write: c.pty.WriteToPane,
		sleep: time.Sleep,
	}
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
	rc, err := remoteControl(c.remoteControlOps(), pane, name)
	if err != nil {
		return ctl.Response{Error: err.Error()}
	}
	return ctl.Response{OK: true, Data: rc}
}
