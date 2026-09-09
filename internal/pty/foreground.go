package pty

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Foreground describes the process group the terminal currently sends input to: the
// command the user is waiting on, or nothing while the shell sits at its prompt.
type Foreground struct {
	Pid int
	// Name is the command's basename ("ssh", "vim"), empty for the shell itself.
	Name string
	Args []string
}

// Foreground asks the pty which process group owns the terminal (TIOCGPGRP) and
// describes it. The shell's own group means "at the prompt" and yields a zero Name.
func (s *Session) Foreground() (Foreground, bool) {
	conn, err := s.file.SyscallConn()
	if err != nil {
		return Foreground{}, false
	}
	var (
		pgrp int
		ierr error
	)
	// SyscallConn keeps the fd out of blocking mode, which File.Fd() would force.
	if err := conn.Control(func(fd uintptr) { pgrp, ierr = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP) }); err != nil || ierr != nil {
		return Foreground{}, false
	}
	if pgrp <= 0 {
		return Foreground{}, false
	}
	if pgrp == s.Pid() {
		return Foreground{Pid: pgrp}, true
	}
	name, args := processCmd(pgrp)
	if name == "" {
		return Foreground{Pid: pgrp}, true
	}
	return Foreground{Pid: pgrp, Name: name, Args: args}, true
}

// ssh flags that swallow the next argument, so it is not mistaken for the host.
const sshFlagsWithValue = "bcDEeFIiJLlmOopQRSWw"

// SSHTarget picks the host out of an ssh command line: "ssh -p 22 gerry@io-main ls"
// yields "io-main". It returns "" when the arguments name no host.
func SSHTarget(args []string) string {
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			if i+1 < len(args) {
				return sshHost(args[i+1])
			}
			return ""
		case strings.HasPrefix(a, "-") && len(a) > 1:
			// Short flags cluster ("-vvp 22"); only a trailing value flag takes the next argument.
			last := a[len(a)-1:]
			if strings.Contains(sshFlagsWithValue, last) && !strings.Contains(a, "=") {
				i++
			}
		case a != "":
			return sshHost(a)
		}
	}
	return ""
}

// sshHost strips user, scheme and port from an ssh destination.
func sshHost(dest string) string {
	dest = strings.TrimPrefix(dest, "ssh://")
	if i := strings.LastIndex(dest, "@"); i >= 0 {
		dest = dest[i+1:]
	}
	if strings.HasPrefix(dest, "[") { // [2001:db8::1]:22
		if i := strings.Index(dest, "]"); i > 0 {
			return dest[1:i]
		}
	}
	if i := strings.Index(dest, ":"); i > 0 && !strings.Contains(dest[i+1:], ":") {
		dest = dest[:i]
	}
	if i := strings.Index(dest, "/"); i > 0 {
		dest = dest[:i]
	}
	return dest
}

func baseName(cmd string) string {
	if cmd == "" {
		return ""
	}
	return filepath.Base(cmd)
}
