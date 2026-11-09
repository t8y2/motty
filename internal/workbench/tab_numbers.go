package workbench

import (
	"strconv"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

var (
	hitSettingsTabNumbers        = tree.ID(settingsHits, "tab-numbers")
	hitSettingsTabNumbersPrimary = tree.ID(settingsHits, "tab-numbers-primary")
	hitSettingsTabNumbersAlt     = tree.ID(settingsHits, "tab-numbers-alt")
	hitSettingsTabNumbersShift   = tree.ID(settingsHits, "tab-numbers-shift")
)

const (
	// tabNumberMinExtra is the room a number takes from the narrowest tab: two
	// digits (a ten-tab bar is ordinary) and the gap that keeps them off the name.
	tabNumberMinExtra = float32(20)
	// tabNumberGap separates a tab's number from the name it indexes.
	tabNumberGap = float32(6)
)

// tabNumberOptions are the keys a number shortcut may be pressed with: the
// platform's primary modifier, Alt, and the primary one with Shift. Shift alone
// is left out: a shifted digit reaches the application as the digit's symbol
// ("!" for Shift+1), so such a binding would either never match or take that
// symbol away from the terminal.
func (state *State) tabNumberOptions() [3]components.SegmentOption {
	primary, shifted := "Ctrl", "Ctrl+Shift"
	if keymap.Primary == gfx.ModifierMeta {
		primary, shifted = "Cmd", "Cmd+Shift"
	}
	return [...]components.SegmentOption{
		{ID: hitSettingsTabNumbersPrimary, Label: primary},
		{ID: hitSettingsTabNumbersAlt, Label: "Alt"},
		{ID: hitSettingsTabNumbersShift, Label: shifted},
	}
}

// tabNumberModifiers is the modifier set of one of those options.
func tabNumberModifiers(id gfx.ID) (gfx.Modifiers, bool) {
	primary := keymap.Primary
	switch id {
	case hitSettingsTabNumbersPrimary:
		return primary, true
	case hitSettingsTabNumbersAlt:
		return gfx.ModifierAlt, true
	case hitSettingsTabNumbersShift:
		return primary | gfx.ModifierShift, true
	}
	return 0, false
}

// tabNumberModifierID is the option the number family is bound to. A chord the
// user bound to some other combination on the shortcuts page has none: the
// settings row then shows no choice until one is pressed.
func (state *State) tabNumberModifierID() gfx.ID {
	chord := state.shortcuts.chords.Chord(shortcutTabNumber)
	for _, option := range state.tabNumberOptions() {
		if modifiers, _ := tabNumberModifiers(option.ID); modifiers == chord.Modifiers {
			return option.ID
		}
	}
	return gfx.ID{}
}

// setTabNumberModifier rebinds the whole digit row to one of the options. A
// chord another action owns leaves the row as it was.
func (state *State) setTabNumberModifier(id gfx.ID) bool {
	modifiers, ok := tabNumberModifiers(id)
	if !ok {
		return false
	}
	chord := keymap.Chord{Modifiers: modifiers, Char: '1'}
	if state.shortcuts.chords.Chord(shortcutTabNumber) == chord {
		return false
	}
	if _, bound := state.shortcuts.bind(shortcutTabNumber, chord); !bound {
		return false
	}
	return true
}

// setTabNumbers shows or hides the number each tab carries, and with it the
// number shortcuts: a key with no number on screen has nothing to name.
func (state *State) setTabNumbers(shown bool) bool {
	if state.tabNumbers == shown {
		return false
	}
	state.tabNumbers = shown
	// The narrowest tab keeps room for the number, so the bar's geometry is
	// measured again.
	state.chrome.geometry.valid = false
	return true
}

// tabNumber is a tab's number as text, or nothing for a tab number shortcuts do
// not reach.
func tabNumber(number int) string {
	if number <= 0 {
		return ""
	}
	return strconv.Itoa(number)
}

// tabNumberColor is the tab's text color with less ink: the number names the
// name, it does not compete with it.
func tabNumberColor(foreground gfx.Color) gfx.Color {
	foreground.Alpha *= .6
	return foreground
}

// shortcutActionFor is actionFor with the settings that switch an action off.
// The number row is the one action a setting can silence: with the numbers
// hidden, its keys belong to the terminal again.
func (state *State) shortcutActionFor(chord keymap.Chord) shortcutAction {
	action := state.shortcuts.actionFor(chord)
	if action == shortcutTabNumber && !state.tabNumbers {
		return shortcutNone
	}
	return action
}
