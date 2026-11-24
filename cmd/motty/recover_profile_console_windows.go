package main

import (
	"os"

	winapi "golang.org/x/sys/windows"
	"golang.org/x/term"
)

// Release builds use the Windows GUI subsystem. Attach the invoking terminal
// before asking for a password when the process did not inherit console handles.
func attachRecoveryConsole() {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return
	}
	const attachParentProcess = 0xffffffff
	attach := winapi.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	_, _, _ = attach.Call(attachParentProcess)
	if input, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = input
	}
	if output, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stderr = output
	}
}
