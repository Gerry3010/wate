// Package root builds the root agent's command line and instructions.
package root

import (
	_ "embed"
	"strings"

	"github.com/Gerry3010/wate/internal/config"
)

//go:embed instructions.md
var builtIn string

// Instructions are the root agent's instructions: custom (when given) or the built-in ones,
// with {{WATCH}} replaced by the command that starts its watcher.
func Instructions(custom, watch string) string {
	text := builtIn
	if strings.TrimSpace(custom) != "" {
		text = custom
	}
	return strings.ReplaceAll(text, "{{WATCH}}", watch)
}

// Restart describes the restart the root agent comes back from, when it has just happened.
type Restart struct {
	// ByRoot is set when the root agent asked for it; Reason is what was given.
	ByRoot bool
	Reason string
}

// Prompt is what the root agent's session starts with. Coming back from a restart, it is told
// so and where to find what was running before, so it can check that everything came back.
func Prompt(resume bool, restart *Restart) string {
	if restart != nil {
		who := "the user asked for it"
		if restart.ByRoot {
			who = "you asked for it"
		}
		if restart.Reason != "" {
			who += " (" + restart.Reason + ")"
		}
		return "wate has restarted; " + who + ". Before it went down it wrote down what was running: " +
			"read it with wate_restart_manifest and compare it with wate_sessions and wate_tabs. Bring " +
			"back the sessions that are missing with wate_session_resume (with no session id it resumes " +
			"every one that is offered), put lost tab colours back with wate_tab_color, switch Remote " +
			"Control on again where it was on, start your attention watcher, then tell the user in a few " +
			"lines what is back and what is not."
	}
	if resume {
		return "wate has started you again. Start your attention watcher, then tell the user in a " +
			"few lines what has changed since you last looked, if anything."
	}
	return "wate has started you as its root agent. Start your attention watcher, then give the " +
		"user a short overview of the sessions that are running."
}

// Args is the root agent's command line; watch is the command that starts its watcher and
// mcpConfig the --mcp-config value with its tools. sessionID is continued when resume is set and
// claimed for a fresh session otherwise, so wate can pick it up again after a restart without
// depending on any hook having reported it.
//
// The prompt goes straight after the command: --allowedTools and --mcp-config take any number
// of values and would swallow a prompt standing behind them.
func Args(cfg config.Config, sessionID string, resume bool, restart *Restart, instructions, watch, mcpConfig string) []string {
	cmd := cfg.Claude.Command
	if cmd == "" {
		cmd = "claude"
	}
	args := strings.Fields(cmd)
	args = append(args, Prompt(resume, restart))
	if resume {
		args = append(args, "--resume", sessionID)
	} else {
		args = append(args, "--session-id", sessionID)
	}
	args = append(args, "--name", "root")
	if cfg.Root.Model != "" {
		args = append(args, "--model", cfg.Root.Model)
	}
	args = append(args, "--append-system-prompt", instructions)
	// Its own tools and its watcher run without asking: a root agent stuck on its own
	// permission prompts would watch nothing. Everything else asks as usual.
	args = append(args, "--allowedTools",
		"mcp__wate__wate_sessions", "mcp__wate__wate_tabs", "mcp__wate__wate_session_open",
		"mcp__wate__wate_session_focus", "mcp__wate__wate_session_move", "mcp__wate__wate_notify",
		"mcp__wate__wate_session_remote_control", "mcp__wate__wate_tab_color",
		"mcp__wate__wate_notifications", "mcp__wate__wate_notifications_read",
		"mcp__wate__wate_restart", "mcp__wate__wate_restart_status", "mcp__wate__wate_restart_cancel",
		"mcp__wate__wate_restart_manifest", "mcp__wate__wate_session_resume",
		"Bash("+watch+")", "Read(~/.claude/projects/**)")
	args = append(args, "--mcp-config", mcpConfig)
	return args
}
