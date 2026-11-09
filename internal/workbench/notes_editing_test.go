package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestNotesEditingCommandsAndNativeAssistance(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.activeSection = sectionNotes
	s.notes.list = []savedNote{{ID: "note", Title: "Note", Body: "first\nsecond"}}
	s.selectNote("note")
	s.activateNoteView(&s.notes)
	s.notes.body.SetSelection(text.Range{End: s.notes.body.RuneCount()})
	if !s.notesKey(gfx.KeyEvent{Target: hitNoteBody, Key: gfx.KeyTab}).Handled || s.notes.body.String() != "    first\n    second" {
		t.Fatal("Tab must indent selected lines")
	}
	s.notes.body.Undo()
	if s.notes.body.String() != "first\nsecond" {
		t.Fatal("indent must undo as one operation")
	}
	s.notes.body.SetSelection(text.Range{Start: 6, End: 6})
	s.notesKey(gfx.KeyEvent{Target: hitNoteBody, Key: gfx.KeyUp, Modifiers: gfx.ModifierAlt})
	if s.notes.body.String() != "second\nfirst" {
		t.Fatal("move line shortcut was not applied")
	}
	s.notesKey(gfx.KeyEvent{Target: hitNoteBody, Key: gfx.KeyDown, Modifiers: gfx.ModifierAlt | gfx.ModifierShift})
	if strings.Count(s.notes.body.String(), "second") != 2 {
		t.Fatal("duplicate line shortcut was not applied")
	}

	s.notes.body = *text.NewEditor("    item")
	s.notes.body.SetSelection(text.Range{Start: 8, End: 8})
	s.notes.captureNoteSelection()
	input := s.NotesInput(app.TextInputContext{ID: hitNoteBody})
	input.OnEvent(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextInsertNewline})
	if s.notes.body.String() != "    item\n    " {
		t.Fatalf("auto indent: %q", s.notes.body.String())
	}
	input = s.NotesInput(app.TextInputContext{ID: hitNoteBody})
	input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "("})
	if !strings.HasSuffix(s.notes.body.String(), "()") {
		t.Fatal("default bracket pairing was not applied")
	}
}

func TestNotesFindUsesCurrentDraftAndKeepsWindowQueriesSeparate(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.activeSection = sectionNotes
	s.notes.list = []savedNote{{ID: "note", Title: "Note", Body: "中文 alpha 中文"}}
	s.selectNote("note")
	s.activateNoteView(&s.notes)
	s.notes.body.Insert(" unsaved")
	if !s.Shortcut(gfx.KeyEvent{Target: hitNoteBody, Text: "f", Modifiers: gfx.ModifierControl}).Handled {
		t.Fatal("find shortcut was not consumed")
	}
	input := s.NotesInput(app.TextInputContext{ID: hitNoteFind})
	if input == nil {
		t.Fatal("find input missing")
	}
	input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "中文"})
	if _, total := s.notes.editing.find.Position(&s.notes.body); total != 2 {
		t.Fatal("Unicode matches missing")
	}
	s.notesKey(gfx.KeyEvent{Target: hitNoteFind, Key: gfx.KeyEnter})
	if current, _ := s.notes.editing.find.Position(&s.notes.body); current != 2 {
		t.Fatal("Enter did not step to second match")
	}
	result := s.Shortcut(gfx.KeyEvent{Target: hitNoteFindNext, Text: "f", Modifiers: gfx.ModifierControl})
	if !result.Handled || result.Focus != hitNoteBody || s.notes.editing.findOpen || s.notes.editing.findFocus {
		t.Fatal("second find shortcut did not close and restore editor focus")
	}
	s.Shortcut(gfx.KeyEvent{Target: hitNoteFormats.At(0), Text: "f", Modifiers: gfx.ModifierControl})
	if !s.notes.editing.findOpen || s.notes.editing.find.Query.String() != "中文" {
		t.Fatal("reopening did not retain query")
	}
	s.Shortcut(gfx.KeyEvent{Target: hitNoteFind, Text: "f", Modifiers: gfx.ModifierControl, Repeat: true})
	if !s.notes.editing.findOpen {
		t.Fatal("key repeat toggled find")
	}
	w := noteEditingWindow(t, s, "note")
	w.noteWindowShortcut(gfx.KeyEvent{Target: hitNoteBody, Text: "f", Modifiers: gfx.ModifierMeta})
	noteTextEvent(t, w, hitNoteFind, app.TextEvent{Kind: app.TextInsert, Text: "unsaved"})
	if _, total := w.note.content.editing.find.Position(&w.note.content.body); total != 1 {
		t.Fatal("window find missed live draft")
	}
	if s.notes.editing.find.Query.String() != "中文" {
		t.Fatal("window overwrote main find query")
	}
	result = w.noteWindowKey(gfx.KeyEvent{Target: hitNoteFind, Key: gfx.KeyEscape})
	if !result.Handled || w.note.content.editing.findOpen || result.Focus != hitNoteBody {
		t.Fatal("Escape did not close find and restore focus")
	}
}

func TestNotesPreviewReusesLayout(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.activeSection = sectionNotes
	s.notes.list = []savedNote{{ID: "note", Title: "Note", Body: "# Heading\n\nSome text to measure."}}
	s.selectNote("note")
	var c gfx.Canvas
	measurements := 0
	c.SetMeasurer(func(value string, _, _ float32) float32 { measurements++; return float32(len([]rune(value))) * 7 })
	b := gfx.Rect{Width: 400, Height: 300}
	s.paintNotePreviewContent(&c, b, &s.notes)
	first := measurements
	measurements = 0
	s.paintNotePreviewContent(&c, b, &s.notes)
	if first == 0 || measurements >= first {
		t.Fatalf("steady preview repeated layout: first=%d second=%d", first, measurements)
	}
	s.notes.body.Insert(" changed")
	s.paintNotePreviewContent(&c, b, &s.notes)
	if !strings.Contains(s.notes.rendered.SelectedText(components.MarkdownSelection{Focus: s.notes.rendered.TextLen()}), "changed") {
		t.Fatal("preview cache ignored changed draft")
	}
	b.Width = 200
	s.paintNotePreviewContent(&c, b, &s.notes)
	if s.notes.previewLayout.width != 188 {
		t.Fatal("preview cache ignored resize")
	}

}
