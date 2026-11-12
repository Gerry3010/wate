package pty

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ttyName is the device of the terminal side of the PTY, read off the process running in it:
// macOS has neither /proc nor an ioctl in x/sys for the name.
func ttyName(_ *os.File, pid int) string {
	out, err := exec.Command("ps", "-o", "tty=", "-p", strconv.Itoa(pid)).Output()
	name := strings.TrimSpace(string(out))
	if err != nil || name == "" || strings.HasPrefix(name, "?") {
		return ""
	}
	return "/dev/" + name
}
