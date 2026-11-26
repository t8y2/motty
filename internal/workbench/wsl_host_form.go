package workbench

import (
	"slices"

	"github.com/vivaui/viva/ui"
	"motty/internal/localshell"
)

var (
	hitDialogWSLDistro    = tree.ID(hostProtocolUiHits, "wsl-distribution")
	hitDialogWSLUseUser   = tree.ID(hostProtocolUiHits, "wsl-use-user")
	hitDialogWSLUser      = tree.ID(hostProtocolUiHits, "wsl-user")
	hitDialogWSLDirectory = tree.ID(hostProtocolUiHits, "wsl-directory")
	hitDialogWSLCommand   = tree.ID(hostProtocolUiHits, "wsl-command")
	hitDialogWSLGraphics  = tree.ID(hostProtocolUiHits, "wsl-graphics")
)

func (state *State) declareWSLEndpoint(c ui.C, slots *hostEndpointSlots, height float32) {
	editor, m := &state.editor, state.messages.WSL
	form := &editor.form
	choices := append(form.wslDistroChoices[:0], "")
	for _, choice := range state.localShellMenu.distros {
		if distro, ok := localshell.WSLDistro(choice.Path); ok {
			choices = append(choices, distro)
		}
	}
	// A session synced from another computer can keep its current distribution
	// even while this computer cannot start it.
	if !slices.Contains(choices, editor.wslDistro) {
		choices = append(choices, editor.wslDistro)
	}
	form.wslDistroChoices = choices
	ui.Row(c).Height(height).Gap(8).Children(func() {
		slots.address = ui.Slot(c).Grow(1)
		slots.address.Children(func() {
			ui.FormField(c, &form.wslDistroField, m.Distribution).FillWidth().Height(height).Children(func() {
				form.wslDistroBox = ui.Select(c, &editor.wslDistro, choices, func(distro string) string {
					if distro == "" {
						return m.DefaultDistribution
					}
					return distro
				}).Label(m.Distribution)
				if form.wslDistroBox.Changed() {
					state.dialogError = ""
				}
			})
		})
	})
	ui.Row(c).Height(height).Gap(8).Items(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Children(func() {
			state.declareDialogCheck(c, hitDialogWSLUseUser, m.UseUser, editor.wslUseUser, 6, 6)
		})
		if editor.wslUseUser {
			slots.second = ui.Slot(c).Grow(1)
			state.declareHostTextField(c, slots.second, &form.wslUserField, hitDialogWSLUser, &editor.wslUser, m.User, "", false, height)
		}
	})
}

func (state *State) declareWSLAdvanced(c ui.C, height float32) {
	editor, m := &state.editor, state.messages.WSL
	form := &editor.form
	directory := ui.Slot(c).Height(height)
	state.declareHostTextField(c, directory, &form.wslDirectoryField, hitDialogWSLDirectory, &editor.wslDirectory, m.Directory, m.DirectoryHint, false, height)
	command := ui.Slot(c).Height(height)
	state.declareHostTextField(c, command, &form.wslCommandField, hitDialogWSLCommand, &editor.wslCommand, m.StartupCommand, "", false, height)
	state.declareDialogCheck(c, hitDialogWSLGraphics, m.Graphics, !editor.wslNoGUI, 6, 6)
	state.declareDialogCheck(c, hitDialogNoHistory, state.messages.History.HostNoRecord, editor.noHistory, 6, 6)
}
