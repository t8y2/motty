package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

var hitNotePreviewText = tree.ID(notesHits, "preview-text")

func (v *noteViewState) clearPreviewSelection() {
	v.previewSelect.Clear()
}

func (s *State) notePreviewSelectionPointer(e gfx.PointerEvent) gfx.EventResult {
	return notePreviewSelectionPointerContent(e, s.notePreviewState())
}

func notePreviewSelectionPointerContent(e gfx.PointerEvent, n *notesUI) gfx.EventResult {
	v := &n.view
	if e.Kind == gfx.PointerDown {
		// A press ends a drag whose release was never seen; only one on the
		// preview text begins another.
		v.previewSelect.EndDrag()
		if e.Target != hitNotePreviewText {
			return gfx.EventResult{}
		}
	} else if !v.previewSelect.Dragging() {
		return gfx.EventResult{}
	}
	result := v.previewSelect.Pointer(e, func(point gfx.Point) (components.TextPoint, bool) {
		offset, ok := n.rendered.OffsetAt(point)
		return components.TextPoint{Offset: offset}, ok
	})
	if !result.Handled {
		return gfx.EventResult{}
	}
	outcome := gfx.EventResult{Handled: true, Repaint: result.Changed}
	if e.Kind == gfx.PointerDown {
		outcome.Focus = hitNotePreviewText
	}
	return outcome
}

func (s *State) notePreviewShortcut(e gfx.KeyEvent) gfx.EventResult {
	n := s.notePreviewState()
	v := &n.view
	if !s.notesVisible() || e.Target != hitNotePreviewText || v.menu != "" || v.popup != "" || v.drawer || s.notes.confirm != "" {
		return gfx.EventResult{}
	}
	return s.notePreviewShortcutContent(e, n)
}

func (s *State) notePreviewShortcutContent(e gfx.KeyEvent, n *notesUI) gfx.EventResult {
	v := &n.view
	chord, ok := keymap.FromEvent(e)
	if !ok {
		return gfx.EventResult{}
	}
	copy := s.shortcuts.actionFor(chord) == shortcutCopy
	all := strings.EqualFold(e.Text, "a") && (e.Modifiers == gfx.ModifierMeta || e.Modifiers == gfx.ModifierControl)
	if !copy && !all {
		return gfx.EventResult{}
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
	return gfx.EventResult{Handled: true, Repaint: true}
}
