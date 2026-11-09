package workbench

import (
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var noteExitHits = tree.Feature("notes-exit")
var (
	hitNoteExitRetry   = tree.ID(noteExitHits, "retry")
	hitNoteExitCancel  = tree.ID(noteExitHits, "cancel")
	hitNoteExitDiscard = tree.ID(noteExitHits, "discard")
)

type noteExitRequest struct{ allow func() }

func (s *State) confirmNoteClose(allow func()) bool {
	if s.noteExit != nil {
		return true
	}
	if !s.hasUnsavedNotes() {
		return false
	}
	s.save()
	if !s.hasUnsavedNotes() {
		return false
	}
	s.noteExit = &noteExitRequest{allow: allow}
	if s.options.Raise != nil {
		s.options.Raise()
	}
	s.invalidateNoteViews()
	return true
}
func (s *State) paintNoteExit(c *gfx.Canvas, b gfx.Rect) {
	if s.noteExit == nil {
		return
	}
	dialog := components.BeginDialog(c, s.Theme(), b, components.DialogOptions{Layer: dialogLayer + 10, Scope: hitNoteExitCancel, CloseID: hitNoteExitCancel, InitialFocusID: hitNoteExitCancel, ContentSize: gfx.Size{Width: 480}, FitBody: true, BodyHeight: 40})
	layout := dialog.Begin(s.noteText("Notes not saved", "笔记尚未保存"), s.noteText("Changes are retained. Retry saving, cancel closing, or discard unsaved note changes.", "更改已保留。可重试保存、取消退出，或放弃尚未保存的笔记更改。"))
	dialog.Actions(layout,
		components.ButtonAction{ID: hitNoteExitDiscard, Label: s.noteText("Discard changes", "放弃修改"), Variant: components.ButtonDestructive},
		components.ButtonAction{ID: hitNoteExitCancel, Label: s.noteText("Cancel exit", "取消退出"), Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitNoteExitRetry, Label: s.noteText("Retry", "重试")})
	dialog.End()
}
func (s *State) clickNoteExit(id gfx.ID) bool {
	if s.noteExit == nil {
		return false
	}
	request := s.noteExit
	switch id {
	case hitNoteExitCancel:
		s.noteExit = nil
	case hitNoteExitRetry:
		s.save()
		if !s.hasUnsavedNotes() {
			s.noteExit = nil
			request.allow()
		}
	case hitNoteExitDiscard:
		s.notes.groupJobs = nil
		s.notes.list = slices.Clone(s.lastSaved.Notes)
		s.notes.folders = slices.Clone(s.lastSaved.NoteFolders)
		s.notes.drafts = map[string]*noteDraft{}
		s.notes.noteDraft = &noteDraft{}
		s.notes.error = ""
		s.selectNoteWithoutSave(s.notes.selected)
		for _, d := range s.fileWindows.documents {
			if d.note == nil {
				continue
			}
			if d.note.content.imageCancel != nil {
				d.note.content.imageCancel()
			}
			d.note.content.noteDraft = nil
			d.syncNoteWindow()
		}
		s.noteExit = nil
		request.allow()
	}
	s.invalidateNoteViews()
	return true
}

func (s *State) noteExitKey(e gfx.KeyEvent) gfx.EventResult {
	if e.Release {
		return gfx.EventResult{Handled: true}
	}
	if e.Key == gfx.KeyEscape {
		s.clickNoteExit(hitNoteExitCancel)
	}
	if e.Key == gfx.KeyTab {
		return gfx.EventResult{}
	}
	if e.Key == gfx.KeyEnter || e.Key == gfx.KeySpace {
		if e.Target == hitNoteExitCancel || e.Target == hitNoteExitRetry || e.Target == hitNoteExitDiscard {
			s.clickNoteExit(e.Target)
		}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}
