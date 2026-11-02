package workbench

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// The interface's own font: the family the window's text — menus, dialogs,
// settings, every label that is not a terminal — is shaped in. The platform
// draws it in its own interface font until the user names another family (one
// whose CJK and Latin glyphs read evenly together, say); "" is the platform's
// choice again. The size of it all is a setting of its own (see theme.go).

// interfaceFontHits is the place of the controls this file paints.
var interfaceFontHits = tree.Feature("interface-font")

var (
	hitInterfaceFontChoose   = tree.ID(interfaceFontHits, "choose")
	hitInterfaceFontDismiss  = tree.ID(interfaceFontHits, "dismiss")
	hitInterfaceFontClose    = tree.ID(interfaceFontHits, "close")
	hitInterfaceFontSearch   = tree.ID(interfaceFontHits, "search")
	hitInterfaceFontItemList = tree.Sub(interfaceFontHits, tree.Name("item"))
)

const interfaceFontVisibleRows = 9

// interfaceFontEntry is one choice: the platform's own interface font (family
// empty) or an installed family.
type interfaceFontEntry struct {
	label  string
	family string
}

type interfaceFontUI struct {
	// family is the chosen family, "" for the platform's own interface font.
	// It is what the user chose, kept as it is whatever this computer has
	// installed, so that a state carried to another computer keeps the choice.
	family string
	// installed is what the system listed; nil until it has been read.
	installed []string
	loading   bool

	search   text.Editor
	raw      string // the search text the filter was built for
	entries  []interfaceFontEntry
	filtered []int32 // indices into entries
	selected int     // index into filtered
	top      int     // first filtered row shown
	built    bool

	// text is the description the settings row shows; textFor is what it was
	// made for.
	text, textFor string
	// probed says the platform has been asked whether it can follow a family at
	// all; supported is its answer.
	probed    bool
	supported bool
}

// interfaceFontAvailable reports whether this build can name a family at all:
// the platform's engine may not follow one (a browser build, say). The setter is
// asked once, with the family in use, which is also the applying of it.
func (state *State) interfaceFontAvailable() bool {
	if !state.uiFont.probed {
		state.uiFont.probed = true
		state.uiFont.supported = state.options.SetInterfaceFont != nil && state.options.SetInterfaceFont(state.uiFont.family)
	}
	return state.uiFont.supported
}

// interfaceFontName is the name of the font in use, in the current language.
func (state *State) interfaceFontName() string {
	if state.uiFont.family != "" {
		return state.uiFont.family
	}
	return state.messages.SettingsInterfaceFontDefault
}

// interfaceFontDescription is the settings row's explanation: what the font is
// now. It is made again only when that changes.
func (state *State) interfaceFontDescription() string {
	name := state.interfaceFontName()
	if state.uiFont.textFor != name || state.uiFont.text == "" {
		state.uiFont.textFor = name
		state.uiFont.text = fmt.Sprintf(state.messages.SettingsInterfaceFontCurrentFmt, name)
	}
	return state.uiFont.text
}

// setInterfaceFont names the family the window's text is drawn in, or "" for
// the platform's own. It reports whether anything changed.
func (state *State) setInterfaceFont(family string) bool {
	family = strings.TrimSpace(family)
	if family == state.uiFont.family {
		return false
	}
	state.uiFont.family = family
	state.uiFont.text = ""
	if state.options.SetInterfaceFont != nil {
		state.options.SetInterfaceFont(family)
	}
	return true
}

// listInterfaceFonts reads the installed families off the window thread and
// delivers the list to fn on it.
func (state *State) listInterfaceFonts(fn func(*State, []string)) {
	list := state.options.FontFamilies
	if list == nil || !state.aiPost(func(*State) {}) {
		return
	}
	go func() {
		families := list()
		state.aiPost(func(s *State) { fn(s, families) })
	}()
}

// validateInterfaceFont checks, once the list can be read, that the saved
// family is still installed, and falls back to the platform's own when it is
// not. The choice stays saved: a copy carried to another computer keeps it.
func (state *State) validateInterfaceFont() {
	if state.uiFont.family == "" {
		return
	}
	family := state.uiFont.family
	state.listInterfaceFonts(func(s *State, families []string) {
		s.uiFont.installed = families
		s.uiFont.built = false
		if s.uiFont.family != family {
			return // the user chose another while the list was being read
		}
		if !slices.Contains(families, family) {
			s.setInterfaceFont("")
			s.status = fmt.Sprintf(s.messages.Tools.FontMissingFmt, family)
			s.save()
		}
	})
}

// openInterfaceFontPicker opens the choice of the interface's font.
func (state *State) openInterfaceFontPicker() {
	state.modal = modalInterfaceFont
	state.uiFont.search = text.Editor{}
	state.uiFont.built = false
	if state.uiFont.installed == nil && !state.uiFont.loading {
		state.uiFont.loading = true
		state.listInterfaceFonts(func(s *State, families []string) {
			s.uiFont.loading = false
			if families == nil {
				families = []string{}
			}
			s.uiFont.installed = families
			s.uiFont.built = false
		})
	}
}

// refreshInterfaceFontPicker builds the list the picker shows for its search
// text.
func (state *State) refreshInterfaceFontPicker() {
	fonts := &state.uiFont
	raw := fonts.search.String()
	if fonts.built && raw == fonts.raw {
		return
	}
	first := !fonts.built
	fonts.built, fonts.raw = true, raw
	fonts.entries = fonts.entries[:0]
	fonts.entries = append(fonts.entries, interfaceFontEntry{label: state.messages.SettingsInterfaceFontDefault})
	for _, name := range fonts.installed {
		fonts.entries = append(fonts.entries, interfaceFontEntry{label: name, family: name})
	}
	query := strings.ToLower(strings.TrimSpace(raw))
	fonts.filtered = fonts.filtered[:0]
	current := -1
	for index, entry := range fonts.entries {
		if query != "" && !strings.Contains(strings.ToLower(entry.label), query) {
			continue
		}
		if entry.family == fonts.family {
			current = len(fonts.filtered)
		}
		fonts.filtered = append(fonts.filtered, int32(index))
	}
	fonts.selected, fonts.top = 0, 0
	if first && current >= 0 {
		fonts.selected = current
		fonts.top = max(min(current-interfaceFontVisibleRows/2, len(fonts.filtered)-interfaceFontVisibleRows), 0)
	}
}

// moveInterfaceFontSelection moves the highlighted row and keeps it in view.
func (state *State) moveInterfaceFontSelection(delta int) {
	fonts := &state.uiFont
	state.refreshInterfaceFontPicker()
	if len(fonts.filtered) == 0 {
		return
	}
	fonts.selected = min(max(fonts.selected+delta, 0), len(fonts.filtered)-1)
	if fonts.selected < fonts.top {
		fonts.top = fonts.selected
	}
	if fonts.selected >= fonts.top+interfaceFontVisibleRows {
		fonts.top = fonts.selected - interfaceFontVisibleRows + 1
	}
}

func (state *State) scrollInterfaceFontPicker(event ui.ScrollEvent) ui.EventResult {
	fonts := &state.uiFont
	state.refreshInterfaceFontPicker()
	before := fonts.top
	step := 1
	if event.DeltaY < 0 {
		step = -1
	}
	fonts.top = min(max(fonts.top+step, 0), max(len(fonts.filtered)-interfaceFontVisibleRows, 0))
	return ui.EventResult{Handled: true, Repaint: fonts.top != before}
}

// chooseInterfaceFont applies the highlighted (or clicked) row and closes the
// picker.
func (state *State) chooseInterfaceFont(row int) {
	fonts := &state.uiFont
	state.refreshInterfaceFontPicker()
	if row < 0 || row >= len(fonts.filtered) {
		return
	}
	entry := fonts.entries[fonts.filtered[row]]
	state.closeModal()
	if state.setInterfaceFont(entry.family) {
		state.save()
	}
}

func (state *State) clickInterfaceFontPicker(id ui.ID) {
	switch {
	case id == hitInterfaceFontDismiss || id == hitInterfaceFontClose:
		state.closeModal()
	case id.Scope() == hitInterfaceFontItemList:
		state.refreshInterfaceFontPicker()
		state.chooseInterfaceFont(state.uiFont.top + item(id))
	}
}

// interfaceFontPickerKey handles the keys of the picker. It reports whether it
// took the key.
func (state *State) interfaceFontPickerKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalInterfaceFont {
		return ui.EventResult{}, false
	}
	handled := ui.EventResult{Handled: true, Repaint: true}
	switch event.Key {
	case ui.KeyEscape:
		state.closeModal()
		return handled, true
	case ui.KeyUp:
		state.moveInterfaceFontSelection(-1)
		return handled, true
	case ui.KeyDown:
		state.moveInterfaceFontSelection(1)
		return handled, true
	case ui.KeyPageUp:
		state.moveInterfaceFontSelection(-interfaceFontVisibleRows)
		return handled, true
	case ui.KeyPageDown:
		state.moveInterfaceFontSelection(interfaceFontVisibleRows)
		return handled, true
	case ui.KeyEnter:
		if !event.Repeat {
			state.chooseInterfaceFont(state.uiFont.selected)
		}
		return handled, true
	}
	return ui.EventResult{}, false
}

func (state *State) paintInterfaceFontPicker(canvas *ui.Canvas, viewport ui.Rect) {
	state.refreshInterfaceFontPicker()
	m, pal, fonts := state.messages, state.pal, &state.uiFont
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitInterfaceFontDismiss, CloseID: hitInterfaceFontClose, InitialFocusID: hitInterfaceFontSearch,
		ContentSize: ui.Size{Width: 560, Height: 502},
	})
	layout := dialog.Begin(m.SettingsInterfaceFont, "")
	rows := max(min(len(fonts.filtered)-fonts.top, interfaceFontVisibleRows), 0)
	var search flex.Box
	f := state.fontPickerLayout.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() {
		search = f.Slot().Height(36)
		f.Column().Height(interfaceFontVisibleRows*32).Shrink(0).MarginSides(10, 0, 0, 0).Children(func() {
			for visible := 0; visible < rows; visible++ {
				row := fonts.top + visible
				entry := fonts.entries[fonts.filtered[row]]
				selected := row == fonts.selected
				inUse := entry.family == fonts.family
				color := pal.text
				line := f.Row().Height(30).Shrink(0).MarginSides(0, 0, 2, 0).PadSides(0, 12, 0, 10).Items(flex.Center).Radius(radius.item).
					HitWith(hitInterfaceFontItemList.At(visible), entry.label, ui.HitOptions{Role: ui.RoleButton, Selected: selected})
				if selected {
					line.Background(pal.accent)
					color = pal.onAccent
				} else {
					line.HoverBackground(pal.surfaceHot)
				}
				line.Children(func() {
					f.Text(entry.label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(color).LineHeight(20).Grow(1).MarginSides(0, 12, 0, 0)
					if inUse {
						f.Icon(lucideIcons[iconCheck]).Size(16, 16).Color(color)
					} else {
						f.Spacer().Size(16, 16).Grow(0)
					}
				})
			}
			switch {
			case fonts.loading:
				f.Text(m.Tools.FontPickerLoading).FontSize(metrics.FontSizeM).Color(pal.muted).LineHeight(24).TextCenter().MarginSides(10, 0, 0, 0)
			case len(fonts.filtered) == 0:
				f.Text(m.Tools.FontPickerEmpty).FontSize(metrics.FontSizeM).Color(pal.muted).LineHeight(24).TextCenter().MarginSides(10, 0, 0, 0)
			}
		})
	})
	f.End()

	value := fonts.search.String()
	focused := canvas.Focused() == hitInterfaceFontSearch
	components.PaintInput(canvas, state.Theme(), search.Rect(), components.InputSnapshot{
		ID: hitInterfaceFontSearch, Label: m.SettingsInterfaceFont, Value: value, Placeholder: m.Tools.FontPickerPlaceholder,
		Focused: focused, PlatformEditing: focused && value != "",
	})
	dialog.End()
}
