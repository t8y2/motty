package workbench

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

// The interface's own font: the family the window's text — menus, dialogs,
// settings, every label that is not a terminal — is shaped in. The platform
// draws it in its own interface font until the user names another family (one
// whose CJK and Latin glyphs read evenly together, say); "" is the platform's
// choice again. It is chosen in the terminal font's picker (font_picker.go),
// which is open here for every installed family.
//
// What the user chose and what is in effect are two things. The choice is saved
// as it is, whatever this computer has installed, so that a state carried to
// another computer keeps it; the family in effect is the choice where the
// platform can follow it and the system has the family, and the platform's own
// font where it cannot. The settings row says which it is.

var hitInterfaceFontChoose = tree.ID(tree.Feature("interface-font"), "choose")

// interfaceFontSample is what the picker sets in the highlighted family: Latin,
// CJK and digits side by side, since how evenly they read together is what the
// family is chosen for. It is a sample of text, not a message, so it is not
// translated.
const (
	interfaceFontSample  = "The quick brown fox 敏捷的棕色狐狸 0123456789"
	interfaceFontSample2 = "Aa Bb Cc 你好，世界！ (1) [2] {3} @ # % &"
)

type interfaceFontUI struct {
	fontList
	// family is the chosen family, "" for the platform's own interface font. It
	// is what the user chose and what is saved.
	family string
	// applied is the family the engine was last told: the choice, or "" when the
	// choice cannot be followed.
	applied string
	// missing is set when the system's list is known and does not have the choice.
	missing bool
	// known says the platform has been asked once whether it can follow a family
	// at all; supported is its answer.
	known, supported bool

	// text is the explanation the settings row shows, empty when the choice is in
	// effect; textMade says it has been made, since empty is a real answer; the
	// rest is what it was made for.
	text       string
	textMade   bool
	textFamily string
	textState  uint8
	textLang   i18n.Language
}

// interfaceFontRowVisible reports whether the settings page shows the row: where
// the platform can follow a family, and where a family was chosen that it cannot
// (a state carried from another platform), so that the row can say so.
func (state *State) interfaceFontRowVisible() bool {
	return state.uiFont.supported || state.uiFont.family != ""
}

// interfaceFontInEffect is the name the row shows for the font the window's text
// is actually drawn in: the family the engine was told, which is the platform's
// own where the choice cannot be followed. Showing the saved choice instead would
// name a font that is not in use, beside a description saying it is not.
func (state *State) interfaceFontInEffect() string {
	if state.uiFont.applied != "" {
		return state.uiFont.applied
	}
	return state.messages.SettingsInterfaceFontDefault
}

// interfaceFontDescription is the row's explanation when the choice is not in
// effect — the platform cannot follow a family, or the system no longer has it —
// and "" when it is: the row names the font itself, so there is nothing else to
// explain. It is made again only when that, or the language, changes.
func (state *State) interfaceFontDescription() string {
	fonts := &state.uiFont
	var kind uint8
	switch {
	case !fonts.supported:
		kind = 1
	case fonts.missing:
		kind = 2
	}
	if !fonts.textMade || fonts.textFamily != fonts.family || fonts.textState != kind || fonts.textLang != state.language {
		fonts.textMade = true
		fonts.textFamily, fonts.textState, fonts.textLang = fonts.family, kind, state.language
		switch kind {
		case 1:
			fonts.text = fmt.Sprintf(state.messages.SettingsInterfaceFontUnsupportedFmt, fonts.family)
		case 2:
			fonts.text = fmt.Sprintf(state.messages.Tools.FontMissingFmt, fonts.family)
		default:
			fonts.text = ""
		}
	}
	return fonts.text
}

// applyInterfaceFont tells the engine the family in effect. The first call is
// also the one question of whether the platform can follow a family at all, which
// its setter answers; the answer is kept, so nothing asks again, and the setter is
// not called again for a family it already has.
func (state *State) applyInterfaceFont() {
	fonts := &state.uiFont
	want := fonts.family
	if fonts.missing {
		want = ""
	}
	if fonts.known && (!fonts.supported || want == fonts.applied) {
		return
	}
	followed := state.options.SetInterfaceFont != nil && state.options.SetInterfaceFont(want)
	if !fonts.known {
		fonts.known, fonts.supported = true, followed
	}
	fonts.applied = ""
	if followed {
		fonts.applied = want
	}
}

// reconcileInterfaceFont decides, now that the system's list may be known,
// whether the choice is in effect. An empty list is no evidence that a family is
// missing (the platform may be unable to list at all), so it leaves the choice
// in effect.
func (state *State) reconcileInterfaceFont() {
	fonts := &state.uiFont
	fonts.missing = fonts.family != "" && len(fonts.installed) > 0 && !slices.Contains(fonts.installed, fonts.family)
	state.applyInterfaceFont()
}

// setInterfaceFont is the user's choice of the family the window's text is drawn
// in, or "" for the platform's own. It reports whether the choice changed.
func (state *State) setInterfaceFont(family string) bool {
	family = strings.TrimSpace(family)
	if family == state.uiFont.family {
		return false
	}
	state.uiFont.family = family
	state.reconcileInterfaceFont()
	return true
}

// validateInterfaceFont applies the saved choice and, once the system's list can
// be read, checks that the family is still installed. A family that is not is not
// used, and not forgotten either: the choice stays saved, the row says it is not
// in effect, and the family is used again where it is installed.
func (state *State) validateInterfaceFont() {
	state.applyInterfaceFont()
	if state.uiFont.family == "" || !state.uiFont.supported {
		return
	}
	state.listFonts(state.options.FontFamilies, func(s *State, families []string) {
		s.uiFont.installed = families
		s.fonts.built = false
		s.reconcileInterfaceFont()
	})
}

// openInterfaceFontPicker opens the choice of the interface's font.
func (state *State) openInterfaceFontPicker() {
	state.openPicker(pickInterfaceFont)
}

// paintInterfaceFontSample is the picker's sample for the interface: the
// highlighted family set in itself. The Default row has no family to set it in,
// so its sample is drawn in the interface font as it is, proportional like the
// rest of the window.
func (state *State) paintInterfaceFontSample(f *ui.Frame, entry fontEntry) {
	pal := state.pal
	f.Column().Height(56).Shrink(0).PadSides(7, 11, 0, 11).Background(pal.terminal).Border(1, pal.line).Radius(radius.control).MarginSides(8, 0, 0, 0).Children(func() {
		first := f.Text(interfaceFontSample).FontSize(metrics.FontSizeM).Color(pal.text).LineHeight(20).Height(20)
		second := f.Text(interfaceFontSample2).FontSize(metrics.FontSizeM).Color(pal.muted).LineHeight(20).Height(20)
		if entry.family != "" {
			face := gfx.MonoFamily(entry.family)
			first.MonoFace(face)
			second.MonoFace(face)
		}
	})
}
