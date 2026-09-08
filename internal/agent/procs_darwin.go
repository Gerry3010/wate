package agent

import (
	"os/exec"
	"strconv"
	"strings"
)

// ClaudeRunningUnder reports whether a process named "claude" is a descendant of pid.
// macOS has no /proc; one `ps` call per poll is cheap enough at a 2 s interval.
func ClaudeRunningUnder(pid int) bool {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,comm=").Output()
	if err != nil {
		return false
	}
	kids := map[int][]int{}
	name := map[int]string{}
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
	isClaude := func(p int) bool { return strings.HasSuffix(name[p], "/claude") || name[p] == "claude" }
	if isClaude(pid) {
		return true
	}
	var walk func(int, int) bool
	walk = func(p, depth int) bool {
		if depth > 6 {
			return false
		}
		for _, c := range kids[p] {
			if isClaude(c) || walk(c, depth+1) {
				return true
			}
		}
		return false
	}
	return walk(pid, 0)
}
