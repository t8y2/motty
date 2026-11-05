//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"sync"

	winsys "golang.org/x/sys/windows"
)

// watchParent ends the MCP server when the process that started it is gone. On
// Windows the pipes a client hands to its child are frequently inherited by
// other processes, so stdin never reaches EOF when the client exits, and the
// server would stay alive holding motty.exe open, which makes the next update
// fail. Nothing here polls: the watch waits on the parent's process handle,
// which Windows signals when the process ends.
func watchParent(cancel context.CancelFunc) (stop func()) {
	return watchOlderProcess(os.Getppid(), cancel)
}

// watchOlderProcess is watchProcess for a process that must have been created
// before this one. Windows reuses process ids, so the process now behind pid
// need not be the parent that was asked for; the parent is always older than
// the server it started, so a younger process means the parent is gone.
func watchOlderProcess(pid int, cancel context.CancelFunc) (stop func()) {
	handle, ok := openWatched(pid, cancel)
	if !ok {
		return func() {}
	}
	if createdAfterUs(handle) {
		winsys.CloseHandle(handle)
		cancel()
		return func() {}
	}
	return waitForProcess(handle, cancel)
}

// watchProcess cancels when the process pid ends, and returns stop to end the
// watch without cancelling. A process that is already gone cancels at once; one
// that cannot be watched at all, because opening it is denied, leaves the
// server to stdin reaching EOF and to the signals.
func watchProcess(pid int, cancel context.CancelFunc) (stop func()) {
	handle, ok := openWatched(pid, cancel)
	if !ok {
		return func() {}
	}
	return waitForProcess(handle, cancel)
}

// openWatched opens pid for waiting. It reports false when there is nothing to
// wait on, having cancelled when the reason is that the process does not exist.
func openWatched(pid int, cancel context.CancelFunc) (winsys.Handle, bool) {
	handle, err := winsys.OpenProcess(winsys.SYNCHRONIZE|winsys.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, winsys.ERROR_INVALID_PARAMETER) {
			cancel() // no such process
		}
		return 0, false
	}
	return handle, true
}

// waitForProcess cancels when the process behind handle ends and owns handle
// from here on. A manual-reset event lets stop wake the waiting goroutine, so
// it never stays blocked in a wait nobody needs any more.
func waitForProcess(handle winsys.Handle, cancel context.CancelFunc) (stop func()) {
	quit, err := winsys.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		winsys.CloseHandle(handle)
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		// A process that ended before this wait began has its handle signalled
		// already, so the wait returns at once.
		if event, err := winsys.WaitForMultipleObjects([]winsys.Handle{quit, handle}, false, winsys.INFINITE); err == nil && event == winsys.WAIT_OBJECT_0+1 {
			cancel()
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = winsys.SetEvent(quit)
			<-done
			winsys.CloseHandle(quit)
			winsys.CloseHandle(handle)
		})
	}
}

// createdAfterUs reports whether the process behind handle started after this
// process, which means it cannot be the parent that started it.
func createdAfterUs(handle winsys.Handle) bool {
	other, ok := createdAt(handle)
	if !ok {
		return false
	}
	self, ok := createdAt(winsys.CurrentProcess())
	if !ok {
		return false
	}
	return other > self
}

// createdAt returns when the process behind handle was created, in the
// 100-nanosecond ticks Windows keeps creation times in.
func createdAt(handle winsys.Handle) (uint64, bool) {
	var created, exited, kernel, user winsys.Filetime
	if err := winsys.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return 0, false
	}
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime), true
}
