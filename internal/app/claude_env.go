package app

import (
	"os"
	"strings"

	"github.com/Gerry3010/wate/internal/pty"
)

// ForgetLaunchingClaude keeps a Claude Code session that started wate out of wate's panes.
//
// Started from inside one — a shell a Claude Code session opened, which is how an agent
// restarts wate — wate inherits that session's markers: CLAUDECODE, its session id, its pid,
// its messaging socket. Every shell in every pane would pass them on, and every Claude Code
// started there would take itself for a child of a session that is long gone (and, for one,
// stop saving its transcripts).
//
// CLAUDECODE is what says the environment came out of a session, so without it nothing is
// touched: a CLAUDE_* variable set in a plain environment is a setting somebody chose.
func ForgetLaunchingClaude() {
	pty.HideFromShells(claudeSessionVars(os.Environ())...)
}

// claudeSessionVars names the variables of env that belong to the Claude Code session it
// came out of, if it came out of one.
func claudeSessionVars(env []string) []string {
	var names []string
	inSession := false
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if name == "CLAUDECODE" {
			inSession = true
		}
		if name == "CLAUDECODE" || strings.HasPrefix(name, "CLAUDE_") {
			names = append(names, name)
		}
	}
	if !inSession {
		return nil
	}
	return names
}
