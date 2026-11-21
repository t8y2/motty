package workbench

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/localshell"
	"motty/internal/shellkind"
)

var (
	hitSettingsLocalStartupOpen = tree.ID(settingsHits, "settings-local-startup-open")
	hitLocalStartupClose        = tree.ID(settingsHits, "local-startup-close")
	hitLocalStartupCancel       = tree.ID(settingsHits, "local-startup-cancel")
	hitLocalStartupSubmit       = tree.ID(settingsHits, "local-startup-submit")
	hitLocalStartupField        = tree.ID(settingsHits, "local-startup-field")
)

// localStartupDialog edits one shell's startup command: the line typed into a
// new or reconnected shell of that kind once it is ready. It is a dialog rather
// than rows of the settings page so the page stays short, and so the command is
// changed in one place from either entry: the terminal settings and the Local
// Terminal menu.
type localStartupDialog struct {
	shell localshell.Choice
	// editor is the draft. It is not restricted: an invalid line stays visible
	// with an error instead of being silently rewritten, and Save refuses it.
	editor text.Editor
	form   ui.FormFieldState
	input  ui.Field
	bound  bool
}

// localStartupKey names a shell's command in the saved map. Windows executable
// paths and distribution names are case-insensitive; Unix paths keep their case
// so distinct installed programs do not share a command.
func localStartupKey(program string) string {
	if runtime.GOOS == "windows" {
		if _, wsl := localshell.WSLDistro(program); !wsl {
			program = filepath.Clean(program)
		}
		return strings.ToLower(program)
	}
	return program
}

// localStartupCommand is the line typed once into a new shell of program, or "".
func (state *State) localStartupCommand(program string) string {
	command := state.localStartupCommands[localStartupKey(program)]
	if !validLoginCommand(command) {
		return ""
	}
	return strings.TrimSpace(command)
}

// setLocalStartupCommand stores the command for program, or forgets it when
// empty.
func (state *State) setLocalStartupCommand(program, command string) {
	if !validLocalShell(program) || !validLoginCommand(command) {
		return
	}
	program = localStartupKey(program)
	command = strings.TrimSpace(command)
	if state.localStartupCommands[program] == command {
		return
	}
	if command == "" {
		delete(state.localStartupCommands, program)
	} else {
		if state.localStartupCommands == nil {
			state.localStartupCommands = make(map[string]string)
		}
		state.localStartupCommands[program] = command
	}
	state.save()
}

// cleanLocalStartupCommands keeps the commands that could have come from the
// dialog. A shell this computer no longer detects is kept: it may be installed
// again, and a saved tab may still name it.
func cleanLocalStartupCommands(commands map[string]string) map[string]string {
	var kept map[string]string
	for program, command := range commands {
		if !validLocalShell(program) || !validLoginCommand(command) {
			continue
		}
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		if kept == nil {
			kept = make(map[string]string)
		}
		kept[localStartupKey(program)] = command
	}
	return kept
}

// localShellSettingChoice is the shell the Default local shell row shows: the
// configured one when it is set, else this computer's detected default.
func (state *State) localShellSettingChoice() localshell.Choice {
	if index := state.localShellSelected(); index >= 0 {
		return state.localShellMenu.choices[index]
	}
	if state.localShell != "" {
		name := filepath.Base(state.localShell)
		if distro, ok := localshell.WSLDistro(state.localShell); ok {
			name = localshell.WSLName(distro)
		}
		return localshell.Choice{Name: name, Path: state.localShell}
	}
	return state.defaultShellChoice()
}

// shellListed reports whether this computer currently offers program.
func (state *State) shellListed(program string) bool {
	for _, choice := range state.localShellMenu.choices {
		if choice.Path == program {
			return true
		}
	}
	return false
}

// openLocalStartupCommandDialog edits shell's command. An empty shell names the
// last one edited, or the settings' default, so a row with no shell of its own
// still opens on something useful.
func (state *State) openLocalStartupCommandDialog(shell localshell.Choice) {
	if state.modal != modalNone {
		return
	}
	if shell.Path == "" {
		shell = state.localShellMenu.startup
	}
	if shell.Path == "" || !state.shellListed(shell.Path) {
		if !state.localShellMenu.loaded {
			state.refreshLocalShells()
		}
		shell = state.defaultShellChoice()
	}
	state.localStartupDialog = localStartupDialog{shell: shell}
	state.localStartupDialog.editor.Insert(state.localStartupCommand(shell.Path))
	state.localStartupDialog.editor.SetSelection(text.Range{End: state.localStartupDialog.editor.RuneCount()})
	state.localShellMenu.startup = shell
	state.dialogError = ""
	state.modal = modalLocalStartupCommand
}

// submitLocalStartupCommandDialog saves the draft and closes the dialog. A line
// that cannot be typed as one command keeps the dialog open with the error; an
// empty line clears the command.
func (state *State) submitLocalStartupCommandDialog() {
	dialog := &state.localStartupDialog
	command := strings.TrimSpace(dialog.editor.String())
	if !validLoginCommand(command) {
		state.dialogError = state.messages.SettingsLocalStartupInvalid
		return
	}
	state.setLocalStartupCommand(dialog.shell.Path, command)
	state.localShellMenu.startup = dialog.shell
	state.modal = modalNone
	state.dialogError = ""
}

func (state *State) clickLocalStartupCommandDialog(id gfx.ID) {
	switch id {
	case hitLocalStartupClose, hitLocalStartupCancel:
		state.modal = modalNone
		state.dialogError = ""
	case hitLocalStartupSubmit:
		state.submitLocalStartupCommandDialog()
	}
}

// localStartupCommandKey makes Enter in the field, or on the button, save.
func (state *State) localStartupCommandKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalLocalStartupCommand || event.Key != gfx.KeyEnter || event.Repeat {
		return gfx.EventResult{}, false
	}
	if event.Target != hitLocalStartupField && event.Target != hitLocalStartupSubmit {
		return gfx.EventResult{}, false
	}
	state.clickLocalStartupCommandDialog(hitLocalStartupSubmit)
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) declareLocalStartupCommandDialog(c ui.C) {
	messages := state.messages
	dialog := &state.localStartupDialog
	description := dialog.shell.Name
	if description == "" {
		description = dialog.shell.Path
	}
	// No scrim dismiss: a press beside the form must not discard its input.
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 480, description, dialogRowHeight(c.Theme()))
	box := ui.Dialog(c, ui.DialogSpec{
		Title: messages.SettingsLocalStartupCommands, Description: description,
		Width: 480, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitLocalStartupClose, InitialFocusID: hitLocalStartupField,
	})
	box.Body(func() {
		if !dialog.bound {
			dialog.input.Bind(&dialog.editor)
			dialog.bound = true
		}
		dialog.form.For, dialog.form.Error, dialog.form.Invalid = hitLocalStartupField, state.dialogError, state.dialogError != ""
		ui.FormField(c, &dialog.form, messages.SettingsLocalStartupCommand).FillWidth().Height(dialogRowHeight(c.Theme())).Children(func() {
			ui.TextFieldID(c, hitLocalStartupField, &dialog.input, messages.SettingsLocalStartupCommand, startupCommandExample(dialog.shell.Path))
		})
	})
	box.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitLocalStartupCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitLocalStartupSubmit, Label: messages.SettingsLocalStartupSave, Variant: components.ButtonDefault},
		)
	})
}

// paintLocalStartupSetting is the terminal settings row that opens the dialog
// for the default shell: one row instead of a list of every installed shell.
func (state *State) paintLocalStartupSetting(page *settingsPage) {
	messages := state.messages
	page.actionRow(messages.SettingsLocalStartupCommands, messages.SettingsLocalStartupDescription, false,
		components.ButtonAction{ID: hitSettingsLocalStartupOpen, Label: messages.SettingsLocalStartupEdit, Variant: components.ButtonOutline})
}

// clickLocalShellPickerSetup opens the dialog from the gear beside a shell in
// the Local Terminal menu.
func (state *State) clickLocalShellPickerSetup(id gfx.ID) bool {
	index, ok := id.In(hitLocalShellPickerSetup)
	if !ok || index < 0 || index >= len(state.localShellMenu.choices) {
		return false
	}
	choice := state.localShellMenu.choices[index]
	state.localShellPicker.open = false
	state.openLocalStartupCommandDialog(choice)
	return true
}

// paintLocalStartupGear is the gear that opens a shell's startup command from
// the Local Terminal menu.
func (state *State) paintLocalStartupGear(canvas *gfx.Canvas, id gfx.ID, slot gfx.Rect) {
	state.paintIconButton(canvas, id, iconSettings,
		gfx.Rect{X: slot.X + (slot.Width-24)/2, Y: slot.Y + (slot.Height-22)/2, Width: 24, Height: 22},
		state.messages.SettingsLocalStartupCommands, false, iconButtonQuiet, state.pal.surface)
}

// startupCommandExample is the placeholder the dialog shows: a command in the
// language of that shell, so the same dialog is useful to cmd.exe, PowerShell
// and a POSIX shell.
func startupCommandExample(program string) string {
	switch shellkind.FromProgram(program) {
	case shellkind.Cmd, shellkind.PowerShell:
		return "chcp 65001"
	case shellkind.Fish:
		return "set -gx LANG zh_CN.UTF-8"
	case shellkind.Nushell:
		return `$env.LANG = "zh_CN.UTF-8"`
	}
	return "export LANG=zh_CN.UTF-8"
}
