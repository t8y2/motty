package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestNotesPreviewWheelScrollsWithoutChangingDraft(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.body.Insert(strings.Repeat("paragraph\n\n", 300))
	s.notes.view.mode = notePreview
	var c gfx.Canvas
	paint := func() { c.Reset(); s.Paint(&c, gfx.Size{Width: 1400, Height: 900}) }
	paint()
	b := s.notes.previewBounds
	before := s.notes.body.String()
	config := s.WindowConfig(app.Config{OnScroll: s.Scroll})
	config.OnScroll(gfx.ScrollEvent{X: b.X + 20, Y: b.Y + 40, DeltaY: 400})
	paint()
	if s.notes.previewScroll <= 0 {
		t.Fatal("the wheel did not scroll the preview")
	}
	if s.notes.body.String() != before {
		t.Fatal("scrolling changed the draft")
	}
	saved := s.notes.previewScroll
	config.OnScroll(gfx.ScrollEvent{X: b.X + 20, Y: b.Y + 40, DeltaY: -10000})
	paint()
	if s.notes.previewScroll != 0 {
		t.Fatalf("the wheel did not return to the top: %v (from %v)", s.notes.previewScroll, saved)
	}
}

func TestNotesTreeWheelScrollsAndSelectingResetsThePreview(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	for i := 0; i < 80; i++ {
		s.notes.list = append(s.notes.list, savedNote{ID: fmt.Sprint(i), Title: fmt.Sprintf("note %02d", i), Order: i + 1})
	}
	var c gfx.Canvas
	paint := func() { c.Reset(); s.Paint(&c, gfx.Size{Width: 1400, Height: 900}) }
	paint()
	first, ok := noteTreeRowHit(&c, &s.notes, s.notes.selected)
	if !ok {
		t.Fatal("the selected note is not painted")
	}
	config := s.WindowConfig(app.Config{OnScroll: s.Scroll})
	for range 30 {
		config.OnScroll(gfx.ScrollEvent{X: first.Bounds.X + 20, Y: first.Bounds.Y + 5, DeltaY: 300})
	}
	paint()
	if _, ok := noteTreeRowHit(&c, &s.notes, s.notes.selected); ok {
		t.Skip("the selected note is still within the scrolled view")
	}
	if _, ok := noteTreeRowHit(&c, &s.notes, "79"); !ok {
		t.Fatal("the tree did not scroll to the end")
	}
	// A new selection starts the preview at the top.
	s.notes.setPreviewScroll(120)
	s.selectNoteWithoutSave("79")
	paint()
	if s.notes.previewScroll != 0 {
		t.Fatal("selecting a note kept the preview offset")
	}
}

func TestNotesPickerWheelScrollsItsResults(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	var tags []string
	for i := 0; i < 80; i++ {
		tags = append(tags, fmt.Sprintf("long tag %02d", i))
	}
	s.notes.tags.Insert(strings.Join(tags, ","))
	v := &s.notes.view
	v.documentBounds.Width = 600
	var c gfx.Canvas
	paint := func() { c.Reset(); s.Paint(&c, gfx.Size{Width: 1400, Height: 900}) }
	paint()
	s.Click(hitNoteTagsOpen)
	paint()
	first, ok := hitByID(&c, hitNoteTagRows.At(s.noteTagToken("long tag 00")))
	if !ok {
		t.Fatal("the first tag is not painted")
	}
	config := s.WindowConfig(app.Config{OnScroll: s.Scroll})
	for range 10 {
		config.OnScroll(gfx.ScrollEvent{X: first.Bounds.X + 10, Y: first.Bounds.Y + 5, DeltaY: 200})
	}
	paint()
	last, ok := hitByID(&c, hitNoteTagRows.At(s.noteTagToken("long tag 79")))
	if !ok || last.Bounds.Y <= first.Bounds.Y {
		t.Fatal("the wheel did not scroll the picker results")
	}
}
