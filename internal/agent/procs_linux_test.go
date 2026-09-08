package agent

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestClaudeRunningUnder(t *testing.T) {
	if ClaudeRunningUnder(os.Getpid()) {
		t.Fatal("test process has no claude child")
	}
	// A child process whose comm is "claude": copy /bin/sleep under that name.
	dir := t.TempDir()
	bin := dir + "/claude"
	src, _ := os.ReadFile("/bin/sleep")
	os.WriteFile(bin, src, 0o755)
	cmd := exec.Command("/bin/sh", "-c", bin+" 5")
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start helper:", err)
	}
	defer cmd.Process.Kill()
	deadline := time.Now().Add(2 * time.Second)
	for !ClaudeRunningUnder(cmd.Process.Pid) {
		if time.Now().After(deadline) {
			t.Fatal("claude grandchild not detected")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
