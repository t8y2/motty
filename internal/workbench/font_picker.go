package workbench

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

// The terminal's font. The two built-in faces are always there; any installed
// fixed-pitch family can be chosen from a list that is read from the system when
// the picker is first opened (reading it takes a moment, so never on a paint).
//
// Behind the font sit up to maxFallbackFonts more families, tried in order for the
// characters it has no glyph for (a Latin programming font and a CJK one, say). The
// picker serves both: it opens for the font, or for the next fallback.
//
// The same picker also chooses the interface's font (interface_font.go): what
// differs between the three is the list of families it offers, the rows it adds
// to it, what the highlighted row is previewed in, and what confirming does;
// the search, the highlight, the keys, the double click and the buttons are one.

// fontPickerHits is the place of the controls this file paints.
var fontPickerHits = tree.Feature("font-picker")

var (
	hitFontPickerSearch      = tree.ID(fontPickerHits, "font-picker-search")
	hitFontPickerDismiss     = tree.ID(fontPickerHits, "font-picker-dismiss")
	hitFontPickerClose       = tree.ID(fontPickerHits, "font-picker-close")
	hitFontPickerCancel      = tree.ID(fontPickerHits, "font-picker-cancel")
	hitFontPickerConfirm     = tree.ID(fontPickerHits, "font-picker-confirm")
	hitSettingsFontPick      = tree.ID(fontPickerHits, "settings-font-pick")
	hitSettingsFallbackAdd   = tree.ID(fontPickerHits, "settings-font-fallback-add")
	hitSettingsFallbackClear = tree.ID(fontPickerHits, "settings-font-fallback-clear")
	hitFontPickerItemList    = tree.Sub(fontPickerHits, tree.Name("font-picker-item"))
)

const fontPickerVisibleRows = 8

// fontPickerKind is what the picker is choosing.
type fontPickerKind uint8

const (
	pickTerminalFont  fontPickerKind = iota // the terminal's font, the built-in faces included
	pickFallbackFont                        // a family behind the terminal's font
	pickInterfaceFont                       // the family of the window's own text
)

// fontList is a list of installed families, read off the window thread when it is
// first wanted (reading it takes a moment, so never on a paint).
type fontList struct {
	// installed is what the system listed; nil until it has been read.
	installed []string
	loading   bool
}

// pickerList is the list the open picker offers: the fixed-pitch families for the
// terminal, every family for the interface.
func (state *State) pickerList() *fontList {
	if state.fonts.kind == pickInterfaceFont {
		return &state.uiFont.fontList
	}
	return &state.fonts.fontList
}

// pickerFamilies reads the installed families the open picker offers.
func (state *State) pickerFamilies() func() []string {
	if state.fonts.kind == pickInterfaceFont {
		return state.options.FontFamilies
	}
	return state.options.MonoFamilies
}

// fallbackActions are the buttons of the fallback fonts row: add one more while there
// is room, and clear them once there is one.
func (state *State) fallbackActions() []components.ButtonAction {
	m := state.messages.Tools
	fonts := &state.fonts
	if fonts.actionCountFor != len(fonts.fallbacks) || fonts.actionAddLabel != m.FontFallbackAdd || fonts.actionClearLabel != m.FontFallbackClear {
		fonts.actionCountFor = len(fonts.fallbacks)
		fonts.actionAddLabel, fonts.actionClearLabel = m.FontFallbackAdd, m.FontFallbackClear
		fonts.actions = [2]components.ButtonAction{}
		fonts.actionCount = 0
		if len(fonts.fallbacks) < maxFallbackFonts {
			fonts.actions[fonts.actionCount] = components.ButtonAction{ID: hitSettingsFallbackAdd, Label: m.FontFallbackAdd, Variant: components.ButtonOutline}
			fonts.actionCount++
		}
		if len(fonts.fallbacks) > 0 {
			fonts.actions[fonts.actionCount] = components.ButtonAction{ID: hitSettingsFallbackClear, Label: m.FontFallbackClear, Variant: components.ButtonOutline}
			fonts.actionCount++
		}
	}
	return fonts.actions[:fonts.actionCount]
}

// maxFallbackFonts is how many families can stand behind the terminal's font.
const maxFallbackFonts = 3

// fontEntry is one choice: a built-in face (family empty) or an installed family.
type fontEntry struct {
	label  string
	family string
	face   gfx.MonoFace // for built-in entries
}

type fontsUI struct {
	// family is the installed family in use, "" for a built-in face; classic then
	// says which one.
	family  string
	classic bool
	// main and bold are the weights the terminal's text is drawn at, as the
	// platform's font weights, or 0 for the face's own.
	main, bold int
	// appliedMain and appliedBold are the weights the engine was last told.
	appliedMain, appliedBold int
	// fallbacks are the families that draw what the font lacks, in the order the user
	// added them. It is what the user chose, kept as it is whatever this computer has
	// installed, so that data carried to another computer (a portable copy) keeps the
	// choice; usable is the part of it the face is made from.
	fallbacks []string
	usable    []string
	fontList
	// kind is what the picker is open for; the fields below are the picker's,
	// whichever it is open for.
	kind fontPickerKind

	search   text.Editor
	raw      string // the search text the filter was built for
	entries  []fontEntry
	filtered []int32 // indices into entries
	selected int     // index into filtered
	scroll   float32 // pixel offset of the filtered list
	built    bool

	// searchSlot is where the body's first slot sits: the search field is
	// painted into it after the body has laid it out. searchDraw is that paint,
	// kept once so a warm repaint makes no new method value.
	searchSlot ui.Box
	searchDraw func(*ui.DrawContext)
	// weightMain and weightBold are the picker's pending weights: what Confirm
	// applies, once the sample's family is taken.
	weightMain, weightBold int
	// weightChoices and weightName feed the weight drop-downs. They are kept
	// across paints, so a repaint of the open picker makes no new slice or
	// method value.
	weightChoices []int
	weightName    func(int) string
	// weights is what each family is installed with, read off the window thread
	// the first time a picker shows the family; weightsReading marks a read that
	// has not come back yet. Both pickers share them: a family's weights are the
	// family's.
	weights        map[string][]int
	weightsReading map[string]bool
	// pickerLoad reads the installed families the open picker offers. The picker
	// makes it when it opens and drops it on close, so closing the picker cancels
	// the read and the next opening starts clean.
	pickerLoad *ui.EffectState[[]string, int]

	// text is the description the settings row shows; textFor and textLang are what
	// it was made for.
	text, textFor string
	textLang      i18n.Language
	// fallbackText is the same for the fallback row.
	fallbackText, fallbackFor string
	fallbackLang              i18n.Language
	// actions are the fallback row's buttons, cached for the settings page's
	// zero-allocation repaint path.
	actions                          [2]components.ButtonAction
	actionCount, actionCountFor      int
	actionAddLabel, actionClearLabel string
}

// fontName is the name of the font in use, in the current language.
func (state *State) fontName() string {
	switch {
	case state.fonts.family != "":
		return state.fonts.family
	case state.fonts.classic:
		return state.messages.SettingsTerminalFontClassic
	}
	return state.messages.SettingsTerminalFontSystem
}

// fontDescription is the settings row's explanation: what the font is now. It is
// made again only when that changes.
func (state *State) fontDescription() string {
	name := state.fontName()
	if state.fonts.textFor != name || state.fonts.textLang != state.language || state.fonts.text == "" {
		state.fonts.textFor, state.fonts.textLang = name, state.language
		state.fonts.text = fmt.Sprintf(state.messages.Tools.FontCurrentFmt, name)
	}
	return state.fonts.text
}

// fallbackDescription is the explanation of the fallback fonts row: the families in
// order, or what the row is for while there are none.
func (state *State) fallbackDescription() string {
	fonts := &state.fonts
	if len(fonts.fallbacks) == 0 {
		return state.messages.Tools.FontFallbackEmpty
	}
	key := strings.Join(fonts.fallbacks, "\n")
	if fonts.fallbackFor != key || fonts.fallbackLang != state.language || fonts.fallbackText == "" {
		fonts.fallbackFor, fonts.fallbackLang = key, state.language
		fonts.fallbackText = fmt.Sprintf(state.messages.Tools.FontFallbackFmt, strings.Join(fonts.fallbacks, " · "))
	}
	return fonts.fallbackText
}

// installedFamily reports whether the system listed family. While the list has not
// been read every family is taken to be installed: the face is made again once it
// arrives.
func (state *State) installedFamily(family string) bool {
	if state.fonts.installed == nil {
		return true
	}
	return slices.Contains(state.fonts.installed, family)
}

// terminalFace is the face the terminal draws in: the font with the fallbacks that
// are installed behind it. The fallbacks the user chose are kept as they are; this
// is the part of them this computer can use.
func (state *State) terminalFace() gfx.MonoFace {
	fonts := &state.fonts
	fonts.usable = fonts.usable[:0]
	for _, family := range fonts.fallbacks {
		if state.installedFamily(family) {
			fonts.usable = append(fonts.usable, family)
		}
	}
	base := gfx.MonoSystem
	switch {
	case fonts.family != "":
		base = gfx.MonoFamily(fonts.family)
	case fonts.classic:
		base = gfx.MonoClassic
	}
	return gfx.MonoStack(base, fonts.usable...)
}

// applyTerminalFace makes the terminal draw in the font, fallbacks and weights as
// they stand. It reports whether anything changed.
func (state *State) applyTerminalFace() bool {
	fonts := &state.fonts
	changed := state.setTerminalFontFace(state.terminalFace())
	if fonts.main != fonts.appliedMain || fonts.bold != fonts.appliedBold {
		if state.options.SetTerminalFontWeights != nil {
			state.options.SetTerminalFontWeights(fonts.main, fonts.bold)
		}
		fonts.appliedMain, fonts.appliedBold = fonts.main, fonts.bold
		changed = true
	}
	return changed
}

// cleanFallbackFonts is the fallback list a saved file or the picker may hold: names a
// font can have, without the font itself or a repeat, and no more than the limit.
func cleanFallbackFonts(names []string, primary string) []string {
	var kept []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if gfx.MonoFamily(name) == gfx.MonoSystem || name == primary || slices.Contains(kept, name) {
			continue
		}
		if len(kept) == maxFallbackFonts {
			break
		}
		kept = append(kept, name)
	}
	return kept
}

// listFonts reads the installed families off the window thread and delivers the
// list to fn on it. list is nil where the caller has none.
func (state *State) listFonts(list func() []string, fn func(*State, []string)) {
	if list == nil || !state.aiPost(func(*State) {}) { // none, or the window is closing
		return
	}
	go func() {
		families := list()
		state.aiPost(func(s *State) { fn(s, families) })
	}()
}

// validateTerminalFont checks, once the list can be read, that the saved family
// is still installed, and falls back to the system face when it is not. The
// fallback families that are not installed are left out of the face, but stay in the
// saved list: a copy carried to another computer keeps what was chosen there.
func (state *State) validateTerminalFont() {
	if state.fonts.family == "" && len(state.fonts.fallbacks) == 0 {
		return
	}
	family := state.fonts.family
	state.listFonts(state.options.MonoFamilies, func(s *State, families []string) {
		s.fonts.installed = families
		s.fonts.built = false
		if s.fonts.family != family {
			return // the user chose another while the list was being read
		}
		if family != "" && !slices.Contains(families, family) {
			s.fonts.family, s.fonts.classic = "", false
			s.applyTerminalFace()
			s.status = fmt.Sprintf(s.messages.Tools.FontMissingFmt, family)
			s.save()
			return
		}
		s.applyTerminalFace()
	})
}

// openFontPicker opens the picker for the font, or, with fallback, for the next
// family behind it.
func (state *State) openFontPicker(fallback bool) {
	if fallback {
		state.openPicker(pickFallbackFont)
		return
	}
	state.openPicker(pickTerminalFont)
}

// openPicker opens the picker for what kind names, and starts reading the list it
// offers if that has not been read. The read is an effect of the window page: it
// runs once, and closing the picker cancels it.
func (state *State) openPicker(kind fontPickerKind) {
	state.modal = modalFontPicker
	state.fonts.kind = kind
	state.fonts.search = text.Editor{}
	state.fonts.built = false
	if kind == pickInterfaceFont {
		// The weights the picker starts from: what is in effect, clamped to the
		// highlighted family once its weights are read.
		state.fonts.weightMain, state.fonts.weightBold = state.uiFont.main, state.uiFont.bold
	} else if kind == pickTerminalFont {
		state.fonts.weightMain, state.fonts.weightBold = state.fonts.main, state.fonts.bold
	}
	list := state.pickerList()
	if list.installed != nil || list.loading {
		// The list is known, or another opening is already reading it.
		return
	}
	fonts := &state.fonts
	fonts.closePickerPage()
	families := state.pickerFamilies()
	fonts.pickerLoad = ui.NewEffect[[]string, int](func(context.Context) ([]string, error) {
		return families(), nil
	})
	list.loading = true
}

// finishFontPickerLoad applies the families the picker's effect read to the list
// the picker is showing.
func (state *State) finishFontPickerLoad(families []string) {
	fonts := &state.fonts
	list := state.pickerList()
	list.loading = false
	if families == nil {
		families = []string{}
	}
	list.installed = families
	fonts.built = false
	if fonts.kind == pickInterfaceFont {
		state.reconcileInterfaceFont()
		return
	}
	state.applyTerminalFace()
}

// closePickerPage drops the picker's read, leaving the lists as they are. A
// read that had not finished is cancelled when the page stops declaring it; its
// result is dropped.
func (fonts *fontsUI) closePickerPage() {
	fonts.pickerLoad = nil
}

// closeFontPickerPage closes the open picker's page and read. The list's loading
// flag is cleared when nothing arrived, so the next opening asks again.
func (state *State) closeFontPickerPage() {
	list := state.pickerList()
	if list.installed == nil {
		list.loading = false
	}
	state.fonts.closePickerPage()
}

// refreshFontPicker builds the list the picker shows for its search text.
func (state *State) refreshFontPicker() {
	fonts := &state.fonts
	raw := fonts.search.String()
	if fonts.built && raw == fonts.raw {
		return
	}
	first := !fonts.built
	fonts.built, fonts.raw = true, raw
	fonts.entries = fonts.entries[:0]
	switch fonts.kind {
	case pickTerminalFont:
		fonts.entries = append(fonts.entries,
			fontEntry{label: state.messages.SettingsTerminalFontSystem, face: gfx.MonoSystem},
			fontEntry{label: state.messages.SettingsTerminalFontClassic, face: gfx.MonoClassic})
	case pickInterfaceFont:
		fonts.entries = append(fonts.entries, fontEntry{label: state.messages.SettingsInterfaceFontDefault})
	}
	for _, name := range state.pickerList().installed {
		if fonts.kind == pickFallbackFont && (name == fonts.family || slices.Contains(fonts.fallbacks, name)) {
			continue // already drawing with it
		}
		fonts.entries = append(fonts.entries, fontEntry{label: name, family: name})
	}
	query := strings.ToLower(strings.TrimSpace(raw))
	fonts.filtered = fonts.filtered[:0]
	current := -1
	for index, entry := range fonts.entries {
		if query != "" && !strings.Contains(strings.ToLower(entry.label), query) {
			continue
		}
		if fonts.kind != pickFallbackFont && state.fontInUse(entry) {
			current = len(fonts.filtered)
		}
		fonts.filtered = append(fonts.filtered, int32(index))
	}
	fonts.selected, fonts.scroll = 0, 0
	if first && current >= 0 {
		fonts.selected = current
		fonts.scroll = min(float32(max(current-fontPickerVisibleRows/2, 0))*fontPickerRowStep, fonts.maxScroll())
	}
}

// moveFontSelection moves the highlighted row and keeps it in view.
func (state *State) moveFontSelection(delta int) {
	fonts := &state.fonts
	state.refreshFontPicker()
	if len(fonts.filtered) == 0 {
		return
	}
	fonts.selected = min(max(fonts.selected+delta, 0), len(fonts.filtered)-1)
	top := float32(fonts.selected) * fontPickerRowStep
	fonts.scroll = min(max(fonts.scroll, top+30-fontPickerListHeight), top)
	fonts.scroll = min(max(fonts.scroll, 0), fonts.maxScroll())
}

const fontPickerRowStep float32 = 32
const fontPickerListHeight float32 = fontPickerVisibleRows * fontPickerRowStep

// The body's room: the search field, the list and the sample. The interface picker
// adds one row for each of the family's two weights.
const (
	fontPickerBodyHeight        float32 = 366
	fontPickerWeightRowHeight   float32 = 36
	fontPickerWeightChoiceWidth float32 = 300
)

func (fonts *fontsUI) contentHeight() float32 {
	return max(0, float32(len(fonts.filtered))*fontPickerRowStep-2)
}

func (fonts *fontsUI) maxScroll() float32 {
	return max(0, fonts.contentHeight()-fontPickerListHeight)
}

func (state *State) scrollFontPicker(event gfx.ScrollEvent) gfx.EventResult {
	state.refreshFontPicker()
	fonts := &state.fonts
	before := fonts.scroll
	fonts.scroll = min(max(before+event.StepY(fontPickerRowStep), 0), fonts.maxScroll())
	return gfx.EventResult{Handled: true, Repaint: fonts.scroll != before}
}

// selectFont updates the highlighted row so the sample previews it.
func (state *State) selectFont(row int) {
	fonts := &state.fonts
	state.refreshFontPicker()
	if row < 0 || row >= len(fonts.filtered) {
		return
	}
	fonts.selected = row
}

// canConfirmFont reports whether the highlighted row can be applied: the Confirm
// button, Enter and a double click all ask this, so none of them can do what the
// others are shown to refuse. Nothing can be applied while the installed families are
// still being read, and nothing is highlighted when no font matches the search.
func (state *State) canConfirmFont() bool {
	fonts := &state.fonts
	state.refreshFontPicker()
	return !state.pickerList().loading && fonts.selected >= 0 && fonts.selected < len(fonts.filtered)
}

// pendingWeights is the weight pair Confirm applies to family. The pair is only
// the user's while the drop-downs that show it are there: a family whose weights
// are not known, or that the platform cannot report, gets the platform's own, so a
// pair chosen for another family is never applied unseen.
func (state *State) pendingWeights(family string) (main, bold int) {
	if len(state.familyWeights(family)) == 0 {
		return 0, 0
	}
	return state.fonts.weightMain, state.fonts.weightBold
}

// confirmFont applies the highlighted row and closes the picker.
func (state *State) confirmFont() {
	if !state.canConfirmFont() {
		return
	}
	fonts := &state.fonts
	entry := fonts.entries[fonts.filtered[fonts.selected]]
	kind := fonts.kind
	state.closeModal()
	var changed bool
	switch kind {
	case pickFallbackFont:
		changed = state.addFallbackFont(entry.family)
	case pickInterfaceFont:
		main, bold := state.pendingWeights(entry.family)
		changed = state.setInterfaceFont(entry.family, main, bold)
	default:
		main, bold := state.pendingWeights(entry.family)
		changed = state.useTerminalFontWeights(entry, main, bold)
	}
	if changed {
		state.save()
	}
}

// cancelFontPicker closes the picker without applying unconfirmed selections.
func (state *State) cancelFontPicker() {
	state.closeModal()
}

// fontSampleFace is the face the picker's sample is set in: the highlighted font with
// the fallbacks behind it, or, when a fallback is being chosen, the terminal's own
// face with the highlighted family added, so the sample shows the mixed text.
func (state *State) fontSampleFace(entry fontEntry) gfx.MonoFace {
	fonts := &state.fonts
	if fonts.kind == pickFallbackFont {
		base := gfx.MonoSystem
		switch {
		case fonts.family != "":
			base = gfx.MonoFamily(fonts.family)
		case fonts.classic:
			base = gfx.MonoClassic
		}
		families := append(slices.Clone(fonts.usable), entry.family)
		return gfx.MonoStack(base, families...)
	}
	base := entry.face
	if entry.family != "" {
		base = gfx.MonoFamily(entry.family)
	}
	return gfx.MonoStack(base, fonts.usable...)
}

// fontInUse reports whether entry is the font the picker is choosing.
func (state *State) fontInUse(entry fontEntry) bool {
	if state.fonts.kind == pickInterfaceFont {
		return entry.family == state.uiFont.family
	}
	if entry.family != "" {
		return entry.family == state.fonts.family
	}
	return state.fonts.family == "" && (entry.face == gfx.MonoClassic) == state.fonts.classic
}

// useTerminalFont switches the terminal to a font, keeping the weights it draws at.
// It reports whether anything changed.
func (state *State) useTerminalFont(entry fontEntry) bool {
	return state.useTerminalFontWeights(entry, state.fonts.main, state.fonts.bold)
}

// useTerminalFontWeights switches the terminal to a font and the two weights its
// text is drawn at. It reports whether anything changed.
func (state *State) useTerminalFontWeights(entry fontEntry, main, bold int) bool {
	fonts := &state.fonts
	family, classic := entry.family, false
	if family == "" {
		// A built-in face has no weights of its own to choose.
		classic = entry.face == gfx.MonoClassic
		main, bold = 0, 0
	} else if gfx.MonoFamily(family) == gfx.MonoSystem {
		return false
	}
	changed := fonts.family != family || fonts.classic != classic || fonts.main != main || fonts.bold != bold
	fonts.family, fonts.classic = family, classic
	fonts.main, fonts.bold = main, bold
	// A family cannot stand behind itself.
	if before := len(fonts.fallbacks); family != "" {
		fonts.fallbacks = slices.DeleteFunc(fonts.fallbacks, func(name string) bool { return name == family })
		changed = changed || len(fonts.fallbacks) != before
	}
	return state.applyTerminalFace() || changed
}

// addFallbackFont puts a family behind the others. It reports whether anything changed.
func (state *State) addFallbackFont(family string) bool {
	fonts := &state.fonts
	if family == "" || len(fonts.fallbacks) >= maxFallbackFonts || family == fonts.family || slices.Contains(fonts.fallbacks, family) || gfx.MonoFamily(family) == gfx.MonoSystem {
		return false
	}
	fonts.fallbacks = append(fonts.fallbacks, family)
	state.applyTerminalFace()
	return true
}

// clearFallbackFonts removes every family behind the font. It reports whether there was one.
func (state *State) clearFallbackFonts() bool {
	if len(state.fonts.fallbacks) == 0 {
		return false
	}
	state.fonts.fallbacks = nil
	state.applyTerminalFace()
	return true
}

func (state *State) clickFontPicker(id gfx.ID) {
	switch {
	case id == hitFontPickerDismiss || id == hitFontPickerClose || id == hitFontPickerCancel:
		state.cancelFontPicker()
	case id == hitFontPickerConfirm:
		state.confirmFont()
	}
}

// fontPickerKey handles the keys of the picker. It reports whether it took the key.
func (state *State) fontPickerKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalFontPicker {
		return gfx.EventResult{}, false
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	move := func(delta int) (gfx.EventResult, bool) {
		state.moveFontSelection(delta)
		// Enter confirms the focused row, so list navigation must move focus too.
		// Navigation from the search field keeps its text editor focused.
		if _, onRow := event.Target.In(hitFontPickerItemList); onRow && len(state.fonts.filtered) > 0 {
			handled.Focus = hitFontPickerItemList.At(state.fonts.selected)
			handled.FocusVisible = true
		}
		return handled, true
	}
	if event.Key == gfx.KeyUp || event.Key == gfx.KeyDown || event.Key == gfx.KeyPageUp || event.Key == gfx.KeyPageDown {
		// The list navigates with the arrows only from its own rows or the search
		// field: a weight drop-down the dialog opened keeps the keys while it has
		// them, or the highlight would move behind the open list.
		_, onRow := event.Target.In(hitFontPickerItemList)
		if !event.Target.IsZero() && event.Target != hitFontPickerSearch && !onRow {
			return gfx.EventResult{}, false
		}
	}
	switch event.Key {
	case gfx.KeyEscape:
		state.cancelFontPicker()
		return handled, true
	case gfx.KeyUp:
		return move(-1)
	case gfx.KeyDown:
		return move(1)
	case gfx.KeyPageUp:
		return move(-fontPickerVisibleRows)
	case gfx.KeyPageDown:
		return move(fontPickerVisibleRows)
	case gfx.KeyEnter:
		// Enter confirms from the search field and the list. On any other control, the
		// Cancel and close buttons, it is that control's own activation.
		row, onRow := event.Target.In(hitFontPickerItemList)
		if event.Target != hitFontPickerSearch && !onRow && !event.Target.IsZero() {
			return gfx.EventResult{}, false
		}
		if !event.Repeat {
			if onRow {
				state.refreshFontPicker()
				state.selectFont(row)
			}
			state.confirmFont()
		}
		return handled, true
	}
	return gfx.EventResult{}, false
}

// confirmFontOnDoubleClick is called while painting the picker: when this paint is the
// event pass of a double click on a row, that row is applied. Consuming the press
// keeps its release from clicking whatever is under the pointer once the picker is
// gone. It reports whether the picker was closed.
func (state *State) confirmFontOnDoubleClick(canvas *gfx.Canvas) bool {
	index, event, ok := canvas.PointerIn(hitFontPickerItemList)
	if !ok || event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonPrimary || event.ClickCount != 2 {
		return false
	}
	state.selectFont(index)
	canvas.Consume()
	if !state.canConfirmFont() {
		return false
	}
	state.confirmFont()
	return true
}

// declareFontPicker declares the picker as a dialog of the window page: its
// search field, the list of families and the sample of the highlighted one. The
// search input's text is painted after the page, into the slot this declares.
func (state *State) declareFontPicker(c ui.C) {
	state.refreshFontPicker()
	// A double click on a row applies it. It is read while the page declares,
	// before the rows do, so its press does not also click what is under the
	// pointer once the picker is gone.
	if state.confirmFontOnDoubleClick(c.Canvas()) {
		return
	}
	m, fonts := state.messages.Tools, &state.fonts
	title := m.FontPickerTitle
	switch fonts.kind {
	case pickFallbackFont:
		title = m.FontFallbackPickerTitle
	case pickInterfaceFont:
		title = state.messages.SettingsInterfaceFont
	}
	viewport := dialogViewport(c)
	bodyHeight := fontPickerBodyHeight
	if fonts.kind != pickFallbackFont {
		// The picker chooses the family's two weights as well. Their room is kept
		// whether or not the family's weights are known yet, so the dialog does not
		// grow when the read lands.
		bodyHeight += 2 * fontPickerWeightRowHeight
	}
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 560, "", bodyHeight)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 560, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitFontPickerDismiss, CloseID: hitFontPickerClose,
		InitialFocusID: hitFontPickerSearch,
	})
	dialog.Body(func() { state.declareFontPickerBody(c) })
	dialog.Footer(func() {
		confirmDisabled := !state.canConfirmFont()
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitFontPickerCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitFontPickerConfirm, Label: m.FontPickerConfirm, Variant: components.ButtonDefault, Disabled: confirmDisabled},
		)
	})
}

// paintFontPickerSearch paints the picker's search field into the slot the
// dialog body laid out. It runs as part of the body's paint, inside the dialog's
// focus scope, so the field keeps the keys and text the dialog routes.
func (state *State) paintFontPickerSearch(d *ui.DrawContext) {
	fonts := &state.fonts
	m := state.messages.Tools
	title := m.FontPickerTitle
	switch fonts.kind {
	case pickFallbackFont:
		title = m.FontFallbackPickerTitle
	case pickInterfaceFont:
		title = state.messages.SettingsInterfaceFont
	}
	value := fonts.search.String()
	focused := d.Canvas.Focused() == hitFontPickerSearch
	components.PaintInput(d.Canvas, *d.Theme, fonts.searchSlot.Rect(), components.InputSnapshot{
		ID: hitFontPickerSearch, Label: title, Value: value, Placeholder: m.FontPickerPlaceholder,
		Focused: focused, PlatformEditing: focused && value != "", NavigationKeys: true,
	})
}

// declareFontPickerBody declares the dialog body: the search field's room, the
// list of families (loading, empty or its visible rows) and the sample under it.
// The list keeps room for all its rows, so the sample under it stays where it is.
func (state *State) declareFontPickerBody(c ui.C) {
	m, pal, fonts := state.messages.Tools, state.pal, &state.fonts
	if load := fonts.pickerLoad; load != nil {
		ui.Effect(c, load, 0)
		if families, _, ok := load.Take(); ok {
			fonts.pickerLoad = nil
			state.finishFontPickerLoad(families)
			state.refreshFontPicker() // the paint that takes the list also shows it
		}
	}
	fonts.scroll = min(max(fonts.scroll, 0), fonts.maxScroll())
	rows := len(fonts.filtered)
	if fonts.searchDraw == nil {
		fonts.searchDraw = state.paintFontPickerSearch
	}
	column := ui.Column(c).Fill()
	column.Children(func() {
		fonts.searchSlot = ui.Slot(c).Height(36)
		ui.Column(c).Height(fontPickerListHeight).Shrink(0).MarginSides(10, 0, 0, 0).Children(func() {
			switch {
			case state.pickerList().loading:
				ui.Text(c, m.FontPickerLoading).FontSize(metrics.FontSizeM).Color(pal.muted).LineHeight(24).TextCenter().MarginSides(10, 0, 0, 0)
			case rows == 0:
				ui.Text(c, m.FontPickerEmpty).FontSize(metrics.FontSizeM).Color(pal.muted).LineHeight(24).TextCenter().MarginSides(10, 0, 0, 0)
			}
			if rows > 0 {
				state.declareFontPickerRows(c)
			}
		})
		if rows > 0 {
			// The highlighted font, set in itself.
			entry := fonts.entries[fonts.filtered[fonts.selected]]
			state.declareFontPickerSample(c, entry)
			if fonts.kind != pickFallbackFont {
				state.declareFontPickerWeightRows(c, entry)
			}
		}
	})
	column.DrawOver(fonts.searchDraw)
}

// declareFontPickerRows declares the visible rows of the list, translating them
// by the scroll offset the window keeps: the list keeps its layout work bounded
// by the viewport while retaining logical row IDs.
func (state *State) declareFontPickerRows(c ui.C) {
	pal, fonts := state.pal, &state.fonts
	rows := len(fonts.filtered)
	first := int(fonts.scroll / fontPickerRowStep)
	last := min(rows, first+fontPickerVisibleRows+1)
	ui.ScrollAt(c, fonts.scroll).Fill().Children(func() {
		if top := float32(first) * fontPickerRowStep; top > 0 {
			ui.Slot(c).Height(top)
		}
		for row := first; row < last; row++ {
			entry := fonts.entries[fonts.filtered[row]]
			selected := row == fonts.selected
			inUse := fonts.kind != pickFallbackFont && state.fontInUse(entry)
			color := pal.text
			line := ui.Row(c).Height(30).Shrink(0).MarginSides(0, 0, 2, 0).PadSides(0, 12, 0, 10).Items(ui.Center).Radius(radius.item).
				HitWith(hitFontPickerItemList.At(row), entry.label, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected})
			if selected {
				line.Background(pal.accent)
				color = pal.onAccent
			} else {
				line.Hover(ui.Look{}.Background(pal.surfaceHot))
			}
			if line.Clicked() {
				state.selectFont(row)
			}
			line.Children(func() {
				ui.Text(c, entry.label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(color).LineHeight(20).Grow(1).MarginSides(0, 12, 0, 0)
				if inUse {
					ui.Icon(c, lucideIcons[iconCheck]).Size(16, 16).Color(color)
				} else {
					ui.Spacer(c).Size(16, 16).Grow(0)
				}
			})
		}
		if rest := fonts.contentHeight() - float32(last)*fontPickerRowStep; rest > 0 {
			ui.Slot(c).Height(rest)
		}
	})
}

// declareFontPickerSample declares the sample of the highlighted font: the
// interface's own sample for that picker, the terminal's mono sample otherwise.
func (state *State) declareFontPickerSample(c ui.C, entry fontEntry) {
	if state.fonts.kind == pickInterfaceFont {
		state.declareInterfaceFontSample(c, entry)
		return
	}
	m, pal := state.messages.Tools, state.pal
	face := state.fontSampleFace(entry)
	ui.Column(c).Height(56).Shrink(0).PadSides(7, 11, 0, 11).Background(pal.terminal).Border(1, pal.line).Radius(radius.control).MarginSides(8, 0, 0, 0).Children(func() {
		ui.Text(c, m.FontPickerSample).FontSize(metrics.FontSizeM).Color(pal.text).MonoFace(face).LineHeight(20).Height(20)
		ui.Text(c, m.FontPickerSampleSymbols).FontSize(metrics.FontSizeM).Color(pal.muted).MonoFace(face).LineHeight(20).Height(20)
	})
}

// declareFontPickerWeightRows declares the picker's two weight rows: the weight the
// family's main text is drawn at and the weight its bold text is, each a drop-down
// over the family's own weights and the platform's own (Default). The rows appear
// once the platform has listed the family's weights; until then the choice keeps
// what is in effect, and a family the platform cannot report weights for — a
// built-in face, the platform's own interface font — keeps them too.
func (state *State) declareFontPickerWeightRows(c ui.C, entry fontEntry) {
	weights := state.familyWeights(entry.family)
	if len(weights) == 0 {
		return
	}
	fonts := &state.fonts
	fonts.weightMain = nearestWeight(fonts.weightMain, weights)
	fonts.weightBold = nearestWeight(fonts.weightBold, weights)
	// The platform's own weight is a choice as well, so a family's weight can be
	// chosen explicitly and taken away again.
	fonts.weightChoices = append(append(fonts.weightChoices[:0], 0), weights...)
	if fonts.weightName == nil {
		fonts.weightName = state.fontWeightChoiceName
	}
	m := state.messages.Tools
	state.declareFontWeightRow(c, m.FontWeightMain, &fonts.weightMain)
	state.declareFontWeightRow(c, m.FontWeightBold, &fonts.weightBold)
}

// declareFontWeightRow declares one weight drop-down row of the picker, over the
// choices declareFontPickerWeightRows has kept.
func (state *State) declareFontWeightRow(c ui.C, label string, value *int) {
	fonts, pal := &state.fonts, state.pal
	ui.Row(c).Height(fontPickerWeightRowHeight).Shrink(0).MarginSides(8, 0, 0, 10).Items(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(metrics.FontSizeM).Color(pal.muted).Grow(1)
		ui.Select(c, value, fonts.weightChoices, fonts.weightName).Width(fontPickerWeightChoiceWidth)
	})
}

// familyWeights is the weights family is installed with, or nil while the read is
// still in flight: reading them is the platform's and takes a moment, so it happens
// off the paint and the next paint shows them. A family with no weights listed is
// one the platform cannot report weights for, or the platform's own.
func (state *State) familyWeights(family string) []int {
	fonts, read := &state.fonts, state.options.FontWeights
	if family == "" || read == nil {
		return nil
	}
	if weights, ok := fonts.weights[family]; ok {
		return weights
	}
	if fonts.weightsReading == nil {
		fonts.weightsReading = make(map[string]bool)
	}
	if !fonts.weightsReading[family] {
		fonts.weightsReading[family] = true
		state.listWeights(family, func(s *State, family string, weights []int) {
			if s.fonts.weights == nil {
				s.fonts.weights = make(map[string][]int)
			}
			s.fonts.weights[family] = weights
			delete(s.fonts.weightsReading, family)
		})
	}
	return nil
}

// listWeights reads family's installed weights off the window thread and delivers
// them to fn on it.
func (state *State) listWeights(family string, fn func(*State, string, []int)) {
	read := state.options.FontWeights
	if read == nil || state.callbacksClosed.Load() { // none, or the window is closing
		return
	}
	go func() {
		weights := read(family)
		state.aiPost(func(s *State) { fn(s, family, weights) })
	}()
}

// fontWeightName is what the picker calls a weight: the style names fonts are
// installed under, which are not translated. "" is the platform's own weight.
func fontWeightName(weight int) string {
	switch weight {
	case 0:
		return ""
	case 100:
		return "Thin"
	case 200:
		return "ExtraLight"
	case 300:
		return "Light"
	case 350:
		return "SemiLight"
	case 400:
		return "Regular"
	case 500:
		return "Medium"
	case 600:
		return "SemiBold"
	case 700:
		return "Bold"
	case 800:
		return "ExtraBold"
	case 900:
		return "Black"
	}
	return strconv.Itoa(weight)
}

// fontWeightChoiceName is what the weight drop-downs call a weight: the style name
// fonts are installed under, and a message for the platform's own.
func (state *State) fontWeightChoiceName(weight int) string {
	if weight == 0 {
		return state.messages.Tools.FontWeightDefault
	}
	return fontWeightName(weight)
}

// nearestWeight is weight as one of the family's weights: itself when the family
// has it, 0 for the platform's own, the nearest one otherwise, and unchanged where
// the family's weights are not known.
func nearestWeight(weight int, weights []int) int {
	if weight == 0 || len(weights) == 0 {
		return weight
	}
	nearest := weights[0]
	for _, candidate := range weights {
		if weightGap(candidate, weight) < weightGap(nearest, weight) {
			nearest = candidate
		}
	}
	return nearest
}

// weightGap is how far apart two weights are: their numbers run low to high, 100
// to 900.
func weightGap(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
