package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

var hitNotePreviewText = tree.ID(notesHits, "preview-text")

func (v *noteViewState) clearPreviewSelection() {
	v.previewSelection = components.MarkdownSelection{}
	v.previewSelecting = false
}

func (s *State) notePreviewSelectionPointer(e ui.PointerEvent) ui.EventResult {
	return notePreviewSelectionPointerContent(e, s.notePreviewState())
}

func notePreviewSelectionPointerContent(e ui.PointerEvent, n *notesUI) ui.EventResult {
	v := &n.view
	if e.Kind == ui.PointerDown && e.Target == hitNotePreviewText && (e.Button == ui.PointerButtonPrimary || e.Button == ui.PointerButtonNone) {
		if offset, ok := n.rendered.OffsetAt(ui.Point{X: e.X, Y: e.Y}); ok {
			v.previewSelection = components.MarkdownSelection{Anchor: offset, Focus: offset}
			v.previewSelecting = true
			return ui.EventResult{Handled: true, Repaint: true, Focus: hitNotePreviewText}
		}
	}
	if v.previewSelecting {
		switch e.Kind {
		case ui.PointerMove, ui.PointerUp:
			if e.Kind == ui.PointerMove && !e.Captured {
				return ui.EventResult{}
			}
			if offset, ok := n.rendered.OffsetAt(ui.Point{X: e.X, Y: e.Y}); ok {
				v.previewSelection.Focus = offset
			}
			if e.Kind == ui.PointerUp {
				v.previewSelecting = false
			}
			return ui.EventResult{Handled: true, Repaint: true}
		case ui.PointerExit:
			v.previewSelecting = false
			return ui.EventResult{Handled: true, Repaint: true}
		case ui.PointerDown:
			v.previewSelecting = false
		}
	}
	return ui.EventResult{}
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
	chord, ok := shortcutChordFromEvent(e)
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
			v.previewSelection = components.MarkdownSelection{Focus: n.rendered.TextLen()}
		} else if value := n.rendered.SelectedText(v.previewSelection); value != "" && s.options.SetClipboardText != nil {
			if err := s.options.SetClipboardText(value); err != nil {
				n.error = err.Error()
			}
		}
	}
	return ui.EventResult{Handled: true, Repaint: true}
}
