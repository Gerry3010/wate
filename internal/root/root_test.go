package root

import (
	"slices"
	"strings"
	"testing"

	"github.com/Gerry3010/wate/internal/config"
)

func TestArgs(t *testing.T) {
	cfg := config.Defaults()
	cfg.Claude.Command = "claude --verbose"
	got := Args(cfg, "11111111-2222-3333-4444-555555555555", false, nil, "INSTR", "/bin/wate ctl watch --once", "MCP")
	want := []string{
		"claude", "--verbose", Prompt(false, nil),
		"--session-id", "11111111-2222-3333-4444-555555555555",
		"--name", "root", "--model", "opus",
		"--append-system-prompt", "INSTR",
		"--allowedTools", "mcp__wate__wate_sessions", "mcp__wate__wate_tabs", "mcp__wate__wate_session_open",
		"mcp__wate__wate_session_focus", "mcp__wate__wate_session_move", "mcp__wate__wate_notify",
		"mcp__wate__wate_session_remote_control", "mcp__wate__wate_tab_color",
		"mcp__wate__wate_notifications", "mcp__wate__wate_notifications_read",
		"mcp__wate__wate_restart", "mcp__wate__wate_restart_status", "mcp__wate__wate_restart_cancel",
		"mcp__wate__wate_restart_manifest", "mcp__wate__wate_session_resume",
		"Bash(/bin/wate ctl watch --once)", "Read(~/.claude/projects/**)",
		"--mcp-config", "MCP",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	cfg.Root.Model = ""
	again := Args(cfg, "abc", true, nil, "I", "w", "M")
	if !slices.Contains(again, "--resume") || slices.Contains(again, "--session-id") || slices.Contains(again, "--model") {
		t.Fatalf("resume without a model: %q", again)
	}
	if again[2] != Prompt(true, nil) {
		t.Fatalf("the prompt must come right after the command: %q", again)
	}
}

func TestInstructions(t *testing.T) {
	if got := Instructions("", "W"); !strings.Contains(got, "    W\n") || strings.Contains(got, "{{WATCH}}") {
		t.Fatal("the watch command is not in the built-in instructions")
	}
	if got := Instructions("mine: {{WATCH}}", "W"); got != "mine: W" {
		t.Fatalf("custom instructions: %q", got)
	}
}

func TestComingBackFromARestartTheRootAgentIsToldWhoAskedAndWhatToCheck(t *testing.T) {
	mine := Prompt(true, &Restart{ByRoot: true, Reason: "new build"})
	theirs := Prompt(true, &Restart{})
	for _, want := range []string{"you asked for it (new build)", "wate_restart_manifest", "wate_session_resume", "wate_tab_color", "watcher"} {
		if !strings.Contains(mine, want) {
			t.Errorf("the prompt after its own restart lacks %q:\n%s", want, mine)
		}
	}
	if !strings.Contains(theirs, "the user asked for it") {
		t.Errorf("after the user's restart: %s", theirs)
	}
}
