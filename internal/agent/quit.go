package agent

import (
	"syscall"
	"time"
)

// StopProcesses asks the given processes to exit (SIGTERM) and waits until they are gone or
// the timeout expires; it returns how many were signalled and how many are still alive.
//
// Claude Code cleans up on SIGTERM (flushes its transcript, runs SessionEnd hooks); the SIGHUP
// of a closing PTY can cut it off mid-write, so wate stops the agents before the shells.
func StopProcesses(pids []int, timeout time.Duration) (signalled, left int) {
	alive := map[int]bool{}
	for _, pid := range pids {
		if pid <= 0 || alive[pid] {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err == nil {
			alive[pid] = true
		}
	}
	signalled = len(alive)
	deadline := time.Now().Add(timeout)
	for len(alive) > 0 {
		for pid := range alive {
			if !processAlive(pid) {
				delete(alive, pid)
			}
		}
		if len(alive) == 0 || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	return signalled, len(alive)
}
