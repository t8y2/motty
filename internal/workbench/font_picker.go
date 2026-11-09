package workbench

import (
	"fmt"
	"slices"
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

// applyTerminalFace makes the terminal draw in the font and fallbacks as they stand.
// It reports whether the face changed.
func (state *State) applyTerminalFace() bool {
	return state.setTerminalFontFace(state.terminalFace())
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
// offers if that has not been read.
func (state *State) openPicker(kind fontPickerKind) {
	state.modal = modalFontPicker
	state.fonts.kind = kind
	state.fonts.search = text.Editor{}
	state.fonts.built = false
	list := state.pickerList()
	if list.installed != nil || list.loading {
		return
	}
	list.loading = true
	state.listFonts(state.pickerFamilies(), func(s *State, families []string) {
		list.loading = false
		if families == nil {
			families = []string{}
		}
		list.installed = families
		s.fonts.built = false
		if kind == pickInterfaceFont {
			s.reconcileInterfaceFont()
			return
		}
		s.applyTerminalFace()
	})
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
		changed = state.setInterfaceFont(entry.family)
	default:
		changed = state.useTerminalFont(entry)
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

// useTerminalFont switches the terminal to a font. It reports whether anything changed.
func (state *State) useTerminalFont(entry fontEntry) bool {
	fonts := &state.fonts
	family, classic := entry.family, false
	if family == "" {
		classic = entry.face == gfx.MonoClassic
	} else if gfx.MonoFamily(family) == gfx.MonoSystem {
		return false
	}
	changed := fonts.family != family || fonts.classic != classic
	fonts.family, fonts.classic = family, classic
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
	case id.Scope() == hitFontPickerItemList:
		state.refreshFontPicker()
		state.selectFont(item(id))
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

func (state *State) paintFontPicker(canvas *gfx.Canvas, viewport gfx.Rect) {
	state.refreshFontPicker()
	if state.confirmFontOnDoubleClick(canvas) {
		return
	}
	m, pal, fonts := state.messages.Tools, state.pal, &state.fonts
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitFontPickerDismiss, CloseID: hitFontPickerClose, InitialFocusID: hitFontPickerSearch,
		ContentSize: gfx.Size{Width: 560}, FitBody: true, BodyHeight: 366,
	})
	title := m.FontPickerTitle
	switch fonts.kind {
	case pickFallbackFont:
		title = m.FontFallbackPickerTitle
	case pickInterfaceFont:
		title = state.messages.SettingsInterfaceFont
	}
	layout := dialog.Begin(title, "")
	rows := len(fonts.filtered)
	var search, list ui.Box
	// The list keeps room for all its rows, so the sample under it stays where it is;
	// what the list lacks is said under the rows it has.
	f := state.fontPickerLayout.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() {
		search = f.Slot().Height(36)
		list = f.Column().Height(fontPickerListHeight).Shrink(0).MarginSides(10, 0, 0, 0).Children(func() {
			switch {
			case state.pickerList().loading:
				f.Text(m.FontPickerLoading).FontSize(metrics.FontSizeM).Color(pal.muted).LineHeight(24).TextCenter().MarginSides(10, 0, 0, 0)
			case len(fonts.filtered) == 0:
				f.Text(m.FontPickerEmpty).FontSize(metrics.FontSizeM).Color(pal.muted).LineHeight(24).TextCenter().MarginSides(10, 0, 0, 0)
			}
		})
		if len(fonts.filtered) > 0 {
			// The highlighted font, set in itself.
			entry := fonts.entries[fonts.filtered[fonts.selected]]
			if fonts.kind == pickInterfaceFont {
				state.paintInterfaceFontSample(f, entry)
			} else {
				face := state.fontSampleFace(entry)
				f.Column().Height(56).Shrink(0).PadSides(7, 11, 0, 11).Background(pal.terminal).Border(1, pal.line).Radius(radius.control).MarginSides(8, 0, 0, 0).Children(func() {
					f.Text(m.FontPickerSample).FontSize(metrics.FontSizeM).Color(pal.text).MonoFace(face).LineHeight(20).Height(20)
					f.Text(m.FontPickerSampleSymbols).FontSize(metrics.FontSizeM).Color(pal.muted).MonoFace(face).LineHeight(20).Height(20)
				})
			}
		}
	})
	f.End()

	area := components.BeginScrollArea(canvas, state.Theme(), list.Rect(), gfx.Size{Width: list.Rect().Width, Height: fonts.contentHeight()}).Offset(0, fonts.scroll).HoverScrollbars(false)
	geometry := area.BeginContent()
	fonts.scroll = geometry.OffsetY
	// Keep layout work bounded by the viewport while retaining logical row IDs.
	first := int(geometry.OffsetY / fontPickerRowStep)
	last := min(rows, first+fontPickerVisibleRows+1)
	content := geometry.Content
	content.Y += float32(first) * fontPickerRowStep
	content.Height = float32(last-first) * fontPickerRowStep
	f = state.fontPickerListLayout.Begin(canvas, state.Theme(), content)
	f.Column().FillWidth().Children(func() {
		for row := first; row < last; row++ {
			entry := fonts.entries[fonts.filtered[row]]
			selected := row == fonts.selected
			inUse := fonts.kind != pickFallbackFont && state.fontInUse(entry)
			color := pal.text
			line := f.Row().Height(30).Shrink(0).MarginSides(0, 0, 2, 0).PadSides(0, 12, 0, 10).Items(ui.Center).Radius(radius.item).
				HitWith(hitFontPickerItemList.At(row), entry.label, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected})
			if selected {
				line.Background(pal.accent)
				color = pal.onAccent
			} else {
				line.Hover(ui.Look{}.Background(pal.surfaceHot))
			}
			line.Children(func() {
				f.Text(entry.label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(color).LineHeight(20).Grow(1).MarginSides(0, 12, 0, 0)
				if inUse {
					f.Icon(lucideIcons[iconCheck]).Size(16, 16).Color(color)
				} else {
					f.Spacer().Size(16, 16).Grow(0)
				}
			})
		}
	})
	f.End()
	area.EndContent()

	value := fonts.search.String()
	focused := canvas.Focused() == hitFontPickerSearch
	components.PaintInput(canvas, state.Theme(), search.Rect(), components.InputSnapshot{
		ID: hitFontPickerSearch, Label: title, Value: value, Placeholder: m.FontPickerPlaceholder,
		Focused: focused, PlatformEditing: focused && value != "", NavigationKeys: true,
	})
	confirmDisabled := !state.canConfirmFont()
	dialog.Actions(layout,
		components.ButtonAction{ID: hitFontPickerCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitFontPickerConfirm, Label: m.FontPickerConfirm, Variant: components.ButtonDefault, Disabled: confirmDisabled},
	)
	dialog.End()
}
