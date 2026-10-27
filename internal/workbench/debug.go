//go:build dev

package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

const (
	// debugPreviewActions is the number of actions in the button group the Debug
	// page shows.
	debugPreviewActions = 3
	// debugSelectOptions is the number of choices in its select.
	debugSelectOptions = 3
	// debugSwatches is the number of colors in its palette, the disabled last
	// choice included.
	debugSwatches = 4
)

// The gallery's place in the sidebar and its controls. A release build has none
// of them: the page exists only with the dev tag (see debug_release.go).
var (
	debugHits              = tree.Feature("debug")
	hitDebug               = tree.ID(shellHits, "debug")
	hitDebugSelect         = tree.ID(debugHits, "select")
	hitDebugSelectDismiss  = tree.ID(debugHits, "select-dismiss")
	hitDebugActionList     = tree.Sub(debugHits, tree.Name("action"))
	hitDebugSelectItemList = tree.Sub(debugHits, tree.Name("select-item"))
	hitDebugSwatchList     = tree.Sub(debugHits, tree.Name("swatch"))
)

// devState is the gallery's own state: the layout of its previews, the button
// group action pressed last, the select's trigger and popup, and the color
// chosen in the palette. A release build keeps none of it.
type devState struct {
	layout flex.View
	action int
	// selectRect is where the page reserved the select trigger. The trigger and
	// its popup are painted after the page, outside the workspace's clip.
	selectRect   ui.Rect
	selectOpen   bool
	selectOption int
	// swatch is the color chosen in the palette; it indexes debugSwatchColors.
	swatch int
}

// devDeclareNavigation puts the gallery in the sidebar, above Settings.
func (state *State) devDeclareNavigation(f *flex.Frame) {
	state.navigationItem(f, hitDebug, iconBug, state.messages.NavDebug, state.activeSection == sectionDebug, 4)
}

// devClick opens the gallery from the sidebar and handles its controls.
func (state *State) devClick(id ui.ID) bool {
	if id == hitDebug {
		state.activeSection = sectionDebug
		state.save()
		return true
	}
	if tree.Within(id.Scope(), debugHits) {
		return state.clickDebug(id)
	}
	return false
}

// devBeginPaint forgets what the last paint occupied and closes the page's
// popup once the page is no longer on show.
func (state *State) devBeginPaint() {
	state.dev.selectRect = ui.Rect{}
	if state.activeSection != sectionDebug {
		state.dev.selectOpen = false
	}
}

// devPaintPage paints the gallery and reports whether it was the page on show.
func (state *State) devPaintPage(canvas *ui.Canvas, bounds ui.Rect) bool {
	if state.activeSection != sectionDebug {
		return false
	}
	state.paintDebugWorkspace(canvas, bounds)
	return true
}

// devPaintOverlay paints the page's select inside the frame's menu pass, so its
// popup can leave the page and keeps the menu state its keyboard handling needs.
func (state *State) devPaintOverlay(canvas *ui.Canvas) {
	if state.activeSection == sectionDebug {
		state.paintDebugSelect(canvas)
	}
}

// devAddPaletteCommands lists the gallery on the command palette.
func (state *State) devAddPaletteCommands(palette *commandPalette) {
	palette.add(state.messages.NavDebug, hitDebug)
}

// devWorkspaceName names the gallery as a workspace to come back to.
func (state *State) devWorkspaceName() (string, bool) {
	if state.activeSection == sectionDebug {
		return "debug", true
	}
	return "", false
}

// devRestoreWorkspace opens the gallery again when it was the workspace saved.
func (state *State) devRestoreWorkspace(workspace string) bool {
	if workspace != "debug" {
		return false
	}
	state.activeSection = sectionDebug
	return true
}

// devSelectTypeahead offers the gallery select's choices to a typed prefix.
func (state *State) devSelectTypeahead(target ui.ID) ([]components.MenuTypeaheadItem, func(ui.ID) bool, bool) {
	if state.modal != modalNone || state.activeSection != sectionDebug || target != hitDebugSelect || state.dev.selectOpen {
		return nil, nil, false
	}
	labels := state.debugSelectLabels()
	items := make([]components.MenuTypeaheadItem, 0, len(labels))
	for index, label := range labels {
		items = append(items, components.MenuTypeaheadItem{ID: hitDebugSelectItemList.At(index), Label: label})
	}
	return items, func(id ui.ID) bool {
		index, ok := id.In(hitDebugSelectItemList)
		if !ok || index < 0 || index >= len(labels) {
			return false
		}
		state.dev.selectOption = index
		return true
	}, true
}

// debugActionLabels names the actions of the preview group in the active
// language. The array keeps painting allocation-free.
func (state *State) debugActionLabels() [debugPreviewActions]string {
	messages := state.messages
	return [debugPreviewActions]string{messages.DebugAlignLeft, messages.DebugAlignCenter, messages.DebugAlignRight}
}

// debugButtonOptions builds the preview group's stable actions. Viva owns the
// control's look and input; this page only says which actions it holds.
func (state *State) debugButtonOptions() [debugPreviewActions]components.ButtonGroupOption {
	labels := state.debugActionLabels()
	var options [debugPreviewActions]components.ButtonGroupOption
	for index, label := range labels {
		options[index] = components.ButtonGroupOption{
			ID: hitDebugActionList.At(index), Label: label, Variant: components.ButtonOutline,
		}
	}
	return options
}

// debugSelectLabels names the choices of the preview select in the active
// language.
func (state *State) debugSelectLabels() [debugSelectOptions]string {
	messages := state.messages
	return [debugSelectOptions]string{
		messages.DebugEnvironmentDevelopment, messages.DebugEnvironmentStaging, messages.DebugEnvironmentProduction,
	}
}

// debugSwatchLabels names the palette's choices in the active language, in the
// order debugSwatchColors draws them.
func (state *State) debugSwatchLabels() [debugSwatches]string {
	messages := state.messages
	return [debugSwatches]string{
		messages.ColorNone, messages.ColorGreen, messages.ColorBlue, messages.ColorRed,
	}
}

// debugSwatchOptions builds the palette's stable choices for keyboard
// navigation. The last color is disabled, so arrows and Tab skip it.
func (state *State) debugSwatchOptions() [debugSwatches]components.RadioOption {
	labels := state.debugSwatchLabels()
	var options [debugSwatches]components.RadioOption
	for index, label := range labels {
		options[index] = components.RadioOption{
			ID: hitDebugSwatchList.At(index), Label: label, Disabled: index == debugSwatches-1,
		}
	}
	return options
}

// devSwatchKey moves through the gallery's palette with the arrow keys, as a
// radio group does. Tab traversal and the hits themselves are Viva's.
func (state *State) devSwatchKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalNone || state.activeSection != sectionDebug {
		return ui.EventResult{}, false
	}
	if _, ok := event.Target.In(hitDebugSwatchList); !ok {
		return ui.EventResult{}, false
	}
	options := state.debugSwatchOptions()
	next, handled := components.RadioGroupKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	if !handled {
		return ui.EventResult{}, false
	}
	index, ok := next.In(hitDebugSwatchList)
	if !ok || index < 0 || index >= debugSwatches {
		return ui.EventResult{}, false
	}
	changed := state.dev.swatch != index
	state.dev.swatch = index
	return ui.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: changed}, true
}

// clickDebug handles the Debug page's preview controls. The group's hits and the
// select's popup are Viva's; the page only keeps the values and the open state.
func (state *State) clickDebug(id ui.ID) bool {
	if state.modal != modalNone || state.activeSection != sectionDebug {
		return false
	}
	switch {
	case id == hitDebugSelect:
		state.dev.selectOpen = !state.dev.selectOpen
		return true
	case id == hitDebugSelectDismiss:
		state.dev.selectOpen = false
		return true
	}
	if index, ok := id.In(hitDebugActionList); ok && index < debugPreviewActions {
		state.dev.action = index
		return true
	}
	if index, ok := id.In(hitDebugSelectItemList); ok && index < debugSelectOptions {
		state.dev.selectOption = index
		state.dev.selectOpen = false
		return true
	}
	if index, ok := id.In(hitDebugSwatchList); ok && index < debugSwatches-1 {
		state.dev.swatch = index
		return true
	}
	return false
}
