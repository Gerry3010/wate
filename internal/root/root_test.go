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
	got := Args(cfg, "11111111-2222-3333-4444-555555555555", false, "INSTR", "/bin/wate ctl watch --once", "MCP")
	want := []string{
		"claude", "--verbose", Prompt(false),
		"--session-id", "11111111-2222-3333-4444-555555555555",
		"--name", "root", "--model", "opus",
		"--append-system-prompt", "INSTR",
		"--allowedTools", "mcp__wate__wate_sessions", "mcp__wate__wate_tabs", "mcp__wate__wate_session_open",
		"mcp__wate__wate_session_focus", "mcp__wate__wate_session_move", "mcp__wate__wate_notify",
		"Bash(/bin/wate ctl watch --once)", "Read(~/.claude/projects/**)",
		"--mcp-config", "MCP",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	cfg.Root.Model = ""
	again := Args(cfg, "abc", true, "I", "w", "M")
	if !slices.Contains(again, "--resume") || slices.Contains(again, "--session-id") || slices.Contains(again, "--model") {
		t.Fatalf("resume without a model: %q", again)
	}
	if again[2] != Prompt(true) {
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
