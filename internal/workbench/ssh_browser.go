package workbench

import (
	"errors"
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshbrowser"
	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
)

// startSSHBrowser launches the isolated browser the tab menu opens. Tests
// replace it to observe the chosen family without a real browser.
var startSSHBrowser = sshbrowser.Start

// sshBrowserChoices is every family the setting offers, in the order it lists them.
var sshBrowserChoices = sshbrowser.Families[:]

// sshBrowserIcons is what the setting paints before each choice, indexed like
// sshBrowserChoices.
var sshBrowserIcons = []gfx.Icon{icons.Globe, icons.BrowserChrome, icons.BrowserEdge, icons.BrowserChromium}

// paintSSHBrowserSetting is the SSH browser row: the chosen family, with its
// mark and the mark of each option.
func (state *State) paintSSHBrowserSetting(page *settingsPage) {
	m := state.messages
	page.lineWith(m.SettingsSSHBrowser, m.SettingsSSHBrowserDescription, 0, func() {
		selector := ui.Select(page.c, &state.sshBrowser, sshBrowserChoices, state.settingsSSHBrowserLabel).
			Label(m.SettingsSSHBrowser).Icons(sshBrowserIcons)
		if selector.Changed() {
			page.c.After(state.save)
		}
	})
}

func (session *Session) canOpenSSHBrowser() bool {
	return session.hasServer() && !session.disconnected && session.conn != nil && session.phase == sshconn.PhaseConnected
}

func (state *State) openSSHBrowser(session *Session) {
	if !session.canOpenSSHBrowser() || session.browser != nil && session.browser.Running() {
		return
	}
	m := state.messages
	browser, err := startSSHBrowser(session.conn.DialContext, state.sshBrowser)
	switch {
	case errors.Is(err, sshbrowser.ErrBrowserNotFound):
		detail := m.SSHBrowserNotFound
		if state.sshBrowser != sshbrowser.FamilyAuto {
			detail = fmt.Sprintf(m.SSHBrowserChosenNotFoundFmt, state.settingsSSHBrowserLabel(state.sshBrowser))
		}
		state.showActionError(m.MenuOpenSSHBrowser, detail)
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

// settingsSSHBrowserLabel is how the setting names one browser family.
func (state *State) settingsSSHBrowserLabel(family sshbrowser.Family) string {
	m := state.messages
	switch family {
	case sshbrowser.FamilyChrome:
		return m.SSHBrowserChrome
	case sshbrowser.FamilyEdge:
		return m.SSHBrowserEdge
	case sshbrowser.FamilyChromium:
		return m.SSHBrowserChromium
	}
	return m.SSHBrowserAuto
}
