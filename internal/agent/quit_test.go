package agent

import (
	"os/exec"
	"testing"
	"time"
)

func TestStopProcesses(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exec sleep 30")
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start helper:", err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	reaped := make(chan struct{})
	go func() { _ = cmd.Wait(); close(reaped) }()
	start := time.Now()
	signalled, left := StopProcesses([]int{cmd.Process.Pid}, 2*time.Second)
	if signalled != 1 {
		t.Fatalf("signalled = %d, want 1", signalled)
	}
	if left != 0 {
		t.Fatalf("process still alive after SIGTERM")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("waited %v for a process that exits at once", time.Since(start))
	}
	select {
	case <-reaped:
	case <-time.After(time.Second):
		t.Fatal("child never exited")
	}
}

func TestStopProcessesIgnoresUnknown(t *testing.T) {
	if signalled, left := StopProcesses([]int{0, -1}, 50*time.Millisecond); signalled != 0 || left != 0 {
		t.Fatalf("got %d/%d, want 0/0", signalled, left)
	}
}
