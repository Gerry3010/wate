package pty

import (
	"os/exec"
	"strconv"
	"strings"
)

// processCmd reads a process's name and argument vector via ps (macOS has no /proc).
func processCmd(pid int) (string, []string) {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "args=").Output()
	if err != nil {
		return "", nil
	}
	args := strings.Fields(strings.TrimSpace(string(out)))
	if len(args) == 0 {
		return "", nil
	}
	return baseName(args[0]), args
}
