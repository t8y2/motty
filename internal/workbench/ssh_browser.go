package workbench

import (
	"errors"

	"motty/internal/sshbrowser"
	"motty/internal/sshconn"
)

func (session *Session) canOpenSSHBrowser() bool {
	return session.hasServer() && !session.disconnected && session.conn != nil && session.phase == sshconn.PhaseConnected
}

func (state *State) openSSHBrowser(session *Session) {
	if !session.canOpenSSHBrowser() || session.browser != nil && session.browser.Running() {
		return
	}
	m := state.messages
	browser, err := sshbrowser.Start(session.conn.DialContext)
	switch {
	case errors.Is(err, sshbrowser.ErrBrowserNotFound):
		state.showActionError(m.MenuOpenSSHBrowser, m.SSHBrowserNotFound)
	case err != nil:
		state.showActionFailure(m.MenuOpenSSHBrowser, err, m.SSHBrowserStartFailed)
	default:
		session.browser = browser
	}
}

func (session *Session) closeSSHBrowser() {
	if session.browser != nil {
		session.browser.Close()
		session.browser = nil
	}
}
