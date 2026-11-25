package app

import (
	"fmt"
	"time"

	"github.com/Gerry3010/wate/internal/agent"
)

// promptOps is what typing at a Claude Code session's prompt needs from the rest of wate, kept
// apart so the decisions can be tested without a Claude Code to talk to.
type promptOps struct {
	// pid is the Claude Code process in a pane, 0 for none.
	pid   func(pane string) int
	state func(pid int) (agent.ProcState, error)
	// input says what the session's input box holds: "empty", "typed" or "none" (see the
	// frontend's claudeInput).
	input func(pane string) (string, error)
	write func(pane, text string) error
	sleep func(time.Duration)
}

// promptIsFree says whether a line may be typed into the session in pane, whose state is st.
//
// Typing into somebody's session is only harmless when nothing else can catch the keys, so it
// happens only while the session waits at its prompt (Claude Code's own word for it) and the
// input box is empty. Otherwise the line would end up in the middle of what the user is
// writing, or answer a dialog.
func promptIsFree(ops promptOps, pane string, st agent.ProcState) error {
	switch {
	case st.AtPrompt():
	case st.Status == "busy":
		return fmt.Errorf("busy: the session in pane %s is in the middle of a turn; try again when it is done", pane)
	case st.Status == "waiting":
		return fmt.Errorf("waiting: the session in pane %s shows a %s; try again once the user has answered it", pane, firstNonEmpty(st.WaitingFor, "dialog"))
	default:
		return fmt.Errorf("Claude Code in pane %s does not say it is at its prompt (status %q)", pane, st.Status)
	}
	in, err := ops.input(pane)
	if err != nil {
		return err
	}
	switch in {
	case "empty":
		return nil
	case "typed":
		return fmt.Errorf("input not empty: there is text on the prompt in pane %s, the user may be typing; ask them, or try again later", pane)
	default:
		return fmt.Errorf("no empty prompt on screen in pane %s (a dialog, or Claude Code's bash mode?)", pane)
	}
}

// submitLine types line at the prompt and sends it. The Enter goes on its own, a moment later,
// or Claude Code takes it for part of a paste.
func submitLine(ops promptOps, pane, line string) error {
	if err := ops.write(pane, line); err != nil {
		return err
	}
	ops.sleep(submitDelay)
	return ops.write(pane, "\r")
}

// promptOps is the real thing: the pane's Claude Code process (found by pid), its state file,
// and the window that draws its screen.
func (c *CtlService) promptOps(pid func(pane string) int) promptOps {
	return promptOps{
		pid: pid,
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
