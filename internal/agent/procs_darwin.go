package agent

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// procTree is the process table as parent → children plus a name per pid.
// macOS has no /proc; one `ps` call per poll is cheap enough at a 2 s interval.
func procTree() (kids map[int][]int, name map[int]string) {
	kids, name = map[int][]int{}, map[int]string{}
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,comm=").Output()
	if err != nil {
		return kids, name
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		p, _ := strconv.Atoi(f[0])
		pp, _ := strconv.Atoi(f[1])
		kids[pp] = append(kids[pp], p)
		name[p] = f[2]
	}
	return kids, name
}

// ClaudeRunningUnder reports whether a process named "claude" is pid or a descendant of it.
func ClaudeRunningUnder(pid int) bool {
	return len(ClaudePidsUnder(pid)) > 0
}

// ClaudePidsUnder returns the pids of the Claude Code processes in pid's tree (pid included),
// so they can be asked to exit before the pane's shell goes away.
func ClaudePidsUnder(pid int) []int {
	kids, name := procTree()
	isClaude := func(p int) bool { return strings.HasSuffix(name[p], "/claude") || name[p] == "claude" }
	var out []int
	var collect func(int, int)
	collect = func(p, depth int) {
		if isClaude(p) {
			out = append(out, p)
		}
		if depth > 6 {
			return
		}
		for _, c := range kids[p] {
			collect(c, depth+1)
		}
	}
	collect(pid, 0)
	return out
}

// IsLiveClaude reports whether a pid seen earlier is still the running Claude Code process,
// so the poller can skip walking the pane's process tree while nothing changed.
func IsLiveClaude(pid int) bool {
	if pid <= 0 || !processAlive(pid) {
		return false
	}
	_, name := procTree()
	return strings.HasSuffix(name[pid], "/claude") || name[pid] == "claude"
}

// processAlive reports whether the pid can still be signalled (its shell reaps the zombie).
func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }
