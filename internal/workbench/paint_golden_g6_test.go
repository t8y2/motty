package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func init() {
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
		sessionLinkScene("g6-session-reconnect", false),
		sessionLinkScene("g6-session-disconnect", true),
	)
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
