package pty

import (
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// ttyName is the device of the terminal side of the PTY whose master is f, asked of the master
// itself. Through SyscallConn, because File.Fd would switch the master to blocking mode and
// leave the output pump unable to stop.
func ttyName(f *os.File, _ int) string {
	rc, err := f.SyscallConn()
	if err != nil {
		return ""
	}
	var n uint32
	var ierr error
	if err := rc.Control(func(fd uintptr) { n, ierr = unix.IoctlGetUint32(int(fd), unix.TIOCGPTN) }); err != nil || ierr != nil {
		return ""
	}
	return "/dev/pts/" + strconv.FormatUint(uint64(n), 10)
}
