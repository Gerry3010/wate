package app

import (
	"slices"
	"testing"
)

func TestClaudeSessionVarsDropsTheLaunchingSession(t *testing.T) {
	env := []string{
		"HOME=/Users/gerry",
		"CLAUDECODE=1",
		"CLAUDE_CODE_SESSION_ID=c09e8934",
		"CLAUDE_CODE_CHILD_SESSION=1",
		"CLAUDE_PID=4711",
		"CLAUDE_CODE_MESSAGING_SOCKET=/tmp/claude.sock",
		"PATH=/usr/bin",
	}
	got := claudeSessionVars(env)
	want := []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_PID", "CLAUDE_CODE_MESSAGING_SOCKET"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestClaudeSessionVarsLeavesSettingsAlone(t *testing.T) {
	// Without CLAUDECODE nobody launched wate from a session: these were set on purpose.
	env := []string{"CLAUDE_CONFIG_DIR=/Users/gerry/.claude-work", "CLAUDE_CODE_USE_BEDROCK=1", "PATH=/usr/bin"}
	if got := claudeSessionVars(env); got != nil {
		t.Fatalf("got %v, want nothing hidden", got)
	}
}
