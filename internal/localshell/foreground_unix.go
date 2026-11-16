//go:build darwin || linux

package localshell

import (
	"os"

	"golang.org/x/sys/unix"
)

// foregroundArgs reads the foreground process group of a pseudo-terminal. The
// group's leader is the program the user is looking at: the shell at a prompt,
// or the command it is waiting on. The shell's own pid stays the shell even
// after it has started something else.
//
// The descriptor is borrowed through SyscallConn. File.Fd would put the master
// back into blocking mode, and then closing it could no longer wake the reader.
// Control also fails once the file is closed, so a reused descriptor number is
// never asked.
func foregroundArgs(pty *os.File) ([]string, bool) {
	raw, err := pty.SyscallConn()
	if err != nil {
		return nil, false
	}
	pgid := 0
	var ioctlErr error
	if err := raw.Control(func(fd uintptr) {
		pgid, ioctlErr = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP)
	}); err != nil || ioctlErr != nil || pgid <= 0 {
		return nil, false
	}
	return processArgs(pgid)
}
