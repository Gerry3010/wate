package pty

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// processCwd asks lsof for the cwd file descriptor; macOS has no /proc.
func processCwd(pid int) (string, error) {
	out, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			return line[1:], nil
		}
	}
	return "", errors.New("cwd not found")
}
