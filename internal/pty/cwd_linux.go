package pty

import (
	"fmt"
	"os"
)

func processCwd(pid int) (string, error) {
	return os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
}
