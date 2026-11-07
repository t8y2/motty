package workbench

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

func TestNotesRenderedPreviewSelectsAndCopiesText(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.body.Insert("alpha **beta** gamma\n\n```go\nx := 1\n```\n\n[link](https://example.com)")
	s.notes.view.mode = notePreview
	var c ui.Canvas
	c.SetMeasurer(func(value string, _, _ float32) float32 { return float32(len([]rune(value))) * 10 })
	s.Paint(&c, ui.Size{Width: 1400, Height: 900})
	text, ok := hitByID(&c, hitNotePreviewText)
	if !ok || !text.Focusable || !text.SkipTab || text.Cursor != ui.PointerCursorText {
		t.Fatalf("preview is not selectable: %+v", text)
	}
	if hitIndex(c.Hits(), hitNotePreviewText) >= hitIndex(c.Hits(), hitNoteLinks.At(0)) {
		t.Fatal("selection hit obscures links")
	}
	b := s.notes.previewBounds
	start := ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hitNotePreviewText, X: b.X + 6, Y: b.Y + 8}
	if r := s.notesPointer(start); !r.Handled || r.Focus != hitNotePreviewText {
		t.Fatal("press did not focus preview selection")
	}
	end := start
	end.Kind, end.Captured, end.X = ui.PointerMove, true, start.X+50
	s.notesPointer(end)
	end.Kind = ui.PointerUp
	s.notesPointer(end)
	var copied string
	s.options.SetClipboardText = func(value string) error { copied = value; return nil }
	copy := ui.KeyEvent{Target: hitNotePreviewText, Text: "c", Modifiers: keymap.Primary}
	if r := s.Shortcut(copy); !r.Handled || copied != "alpha" || s.notes.view.previewSelect.Dragging() {
		t.Fatalf("drag copy = %q, result=%+v", copied, r)
	}
	all := ui.KeyEvent{Target: hitNotePreviewText, Text: "a", Modifiers: keymap.Primary}
	if !s.Key(all).Handled {
		t.Fatal("select-all was not routed from preview")
	}
	s.Shortcut(copy)
	if !strings.Contains(copied, "alpha beta gamma") || !strings.Contains(copied, "x := 1") || strings.Contains(copied, "**") || strings.Contains(copied, "```") {
		t.Fatalf("copied source instead of rendered text: %q", copied)
	}
	before := s.notes.body.String()
	s.options.SetClipboardText = func(string) error { return errors.New("clipboard unavailable") }
	s.Shortcut(copy)
	if s.notes.error != "clipboard unavailable" || s.notes.body.String() != before {
		t.Fatal("copy failure lost error or modified body")
	}
}

func TestNotesPreviewSelectionIsIsolatedAndClearedWhenContentChanges(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.body.Insert("中文内容")
	s.notes.view.mode = notePreview
	var c ui.Canvas
	s.Paint(&c, ui.Size{Width: 1400, Height: 900})
	s.Shortcut(ui.KeyEvent{Target: hitNotePreviewText, Text: "a", Modifiers: keymap.Primary})
	copied := "unchanged"
	s.options.SetClipboardText = func(value string) error { copied = value; return nil }
	copy := ui.KeyEvent{Target: hitNotePreviewText, Text: "c", Modifiers: keymap.Primary}
	s.Shortcut(copy)
	if copied != "中文内容" {
		t.Fatalf("Chinese copy = %q", copied)
	}
	s.notes.view.popup = "tags"
	copied = "unchanged"
	s.Shortcut(copy)
	if copied != "unchanged" {
		t.Fatal("overlay copied background selection")
	}
	s.notes.view.popup = ""
	s.notes.body.Insert("修改")
	c.Reset()
	s.Paint(&c, ui.Size{Width: 1400, Height: 900})
	copied = "unchanged"
	s.Shortcut(copy)
	if copied != "unchanged" {
		t.Fatal("changed content retained stale selection")
	}
	s.Shortcut(ui.KeyEvent{Target: hitNotePreviewText, Text: "a", Modifiers: keymap.Primary})
	s.notesKey(ui.KeyEvent{Target: hitNotePreviewText, Key: ui.KeyEscape})
	if s.notes.rendered.SelectedText(s.notes.view.previewSelect.In(0)) != "" {
		t.Fatal("Escape did not clear selection")
	}
	s.notes.view.previewSelect.Select(components.TextPoint{}, components.TextPoint{Offset: 1})
	s.notes.view.previewSelect.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary}, func(ui.Point) (components.TextPoint, bool) { return components.TextPoint{}, true })
	s.selectNoteWithoutSave(s.notes.selected)
	if s.notes.view.previewSelect.Dragging() {
		t.Fatal("switching notes retained a drag")
	}
}
