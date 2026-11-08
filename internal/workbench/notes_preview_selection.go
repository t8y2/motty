package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

var hitNotePreviewText = tree.ID(notesHits, "preview-text")

func (v *noteViewState) clearPreviewSelection() {
	v.previewSelect.Clear()
}

func (s *State) notePreviewSelectionPointer(e ui.PointerEvent) ui.EventResult {
	return notePreviewSelectionPointerContent(e, s.notePreviewState())
}

func notePreviewSelectionPointerContent(e ui.PointerEvent, n *notesUI) ui.EventResult {
	v := &n.view
	if e.Kind == ui.PointerDown {
		// A press ends a drag whose release was never seen; only one on the
		// preview text begins another.
		v.previewSelect.EndDrag()
		if e.Target != hitNotePreviewText {
			return ui.EventResult{}
		}
	} else if !v.previewSelect.Dragging() {
		return ui.EventResult{}
	}
	result := v.previewSelect.Pointer(e, func(point ui.Point) (components.TextPoint, bool) {
		offset, ok := n.rendered.OffsetAt(point)
		return components.TextPoint{Offset: offset}, ok
	})
	if !result.Handled {
		return ui.EventResult{}
	}
	outcome := ui.EventResult{Handled: true, Repaint: result.Changed}
	if e.Kind == ui.PointerDown {
		outcome.Focus = hitNotePreviewText
	}
	return outcome
}

func (s *State) notePreviewShortcut(e ui.KeyEvent) ui.EventResult {
	n := s.notePreviewState()
	v := &n.view
	if !s.notesVisible() || e.Target != hitNotePreviewText || v.menu != "" || v.popup != "" || v.drawer || s.notes.confirm != "" {
		return ui.EventResult{}
	}
	return s.notePreviewShortcutContent(e, n)
}

func (s *State) notePreviewShortcutContent(e ui.KeyEvent, n *notesUI) ui.EventResult {
	v := &n.view
	chord, ok := keymap.FromEvent(e)
	if !ok {
		return ui.EventResult{}
	}
	copy := s.shortcuts.actionFor(chord) == shortcutCopy
	all := strings.EqualFold(e.Text, "a") && (e.Modifiers == ui.ModifierMeta || e.Modifiers == ui.ModifierControl)
	if !copy && !all {
		return ui.EventResult{}
	}
	if !e.Release && !e.Repeat {
		if all {
			v.previewSelect.Select(components.TextPoint{}, components.TextPoint{Offset: n.rendered.TextLen()})
		} else if value := n.rendered.SelectedText(v.previewSelect.In(0)); value != "" && s.options.SetClipboardText != nil {
			if err := s.options.SetClipboardText(value); err != nil {
				n.error = err.Error()
			}
		}
	}
	return ui.EventResult{Handled: true, Repaint: true}
}
