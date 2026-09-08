package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ClaudeRunningUnder reports whether pid itself or a descendant is Claude Code
// (the launcher runs claude directly as the pane process, a user types it into a shell).
func ClaudeRunningUnder(pid int) bool {
	return isClaude(pid) || walk(pid, 0)
}

func walk(pid, depth int) bool {
	if depth > 6 {
		return false
	}
	for _, child := range children(pid) {
		if isClaude(child) || walk(child, depth+1) {
			return true
		}
	}
	return false
}

func children(pid int) []int {
	matches, _ := filepath.Glob("/proc/" + strconv.Itoa(pid) + "/task/*/children")
	var out []int
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		for _, f := range strings.Fields(string(b)) {
			if n, err := strconv.Atoi(f); err == nil {
				out = append(out, n)
			}
		}
	}
	return out
}

func isClaude(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil {
		return false
	}
	name := strings.TrimSpace(string(b))
	if name == "claude" {
		return true
	}
	// The npm-installed CLI runs as "node …/claude"; check the command line too.
	if name == "node" || name == "bun" {
		cmd, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
		if err == nil {
			for _, arg := range strings.Split(string(cmd), "\x00") {
				if filepath.Base(arg) == "claude" || strings.HasSuffix(arg, "/claude/cli.js") {
					return true
				}
			}
		}
	}
	return false
}
