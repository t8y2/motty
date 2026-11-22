package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// localShellPickerHits is the place of the controls this file paints.
var localShellPickerHits = tree.Feature("local-shell-picker")

var (
	hitLocalShellPickerDismiss = tree.ID(localShellPickerHits, "local-shell-picker-dismiss")
	hitLocalShellPickerItems   = tree.Sub(localShellPickerHits, tree.Name("local-shell-picker-item"))
	hitLocalShellPickerSetup   = tree.Sub(localShellPickerHits, tree.Name("local-shell-picker-setup"))
)

// localShellPicker is the menu that opens a local terminal with one chosen
// shell, instead of the default one of the settings. Closed, it costs nothing.
type localShellPicker struct {
	open bool
	at   gfx.Point
	// inPane is set when it was opened from the new-tab page: the shell then
	// starts in that empty pane, like the page's own Local Terminal row.
	inPane bool
}

// openLocalShellPicker opens the menu at at, and reports whether this computer
// offers a choice of shells at all. The shells are looked up again each time,
// since one may have been installed since; the WSL distributions follow when
// wsl.exe has named them.
func (state *State) openLocalShellPicker(at gfx.Point, inPane bool) bool {
	if !localShellSettingVisible() || state.modal != modalNone {
		return false
	}
	state.refreshLocalShells()
	state.localShellPicker = localShellPicker{open: true, at: at, inPane: inPane}
	return true
}

// openLocalShellPickerOn is called while painting a Local Terminal entry: when
// this paint is the event pass of a secondary press on it, the menu opens there.
func (state *State) openLocalShellPickerOn(canvas *gfx.Canvas, id gfx.ID, inPane bool) {
	event, ok := canvas.PointerOn(id)
	if !ok || event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary {
		return
	}
	if state.openLocalShellPicker(gfx.Point{X: event.X, Y: event.Y}, inPane) {
		canvas.Consume()
	}
}

func (state *State) declareLocalShellPicker(c ui.C) {
	picker := &state.localShellPicker
	if !picker.open {
		return
	}
	if state.modal != modalNone || !localShellSettingVisible() {
		picker.open = false
		return
	}
	state.adoptWSLDistros()
	m, menuState := state.messages, &state.localShellMenu
	selected := state.localShellSelected()
	icon := lucideIcons[iconTerminal]
	var rows menuRows
	for index, choice := range menuState.choices {
		if state.shellHidden(choice) {
			continue // hidden here, still listed in the setting itself
		}
		item := components.ContextMenuItem{ID: hitLocalShellPickerItems.At(index), Label: choice.Name, Icon: icon, TrailingWidth: 30}
		if index == selected {
			item.Shortcut = m.LocalShellDefault
		}
		rows.add(item)
		setup := hitLocalShellPickerSetup.At(index)
		rows.trailing(30, func(canvas *gfx.Canvas, slot gfx.Rect, _ gfx.Theme) {
			state.paintLocalStartupGear(canvas, setup, slot)
		})
	}
	if len(menuState.choices) == 0 {
		rows.add(components.ContextMenuItem{ID: hitLocalShellPickerItems.At(0), Label: m.SettingsLocalShellEmpty, Icon: icon, Disabled: true})
	}
	state.declareRowsMenu(c, &picker.open, picker.at, hitLocalShellPickerDismiss, &rows, state.chooseLocalShell)
}

// chooseLocalShell runs a choice of the menu, which closed with it.
func (state *State) chooseLocalShell(id gfx.ID) {
	picker := state.localShellPicker
	index, ok := id.In(hitLocalShellPickerItems)
	choices := state.localShellMenu.choices
	if !ok || index < 0 || index >= len(choices) {
		return
	}
	if picker.inPane {
		state.pendingWorkspace = state.deck.ActiveWorkspace()
		state.pendingPane = state.deck.ActivePane(state.pendingWorkspace)
	} else {
		state.pendingWorkspace, state.pendingPane = 0, 0
	}
	state.openLocalTerminalWith(choices[index])
}
