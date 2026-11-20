package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func notesForTree(count int) []savedNote {
	notes := make([]savedNote, count)
	for i := range notes {
		notes[i] = savedNote{ID: fmt.Sprintf("n%03d", i), Title: fmt.Sprintf("Note %d", i), Order: i}
	}
	return notes
}

// noteTreeDeclaredRows returns the rows the page tree laid out, by their
// visible index, from what the last paint left.
func noteTreeDeclaredRows(n *notesUI) int {
	_, last := n.page.tree.Visible()
	return last + 1
}

// TestNotesTreePaintsOnlyVisibleRows checks that a large library costs one
// declaration of the rows in view, not of every note.
func TestNotesTreePaintsOnlyVisibleRows(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.notes.list = notesForTree(400)
	var c gfx.Canvas
	paint := func() {
		c.Reset()
		s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	}
	paint()
	if got := noteTreeDeclaredRows(&s.notes); got == 0 || got >= len(s.notes.list) {
		t.Fatalf("%d rows declared of %d notes", got, len(s.notes.list))
	}
	if _, ok := noteTreeRowHit(&c, &s.notes, "n000"); !ok {
		t.Fatal("the first note is not painted")
	}
	if _, ok := noteTreeRowHit(&c, &s.notes, "n399"); ok {
		t.Fatal("an off-screen note was painted")
	}
	first, ok := noteTreeRowHit(&c, &s.notes, "n000")
	if !ok {
		t.Fatal("the first note is not painted")
	}
	config := s.WindowConfig(app.Config{OnScroll: s.Scroll})
	for range 40 {
		config.OnScroll(gfx.ScrollEvent{X: first.Bounds.X + 10, Y: first.Bounds.Y + 5, DeltaY: 300})
	}
	paint()
	if _, ok := noteTreeRowHit(&c, &s.notes, "n399"); !ok {
		t.Fatal("the last note is not painted at the end of the list")
	}
	if _, ok := noteTreeRowHit(&c, &s.notes, "n000"); ok {
		t.Fatal("the first note is still painted after scrolling to the end")
	}
}

// The notes side panel shares the library tree and has the same rule.
func TestNotesSidebarPaintsOnlyVisibleRows(t *testing.T) {
	s, _, _, _ := liveSession(t)
	s.notes.list = notesForTree(400)
	s.activeSection = sectionTerminal
	s.Click(hitNotesPanel)
	content := &s.noteSidebarState().content
	content.sideAll = true
	var c gfx.Canvas
	paint := func() {
		c.Reset()
		s.Paint(&c, gfx.Size{Width: 1200, Height: 800})
	}
	paint()
	if got := noteTreeDeclaredRows(content); got == 0 || got >= len(s.notes.list) {
		t.Fatalf("%d note rows declared of %d", got, len(s.notes.list))
	}
	if _, ok := noteTreeRowHit(&c, content, "n000"); !ok {
		t.Fatal("the first note is not painted in the side panel")
	}
	if _, ok := noteTreeRowHit(&c, content, "n399"); ok {
		t.Fatal("an off-screen note was painted in the side panel")
	}
	first, _ := noteTreeRowHit(&c, content, "n000")
	config := s.WindowConfig(app.Config{OnScroll: s.Scroll})
	for i := 0; i < 40; i++ {
		config.OnScroll(gfx.ScrollEvent{X: first.Bounds.X + 10, Y: first.Bounds.Y + 5, DeltaY: 300})
		if i%4 == 3 {
			paint()
		}
	}
	paint()
	if _, ok := noteTreeRowHit(&c, content, "n399"); !ok {
		t.Fatal("the last note is not painted at the end of the side panel")
	}
	if _, ok := noteTreeRowHit(&c, content, "n000"); ok {
		t.Fatal("the first note is still painted after the side panel scrolled to the end")
	}
}

func BenchmarkPaintNotesTree400(b *testing.B) {
	s, _, _, _ := liveSession(b)
	s.Click(hitNotes)
	s.notes.list = notesForTree(400)
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		c.Reset()
		s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	}
}
