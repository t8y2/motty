package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/sshconn"
)

func init() {
	for category := settingsGeneral; category < settingsCategoryCount; category++ {
		if category == settingsMCP {
			continue // it shows the path of the test binary
		}
		extraPaintScenes = append(extraPaintScenes, plainScene(fmt.Sprintf("g6-settings-%d", category), func(t *testing.T, s *State) {
			s.openSettings()
			s.selectSettingsCategory(category)
		}))
	}
	extraPaintScenes = append(extraPaintScenes,
		plainScene("g6-rule-delete-autofill", func(t *testing.T, s *State) {
			s.ruleDelete = ruleDelete{kind: ruleDeleteAutofill, name: "Deploy", text: true}
			s.modal = modalDeleteRule
		}),
		plainScene("g6-rule-delete-credential-in-use", func(t *testing.T, s *State) {
			s.ruleDelete = ruleDelete{kind: ruleDeleteCredential, name: "Staging password", uses: 3}
			s.modal = modalDeleteRule
		}),
		plainScene("g6-rule-delete-selected", func(t *testing.T, s *State) {
			s.ruleDelete = ruleDelete{kind: ruleDeleteHighlight, selectedHighlights: make([]selectedHighlightRule, 2)}
			s.modal = modalDeleteRule
		}),
		plainScene("g6-sidebar-settings", func(t *testing.T, s *State) { s.openSidebarSettings() }),
		plainScene("g6-sidebar-settings-some-hidden", func(t *testing.T, s *State) {
			s.hiddenNav = s.hiddenNav.set(navPageNotes, true)
			s.openSidebarSettings()
		}),
		connectedScene("g6-tab-rename", func(s *State, session *Session) { s.openTerminalRename(session) }),
		connectedScene("g6-tab-rename-error", func(s *State, session *Session) {
			s.openTerminalRename(session)
			s.tabRename.editor.SetSelection(text.Range{End: s.tabRename.editor.RuneCount()})
			s.tabRename.editor.Insert("")
			s.clickTabRenameDialog(hitTabRenameSubmit)
		}),
		passwordScene("g6-password", 1, []sshconn.Question{{Text: "Password: ", Secret: true}}),
		passwordScene("g6-password-retry", 2, []sshconn.Question{{Text: "Password: ", Secret: true}}),
		passwordScene("g6-password-two-factor", 1, []sshconn.Question{{Text: "Password: ", Secret: true}, {Text: "Verification code: "}}),
		plainScene("g6-key-passphrase", func(t *testing.T, s *State) {
			s.keyPassphraseDescription = "The key id_ed25519 is protected."
			s.modal = modalKeyPassphrase
		}),
		plainScene("g6-key-passphrase-error", func(t *testing.T, s *State) {
			s.keyPassphraseDescription = "The key id_ed25519 is protected."
			s.keyPassphraseError = "That passphrase did not open the key."
			s.secrets.canSave = true
			s.modal = modalKeyPassphrase
		}),
		plainScene("g6-outbound-proxy-none", func(t *testing.T, s *State) { s.openOutboundProxyDialog() }),
		plainScene("g6-outbound-proxy-socks5", func(t *testing.T, s *State) {
			s.proxy = outboundProxy{kind: sshconn.ProxySOCKS5, address: "10.0.0.1:1080", user: "me"}
			s.openOutboundProxyDialog()
			s.proxyForm.password.Insert("secret")
		}),
		plainScene("g6-outbound-proxy-error", func(t *testing.T, s *State) {
			s.proxy = outboundProxy{kind: sshconn.ProxyHTTPConnect, address: "10.0.0.1:3128"}
			s.openOutboundProxyDialog()
			s.proxyForm.dialogError = "The proxy address is not valid."
		}),
		sessionLinkScene("g6-session-reconnect", false),
		sessionLinkScene("g6-session-disconnect", true),
	)
}

// passwordScene is the authentication dialog of a connection that asks questions.
func passwordScene(name string, attempt int, questions []sshconn.Question) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		world, options := newWorld(t)
		state := newTestState(t, options)
		fake := connect(t, state, world)
		fake.set(func(status *sshconn.Status) {
			status.Phase, status.Attempt, status.Questions = sshconn.PhasePasswordPrompt, attempt, questions
			status.Endpoint = sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}
		})
		state.Wake()
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}

// connectedScene is a scene over a window with one connected session.
func connectedScene(name string, setup func(*State, *Session)) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		world, options := newSharingWorld(t)
		state := newTestState(t, options)
		world.connectFirst(t, state)
		session, _ := state.sessionByIDPointer(state.activeSessionID)
		setup(state, session)
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}

func sessionLinkScene(name string, disconnect bool) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		world, options := newSharingWorld(t)
		state := newTestState(t, options)
		world.connectFirst(t, state)
		session, _ := state.sessionByIDPointer(state.activeSessionID)
		state.askSessionLink(session, disconnect)
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}
