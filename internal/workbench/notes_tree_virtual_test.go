package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func notesForTree(count int) []savedNote {
	notes := make([]savedNote, count)
	for i := range notes {
		notes[i] = savedNote{ID: fmt.Sprintf("n%03d", i), Title: fmt.Sprintf("Note %d", i), Order: i}
	}
	return notes
}

// checkNoteRowsInView fails unless every row the paint declared intersects the
// list's viewport: the tree must not paint the whole library off screen.
func checkNoteRowsInView(t *testing.T, rows []noteRow, viewport gfx.Rect) {
	t.Helper()
	for _, row := range rows {
		if row.bounds.Y+row.bounds.Height <= viewport.Y || row.bounds.Y >= viewport.Y+viewport.Height {
			t.Fatalf("row %q at %+v is outside the viewport %+v", row.id, row.bounds, viewport)
		}
	}
}

func TestNoteTreeVisibleRange(t *testing.T) {
	cases := []struct {
		offset, height float32
		count          int
		first, last    int
	}{
		{0, 0, 10, 0, 0},
		{0, 100, 0, 0, 0},
		{0, 100, 10, 0, 4},
		{15, 100, 10, 0, 4},
		{30, 90, 10, 1, 4},
		{60, 90, 10, 2, 5},
		{285, 30, 10, 9, 10},
		{300, 30, 10, 10, 10},
		{10000, 100, 10, 10, 10},
	}
	for _, c := range cases {
		first, last := noteTreeVisibleRange(c.offset, c.height, c.count)
		if first != c.first || last != c.last {
			t.Errorf("range(%v, %v, %d) = %d, %d; want %d, %d", c.offset, c.height, c.count, first, last, c.first, c.last)
		}
	}
}

// The notes library paints the rows in view, not the whole library: a large
// library costs one walk per paint, not a declaration, layout and painting of
// every note.
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
	v := &s.notes.view
	geo := v.listArea.Geometry()
	if len(s.notes.rows) == 0 || len(s.notes.rows) >= len(s.notes.list) {
		t.Fatalf("%d rows painted of %d notes", len(s.notes.rows), len(s.notes.list))
	}
	checkNoteRowsInView(t, s.notes.rows, geo.Viewport)
	if _, ok := hitByID(&c, hitNoteRows.At(v.noteTokens["n000"])); !ok {
		t.Fatal("the first note is not painted")
	}
	if _, ok := hitByID(&c, hitNoteRows.At(v.noteTokens["n399"])); ok {
		t.Fatal("an off-screen note was painted")
	}

	v.treeScroll = geo.MaxOffsetY
	paint()
	geo = v.listArea.Geometry()
	if v.treeScroll != geo.MaxOffsetY {
		t.Fatalf("scroll = %v, max %v", v.treeScroll, geo.MaxOffsetY)
	}
	if len(s.notes.rows) >= len(s.notes.list) {
		t.Fatalf("%d rows painted of %d notes after scrolling", len(s.notes.rows), len(s.notes.list))
	}
	checkNoteRowsInView(t, s.notes.rows, geo.Viewport)
	if _, ok := hitByID(&c, hitNoteRows.At(v.noteTokens["n399"])); !ok {
		t.Fatal("the last note is not painted at the end of the list")
	}
	if _, ok := hitByID(&c, hitNoteRows.At(v.noteTokens["n000"])); ok {
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
	v := &content.view
	geo := v.listArea.Geometry()
	visible := 0
	for _, hit := range c.Hits() {
		if _, ok := hit.ID.In(hitNoteRows); ok && hit.Bounds.Y >= geo.Viewport.Y && hit.Bounds.Y < geo.Viewport.Y+geo.Viewport.Height {
			visible++
		}
	}
	if visible == 0 || visible >= len(s.notes.list) {
		t.Fatalf("%d note rows painted of %d", visible, len(s.notes.list))
	}
	if _, ok := hitByID(&c, hitNoteRows.At(s.notes.view.noteTokens["n000"])); !ok {
		t.Fatal("the first note is not painted in the side panel")
	}
	if _, ok := hitByID(&c, hitNoteRows.At(s.notes.view.noteTokens["n399"])); ok {
		t.Fatal("an off-screen note was painted in the side panel")
	}

	v.treeScroll = geo.MaxOffsetY
	paint()
	geo = v.listArea.Geometry()
	if _, ok := hitByID(&c, hitNoteRows.At(s.notes.view.noteTokens["n399"])); !ok {
		t.Fatal("the last note is not painted at the end of the side panel")
	}
	if _, ok := hitByID(&c, hitNoteRows.At(s.notes.view.noteTokens["n000"])); ok {
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
