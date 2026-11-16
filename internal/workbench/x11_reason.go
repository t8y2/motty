package workbench

import (
	"errors"
	"fmt"

	"motty/internal/sshconn"
	"motty/internal/x11fwd"
)

// x11Reason says why X11 forwarding is off in the user's language, with what
// to install or start on this system when the cause is a missing X server.
func (state *State) x11Reason(err error, goos string) string {
	tools := state.messages.Tools
	hint := tools.X11HintOther
	switch goos {
	case "darwin":
		hint = tools.X11HintMac
	case "windows":
		hint = tools.X11HintWindows
	}
	var unreachable *x11fwd.UnreachableError
	switch {
	case errors.Is(err, x11fwd.ErrNoDisplay):
		return fmt.Sprintf(tools.X11NoServerFmt, hint)
	case errors.As(err, &unreachable):
		return fmt.Sprintf(tools.X11UnreachableFmt, unreachable.Address, hint)
	case errors.Is(err, sshconn.ErrX11Refused):
		return tools.X11Refused
	}
	return err.Error()
}
