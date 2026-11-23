package workbench

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// noteTreeRowHit finds the tree row of the note or folder id, as the page
// declared it: the rows carry the page's identities, not the application's.
func noteTreeRowHit(c *gfx.Canvas, n *notesUI, id string) (gfx.Hit, bool) {
	rows := n.view.treeScratch.rows
	for i := range rows {
		if rows[i].id != id {
			continue
		}
		hit, ok := hitByID(c, n.page.tree.RowID(i))
		if ok {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// clickNoteTree selects the tree's row named label as the page delivers it.
func clickNoteTree(t testing.TB, s *State, label string) {
	t.Helper()
	clickPageLabel(t, s, gfx.RoleButton, label)
}

// clickNoteTreeRow clicks the tree's row of a note or folder id, in the
// event pass of a paint, where the page delivers its choice.
func clickNoteTreeRow(t testing.TB, s *State, n *notesUI, id string) {
	t.Helper()
	size := s.viewport
	if size.Width == 0 {
		size = gfx.Size{Width: 1400, Height: 900}
	}
	var canvas gfx.Canvas
	s.Paint(&canvas, size)
	row := -1
	for i, entry := range n.view.treeScratch.rows {
		if entry.id == id {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatalf("no tree row for %q", id)
	}
	hit, ok := hitByID(&canvas, n.page.tree.RowID(row))
	if !ok {
		t.Fatalf("tree row %q is not painted", id)
	}
	canvas = gfx.Canvas{}
	canvas.BeginEventPass(hit.ID, gfx.ClickInfo{})
	s.Paint(&canvas, size)
	canvas.EndEventPass()
	canvas.RunAfterPaint()
}

// hitByIDRoleLabel finds the hit of an identity with a role and label.
func hitByIDRoleLabel(c *gfx.Canvas, id gfx.ID, role gfx.Role, label string) (gfx.Hit, bool) {
	for _, hit := range c.Hits() {
		if hit.ID == id && hit.Role == role && hit.Label == label {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// hasSeparator reports whether the page paints the splitter named label.
func hasSeparator(c *gfx.Canvas, label string) bool {
	for _, hit := range c.Hits() {
		if (hit.Role == gfx.RoleSeparator || hit.Role == gfx.RoleSlider) && hit.Label == label {
			return true
		}
	}
	return false
}

// separatorHit finds the splitter named label.
func separatorHit(t testing.TB, c *gfx.Canvas, label string) gfx.Hit {
	t.Helper()
	for _, hit := range c.Hits() {
		if (hit.Role == gfx.RoleSeparator || hit.Role == gfx.RoleSlider) && hit.Label == label {
			return hit
		}
	}
	t.Fatalf("no separator named %q", label)
	return gfx.Hit{}
}

// noteTreeRowLabel is the name the tree shows for a note or folder id.
func noteTreeRowLabel(n *notesUI, id string) string {
	for _, row := range n.view.treeScratch.rows {
		if row.id == id {
			return row.label
		}
	}
	return ""
}

// noteSplitterHit finds the page Splitter the notes surface declared by its
// accessibility label.
func noteSplitterHit(t testing.TB, canvas *gfx.Canvas, label string) gfx.Hit {
	t.Helper()
	for _, hit := range canvas.Hits() {
		if hit.Label == label && (hit.Role == gfx.RoleSlider || hit.Role == gfx.RoleSeparator) {
			return hit
		}
	}
	t.Fatalf("no splitter labelled %q", label)
	return gfx.Hit{}
}

func TestNotesLibrarySplitterKeysResizeAndSave(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitNotes)
	state.Click(hitNoteAdd)
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	page := &state.notes.page
	splitter := noteSplitterHit(t, canvas, state.noteText("Resize note library", "调整目录宽度"))
	before := page.library.Pos
	// The old handle moved 5% of its room per arrow; the page splitter keeps
	// that step with StepFraction.
	step := (page.library.Span - 1) * .05
	result := state.notesKey(gfx.KeyEvent{Target: splitter.ID, Key: gfx.KeyRight})
	if !result.Handled || math.Abs(float64(page.library.Pos-(before+step))) > .5 {
		t.Fatalf("right arrow: handled=%t pos=%v, want %v", result.Handled, page.library.Pos, before+step)
	}
	if state.notes.view.width != page.library.Pos {
		t.Fatalf("saved width %v does not follow the splitter %v", state.notes.view.width, page.library.Pos)
	}
	reopened := newTestState(t, state.options)
	if reopened.notes.view.width != state.notes.view.width {
		t.Fatalf("library width %v did not persist, reopened %v", state.notes.view.width, reopened.notes.view.width)
	}
}

func TestNotesSidebarSplitterKeysMoveBothSplits(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.Click(hitNotes)
	state.Click(hitNoteAdd)
	selected := state.notes.selected
	state.activeSection = sectionTerminal
	state.Click(hitNotesPanel)
	state.Click(hitNoteSideLinked)
	size := gfx.Size{Width: 1200, Height: 800}
	canvas, _ := paintAt(state, size)
	content := &state.noteSidebarState().content
	clickNoteTreeRow(t, state, content, selected)
	canvas, _ = paintAt(state, size)
	page := &content.page
	panel := noteSplitterHit(t, canvas, state.noteText("Resize notes panel", "调整笔记侧栏宽度"))
	side := noteSplitterHit(t, canvas, state.noteText("Resize note browser sections", "调整笔记上下区域"))

	beforeWidth, panelStep := state.noteSidebarState().width, (page.panel.Span-1)*.05
	result := state.noteSidebarKey(gfx.KeyEvent{Target: panel.ID, Key: gfx.KeyLeft})
	if !result.Handled || math.Abs(float64(state.noteSidebarState().width-(beforeWidth+panelStep))) > .5 {
		t.Fatalf("panel left arrow: handled=%t width=%v, want %v", result.Handled, state.noteSidebarState().width, beforeWidth+panelStep)
	}
	beforeSide, sideStep := page.side.Pos, (page.side.Span-1)*.05
	result = state.noteSidebarKey(gfx.KeyEvent{Target: side.ID, Key: gfx.KeyDown})
	if !result.Handled || math.Abs(float64(page.side.Pos-(beforeSide+sideStep))) > .5 {
		t.Fatalf("section down arrow: handled=%t pos=%v, want %v", result.Handled, page.side.Pos, beforeSide+sideStep)
	}
	if state.noteSidebarState().fraction == 0 {
		t.Fatal("section fraction was not kept")
	}
}

func TestNotesWorkspaceKeepsTheEditorMinimumAndHairlineWidth(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitNotes)
	state.Click(hitNoteAdd)
	size := gfx.Size{Width: 900, Height: 600}
	// The section's room is known once the page has laid out; give the
	// library the most it may take and still leave the editor its 480px.
	paintAt(state, size)
	bounds := state.catalogSectionBounds()
	library := min(float32(520), max(float32(160), bounds.Width-1-480))
	state.notes.view.width = library
	state.notes.page.library.Pos = library
	paintAt(state, size)
	paintAt(state, size)
	if state.notes.view.width != library {
		t.Fatalf("library width %v, want the usable-480 limit %v", state.notes.view.width, library)
	}
	if got, want := state.notes.view.documentBounds.Width, bounds.Width-library-1; got != want {
		t.Fatalf("document width %v, want %v (one-pixel splitter)", got, want)
	}
	if state.notes.view.documentBounds.Width < 480 {
		t.Fatalf("document width %v is below the 480px minimum", state.notes.view.documentBounds.Width)
	}
}

func TestNotesDrawerSwitchDoesNotCorruptNote(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)

	s.Click(hitNoteAdd)
	note1ID := s.notes.selected
	s.EditorFor(hitNoteTitle).Insert("Alpha")
	s.EditorFor(hitNoteBody).Insert("Content A")

	s.Click(hitNoteAdd)
	note2ID := s.notes.selected
	s.EditorFor(hitNoteTitle).Insert("Beta")
	s.EditorFor(hitNoteBody).Insert("Content B")

	s.Click(hitNoteAdd)
	note3ID := s.notes.selected
	s.EditorFor(hitNoteTitle).Insert("Gamma")
	s.EditorFor(hitNoteBody).Insert("Content C")

	if !s.selectNote(note1ID) {
		t.Fatal("failed to select note 1")
	}

	s.viewport = gfx.Size{Width: 600, Height: 700}
	paintAt(s, s.viewport)

	// Switch from note 1 to note 2 via the library drawer.
	s.Click(hitNoteTreeToggle)
	if !s.notes.view.drawer {
		t.Fatal("drawer did not open for note 2 switch")
	}
	clickNoteTreeRow(t, s, &s.notes, note2ID)
	if s.notes.view.drawer {
		t.Fatal("drawer should close after choosing note 2")
	}
	if s.notes.selected != note2ID {
		t.Fatalf("selected note = %q, want %q", s.notes.selected, note2ID)
	}
	if got := s.notes.title.String(); got != "Beta" {
		t.Fatalf("note title = %q, want %q", got, "Beta")
	}
	if got := s.notes.body.String(); got != "Content B" {
		t.Fatalf("note body = %q, want %q", got, "Content B")
	}

	// Switch from note 2 to note 3 via the library drawer.
	paintAt(s, s.viewport)
	s.Click(hitNoteTreeToggle)
	if !s.notes.view.drawer {
		t.Fatal("drawer did not open for note 3 switch")
	}
	clickNoteTreeRow(t, s, &s.notes, note3ID)
	if s.notes.view.drawer {
		t.Fatal("drawer should close after choosing note 3")
	}
	if s.notes.selected != note3ID {
		t.Fatalf("selected note = %q, want %q", s.notes.selected, note3ID)
	}
	if got := s.notes.title.String(); got != "Gamma" {
		t.Fatalf("note title = %q, want %q", got, "Gamma")
	}
	if got := s.notes.body.String(); got != "Content C" {
		t.Fatalf("note body = %q, want %q", got, "Content C")
	}

	// Switch back to note 2.
	paintAt(s, s.viewport)
	s.Click(hitNoteTreeToggle)
	clickNoteTreeRow(t, s, &s.notes, note2ID)
	if s.notes.selected != note2ID || s.notes.title.String() != "Beta" || s.notes.body.String() != "Content B" {
		t.Fatalf("note 2 corrupted after switching: title=%q, body=%q", s.notes.title.String(), s.notes.body.String())
	}

	// Check that clicking the active note closes the drawer without modifying anything.
	paintAt(s, s.viewport)
	s.Click(hitNoteTreeToggle)
	if !s.notes.view.drawer {
		t.Fatal("drawer did not open when re-clicking toggle")
	}
	clickNoteTreeRow(t, s, &s.notes, note2ID)
	if s.notes.view.drawer {
		t.Fatal("drawer should close when clicking the already-selected note")
	}
	if s.notes.selected != note2ID || s.notes.title.String() != "Beta" {
		t.Fatalf("active note changed: %q, %q", s.notes.selected, s.notes.title.String())
	}

	// Verify all notes in the store retain their original contents.
	for _, entry := range []struct {
		id    string
		title string
		body  string
	}{
		{note1ID, "Alpha", "Content A"},
		{note2ID, "Beta", "Content B"},
		{note3ID, "Gamma", "Content C"},
	} {
		idx := s.noteIndex(entry.id)
		if idx < 0 {
			t.Fatalf("note %s missing from list", entry.title)
		}
		if s.notes.list[idx].Title != entry.title || s.notes.list[idx].Body != entry.body {
			t.Fatalf("stored note %s was corrupted: title=%q, body=%q", entry.title, s.notes.list[idx].Title, s.notes.list[idx].Body)
		}
	}
}
