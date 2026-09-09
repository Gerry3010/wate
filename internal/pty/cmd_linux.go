package pty

import (
	"os"
	"strconv"
	"strings"
)

// processCmd reads a process's name and argument vector from /proc.
func processCmd(pid int) (string, []string) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return "", nil
	}
	args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	if len(args) == 0 || args[0] == "" {
		// Kernel threads and processes that cleared their argv have no cmdline.
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
		if err != nil {
			return "", nil
		}
		return strings.TrimSpace(string(b)), nil
	}
	return baseName(args[0]), args
}
