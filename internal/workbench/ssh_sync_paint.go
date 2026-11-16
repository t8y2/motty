package workbench

import "github.com/vivaui/viva/components"

// paintSSHSyncSettings is the group of Connection settings for the sync with the
// OpenSSH config. With several windows open they show and change the one the
// first window keeps.
func (state *State) paintSSHSyncSettings(page *settingsPage) {
	m := state.messages.SSHSync
	owner := state.syncLeader()
	owner.sshSyncLoad()
	view := &owner.sshSync
	on := view.file.Enabled
	page.group(m.Group, func() {
		page.toggleRow(m.ToggleRow, m.ToggleDescription, on, hitSettingsSSHSyncToggle)
		page.actionRow(m.FileRow, owner.sshConfigPathText(), !on,
			components.ButtonAction{ID: hitSettingsSSHSyncFile, Label: m.FileChoose, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitSettingsSSHSyncReset, Label: m.FileReset, Variant: components.ButtonGhost})
		page.actionRow(m.NowRow, owner.sshSyncDescription(), !on,
			components.ButtonAction{ID: hitSettingsSSHSyncNow, Label: m.NowButton, Variant: components.ButtonOutline})
	})
}
