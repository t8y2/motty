//go:build !windows

package rdp

// The system-chord capture is a Windows low-level keyboard hook. On the other
// platforms the host has no chord to take, or the window system already gives
// the window every key, so there is nothing to install: the workbench asks
// through desktop.SystemKeyCapture and these stay empty.

func (session *Session) setSystemKeyCaptureLocked(bool) {}

func (session *Session) stopSystemKeyCapture() {}

// keyCapture names the Windows hook state for the Session struct; on the other
// platforms a Session never has one.
type keyCapture = struct{}
