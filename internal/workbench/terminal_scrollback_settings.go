package workbench

import "github.com/vivaui/viva/ui"

// The scrollback stepper: a field wide enough for six digits between its two
// buttons, in a control as wide as the other settings rows.
const (
	scrollbackValueWidth   = float32(84)
	scrollbackStepperWidth = float32(180)
)

// paintScrollbackSetting declares the Terminal page's scrollback row. Its
// middle field can be typed into; the buttons move the count in thousands and
// stop at the bounds the history budget was sized for.
func (state *State) paintScrollbackSetting(page *settingsPage) {
	m := state.messages
	page.lineWith(m.SettingsTerminalScrollback, m.SettingsTerminalScrollbackDescription, scrollbackStepperWidth, func() {
		ui.Row(page.c).Gap(6).Shrink(0).Items(ui.Center).Children(func() {
			page.button(hitSettingsScrollbackMinus, "−", aiButtonOutline, state.terminalScrollback <= minTerminalScrollback)
			state.declareScrollbackField(page.c)
			page.button(hitSettingsScrollbackPlus, "+", aiButtonOutline, state.terminalScrollback >= maxTerminalScrollback)
		})
	})
}

// beginScrollbackEdit opens the count for typing, seeded with the stored one.
func (state *State) beginScrollbackEdit() {
	state.scrollbackEdit.begin(state.terminalScrollback, 6)
}

// commitScrollbackEdit stores the typed count and applies it to the terminals
// already open.
func (state *State) commitScrollbackEdit() {
	if value, ok := state.scrollbackEdit.commit(); ok {
		if state.setTerminalScrollback(value) {
			state.save()
		}
	}
}

// stepTerminalScrollback moves the count by delta, settling a typed number
// first, and reports whether the setting changed.
func (state *State) stepTerminalScrollback(delta int) bool {
	state.commitScrollbackEdit()
	return state.setTerminalScrollback(state.terminalScrollback + delta)
}

// declareScrollbackField paints the count between the − and + buttons, showing
// the stored value unless an edit is open.
func (state *State) declareScrollbackField(c ui.C) ui.Box {
	return state.scrollbackEdit.declare(c, hitSettingsScrollbackValue, state.messages.SettingsTerminalScrollback, scrollbackValueWidth, state.terminalScrollback, 6)
}
