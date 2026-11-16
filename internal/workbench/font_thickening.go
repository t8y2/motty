package workbench

var hitSettingsThickenToggle = tree.ID(settingsHits, "settings-thicken")

// applyFontThickening draws the glyphs the window shows — the terminal's, the labels',
// the editor's — with a thicker stroke or with the font's own outlines, which the
// platform's text rasterizer does with its font smoothing. Off is the default. It keeps
// the platform's answer to whether it has the choice at all: the settings row is shown
// only where it does. It runs where the saved weight is restored, so a stored choice
// shows without a restart, and when the user flips the switch.
func (state *State) applyFontThickening() {
	if state.options.SetFontThickening == nil {
		return
	}
	state.fontThickeningSupported = state.options.SetFontThickening(state.fontThickening)
}

// setFontThickening is the user's choice. The engine draws the window's text again on
// the next paint; nothing is reshaped.
func (state *State) setFontThickening(on bool) bool {
	if state.fontThickening == on {
		return false
	}
	state.fontThickening = on
	state.applyFontThickening()
	return true
}
