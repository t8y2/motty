package workbench

import (
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// hitMenuScrollBar is the scrollbar of whichever popup menu is too tall for the
// window.
var hitMenuScrollBar = tree.ID(shellHits, "menu-scroll-bar")

// popupMenus is the scroll position of the popup menu that is open. Only one is
// at a time (each is modal: a press outside it closes it), so they share a
// position that is reset whenever a different menu takes it or none is painted.
type popupMenus struct {
	scroll components.MenuScroll
	// typeahead is the typed prefix that highlights a row by name. It starts
	// over with the menu's scroll position.
	typeahead components.MenuTypeahead
	// owner is the dismiss identity of the menu the position belongs to.
	owner gfx.ID
	// surface is that menu's rectangle as last painted, where a wheel scrolls it.
	surface gfx.Rect
	// painted says some menu claimed the position in the paint under way.
	painted bool
}

// beginMenus starts the painting of the popup menus of one frame.
func (state *State) beginMenus() {
	state.popups.painted = false
	// A file panel asks for its More menu while it is painted. Asking again each
	// frame is what keeps the request honest: one that nobody renewed is stale.
	state.sftpHeaderMenu = sftpHeaderMenuRequest{}
}

// endMenus ends it: a position no menu claimed is forgotten, so the next menu to
// open starts at its top.
func (state *State) endMenus() {
	popups := &state.popups
	if !popups.painted {
		*popups = popupMenus{}
	}
}

// menuScrollFor is the scroll position of the menu that id dismisses, handed to
// its options. A menu that is not the one that had it starts at the top.
func (state *State) menuScrollFor(id gfx.ID) *components.MenuScroll {
	popups := &state.popups
	popups.painted = true
	if popups.owner != id {
		*popups = popupMenus{owner: id, painted: true}
		popups.scroll.BarID = hitMenuScrollBar
	}
	return &popups.scroll
}

// menuPlaced records where the menu was painted.
func (state *State) menuPlaced(bounds gfx.Rect) { state.popups.surface = bounds }

// selectScroll is the scroll position of an open select popup. A closed select
// has none, so its trigger keeps the keyboard instead of the list.
func (state *State) selectScroll(open bool, dismiss gfx.ID) *components.MenuScroll {
	if !open || dismiss.IsZero() {
		return nil
	}
	return state.menuScrollFor(dismiss)
}

// menuWheel scrolls a menu when the pointer is over it. An open menu is modal,
// as Base UI's is, so a wheel gesture outside its surface is consumed rather
// than scrolling the page behind it; only the menu's own list moves.
func (state *State) menuWheel(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	popups := &state.popups
	if popups.owner.IsZero() {
		return gfx.EventResult{}, false
	}
	if !pointInRect(event.X, event.Y, popups.surface) {
		return gfx.EventResult{Handled: true}, true
	}
	return gfx.EventResult{Handled: true, Repaint: popups.scroll.Wheel(event)}, true
}

// openMenuScroll is the scroll position of the menu that has the keyboard and the
// scrollbar: the popup that is open, or the list of models of the assistant's
// chip. It is nil when neither is.
func (state *State) openMenuScroll() *components.MenuScroll {
	if !state.popups.owner.IsZero() {
		return &state.popups.scroll
	}
	if state.ai.panel.modelMenu {
		return &state.ai.panel.modelList
	}
	return nil
}

// menuPointer drags the thumb of the open menu's scrollbar. The press and the
// release are taken so they do not click: a press elsewhere dismisses the menu.
func (state *State) menuPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	scroll := state.openMenuScroll()
	if scroll == nil || event.Target != scroll.BarID && !scroll.Dragging() {
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: scroll.Drag(event)}, true
}

// menuKey moves the keyboard through the open menu with the arrow, Home, End and
// Page keys, scrolling the item it reaches into view.
func (state *State) menuKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.sftpHeaderMenuOwns(state.popups.owner) {
		if w := state.activeSFTPWindow(); w != nil {
			if w.view == nil || w.view.dialog != sftpDialogNone || w.view.editPath != "" {
				return gfx.EventResult{}, false
			}
		} else {
			session, ok := state.sessionByIDPointer(state.activeSessionID)
			if !ok || !state.sftpHeaderMenuAvailable(session.files) {
				return gfx.EventResult{}, false
			}
		}
	}
	scroll := state.openMenuScroll()
	if scroll == nil || event.Release {
		return gfx.EventResult{}, false
	}
	// The assistant's menu has a list of providers beside the models: the keys
	// belong to the models only when they are the ones that have the keyboard.
	typeahead := &state.popups.typeahead
	if scroll != &state.popups.scroll {
		if !event.Target.IsZero() && event.Target.Scope() != hitAIModels {
			return gfx.EventResult{}, false
		}
		typeahead = &state.ai.panel.modelTypeahead
	}
	// Tab leaves a dropdown or a select and continues through the page, as Base
	// UI does; a context menu keeps Tab inside itself (TabCloses is false).
	if event.Key == gfx.KeyTab && scroll.TabCloses {
		dismiss := state.popups.owner
		if scroll != &state.popups.scroll {
			dismiss = hitAIModelDismiss
		}
		if !dismiss.IsZero() {
			state.Click(dismiss)
			return gfx.EventResult{Handled: true, Repaint: true, ContinueTraversal: true}, true
		}
	}
	if event.Text != "" && event.Modifiers&(gfx.ModifierControl|gfx.ModifierAlt|gfx.ModifierMeta) == 0 {
		now := time.Now()
		// Space continues a running query rather than activating the focused
		// row, even when the query then matches nothing.
		continuesQuery := event.Text == " " && typeahead.Typing(now)
		if id, ok := typeahead.Type(event.Text, now, scroll, event.Target); ok {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: id, FocusVisible: true}, true
		}
		if continuesQuery {
			return gfx.EventResult{Handled: true}, true
		}
	}
	id, ok := scroll.Navigate(event.Key, event.Target, true)
	if !ok {
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: id, FocusVisible: true}, true
}
