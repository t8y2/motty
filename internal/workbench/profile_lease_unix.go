//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package workbench

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockProfileFile(file *os.File, exclusive bool) error {
	mode := unix.LOCK_SH
	if exclusive {
		mode = unix.LOCK_EX
	}
	return unix.Flock(int(file.Fd()), mode|unix.LOCK_NB)
}
