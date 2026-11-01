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

// Prompt is what the root agent's session starts with.
func Prompt(resume bool) string {
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
func Args(cfg config.Config, sessionID string, resume bool, instructions, watch, mcpConfig string) []string {
	cmd := cfg.Claude.Command
	if cmd == "" {
		cmd = "claude"
	}
	args := strings.Fields(cmd)
	args = append(args, Prompt(resume))
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
		"mcp__wate__wate_sessions", "mcp__wate__wate_session_open", "mcp__wate__wate_notify",
		"Bash("+watch+")", "Read(~/.claude/projects/**)")
	args = append(args, "--mcp-config", mcpConfig)
	return args
}
