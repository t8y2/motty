package workbench

import (
	"testing"
)

// The host form and the hosts page: the form's endpoint fields, its pickers and
// the cards the page shows. The scenes pin the geometry the surface keeps while
// its controls move from canvas painting to the page.
func init() {
	extraPaintScenes = append(extraPaintScenes,
		plainScene("hostform-new", func(t *testing.T, s *State) {
			s.openHostDialog()
		}),
		plainScene("hostform-edit", func(t *testing.T, s *State) {
			s.openEditHostID(hostByLabel(t, s, "Production").ID)
		}),
		plainScene("hostform-edit-auth-key", func(t *testing.T, s *State) {
			s.openEditHostID(hostByLabel(t, s, "Production").ID)
			s.editor.auth = authPrivateKey
			s.editor.applyProtocol(protocolSSH)
		}),
		plainScene("hostform-edit-group-open", func(t *testing.T, s *State) {
			s.openEditHostID(hostByLabel(t, s, "Production").ID)
			s.Click(hitDialogGroup)
		}),
		plainScene("hosts-grid", func(t *testing.T, s *State) {
			s.hostView = hostViewGrid
			s.hostsScroll = 0
		}),
		plainScene("hosts-side-tree", func(t *testing.T, s *State) {
			s.hostSide.open = true
			s.revealHostFilterGroup()
			s.rebuildHostTreeRows()
		}),
	)
}
