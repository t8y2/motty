package workbench

import "github.com/vivaui/viva/gfx"

// stepperPointer gives a stepper's field the whole number on a press, so the
// first digit replaces it instead of placing a caret. The press is handled
// before the page's field sees it.
func (state *State) stepperPointer(event gfx.PointerEvent, category settingsCategory, target gfx.ID, begin func()) (gfx.EventResult, bool) {
	if state.modal != modalNone || state.activeSection != sectionSettings || state.settingsCategory != category {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerDown && event.Target == target &&
		(event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone) {
		begin()
		return gfx.EventResult{Focus: target, FocusVisible: true, Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

// stepperKey settles the number a stepper's field is editing. Enter commits,
// Escape discards what was typed, Tab commits before focus moves on, and a key
// aimed at another control settles the edit before that control sees it.
func (state *State) stepperKey(event gfx.KeyEvent, edit *numberStepperEdit, target gfx.ID, commit func()) (gfx.EventResult, bool) {
	if !edit.editing {
		return gfx.EventResult{}, false
	}
	if event.Target != target {
		commit()
		return gfx.EventResult{}, false
	}
	switch event.Key {
	case gfx.KeyEnter:
		commit()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case gfx.KeyEscape:
		edit.editing = false
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case gfx.KeyTab:
		commit()
	}
	return gfx.EventResult{}, false
}
