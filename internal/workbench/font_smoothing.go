package workbench

var hitSettingsFontSmoothing = tree.ID(settingsHits, "settings-font-smoothing")

// fontSmoothingString is the saved form of the switch: only the off choice is written,
// because on is what the platform's text rasterizer already does.
func fontSmoothingString(off bool) string {
	if off {
		return "off"
	}
	return ""
}

// applyFontSmoothing draws the glyphs the window shows — the terminal's, the labels',
// the editor's — with the platform's font smoothing or with the font's own outlines.
// On is the default. It keeps the platform's answer to whether it has the choice at
// all: the settings row is shown only where it does. It runs where the saved choice is
// restored, so it shows without a restart, and when the user flips the switch.
func (state *State) applyFontSmoothing() {
	if state.options.SetFontSmoothing == nil {
		return
	}
	state.fontSmoothingSupported = state.options.SetFontSmoothing(!state.fontSmoothingOff)
}

// setFontSmoothing is the user's choice. The engine draws the window's text again on
// the next paint; nothing is reshaped.
func (state *State) setFontSmoothing(on bool) bool {
	if state.fontSmoothingOff == !on {
		return false
	}
	state.fontSmoothingOff = !on
	state.applyFontSmoothing()
	return true
}
