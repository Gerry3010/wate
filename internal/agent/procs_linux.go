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

// ClaudePidsUnder returns the pids of the Claude Code processes in pid's tree (pid included),
// so they can be asked to exit before the pane's shell goes away.
func ClaudePidsUnder(pid int) []int {
	var out []int
	collect(pid, 0, &out)
	return out
}

func collect(pid, depth int, out *[]int) {
	if isClaude(pid) {
		*out = append(*out, pid)
	}
	if depth > 6 {
		return
	}
	for _, child := range children(pid) {
		collect(child, depth+1, out)
	}
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
	// The main thread's children file covers everything a shell spawns; only a process that
	// forks from another thread (node does not, for the pane's purposes) needs the full walk,
	// and enumerating a node process's threads every two seconds is the expensive part.
	dir := "/proc/" + strconv.Itoa(pid) + "/task/"
	out := readChildren(dir+strconv.Itoa(pid)+"/children", nil)
	if len(out) > 0 {
		return out
	}
	tasks, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, t := range tasks {
		if t.Name() == strconv.Itoa(pid) {
			continue
		}
		out = readChildren(dir+t.Name()+"/children", out)
	}
	return out
}

func readChildren(path string, out []int) []int {
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, f := range strings.Fields(string(b)) {
		if n, err := strconv.Atoi(f); err == nil {
			out = append(out, n)
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

// IsLiveClaude reports whether a pid seen earlier is still the running Claude Code process,
// so the poller can skip walking the pane's process tree while nothing changed.
func IsLiveClaude(pid int) bool { return pid > 0 && processAlive(pid) && isClaude(pid) }

// processAlive reports whether the pid is still running; a zombie waiting to be reaped by its
// shell counts as gone (it can still be signalled, but it is not doing anything any more).
func processAlive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// "pid (comm) state …" — comm may contain spaces and brackets, so scan from the right.
	i := strings.LastIndex(string(b), ") ")
	if i < 0 || len(b) < i+3 {
		return false
	}
	return b[i+2] != 'Z'
}
