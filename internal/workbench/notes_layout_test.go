package workbench

import (
	"math"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"strings"
	"testing"
)

func TestNotesWorkspaceGivesTheDocumentItsWidth(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	body, ok := hitByID(&c, hitNoteBody)
	if !ok || body.Bounds.Width < 650 {
		t.Fatalf("source is squeezed: %+v", body.Bounds)
	}
	if _, ok := hitByID(&c, hitNoteInline); ok {
		t.Fatal("folder form is permanently visible")
	}
	row, ok := noteTreeRowHit(&c, &s.notes, s.notes.selected)
	if !ok || !row.Selected {
		t.Fatal("selected note is not visible as a selected tree row")
	}
	more, ok := hitByID(&c, hitNoteRowMore.At(s.notes.view.noteTokens[s.notes.selected]))
	if !ok || more.Bounds.Y+more.Bounds.Height/2 != row.Bounds.Y+row.Bounds.Height/2 {
		t.Fatal("tree action is not vertically centered in its row")
	}
	toggle, ok := hitByID(&c, hitNoteTreeToggle)
	title, titleOK := hitByID(&c, hitNoteTitle)
	if !ok || !titleOK || toggle.Bounds.Y+toggle.Bounds.Height/2 != title.Bounds.Y+title.Bounds.Height/2 {
		t.Fatal("library toggle is not vertically centered beside the title")
	}
}

func TestNotesLibraryPrimaryActionSpansAboveTheList(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	add, ok := hitByID(&c, hitNoteAdd)
	if !ok {
		t.Fatal("new note action is missing")
	}
	if add.Role != gfx.RoleButton || add.Label != s.noteText("New note", "新建笔记") {
		t.Fatalf("new note action lacks button semantics: %+v", add)
	}
	search, searchOK := hitByID(&c, hitNoteSearchToggle)
	if !searchOK || add.Bounds.X != search.Bounds.X || add.Bounds.Width != s.notes.page.library.Pos-16 || add.Bounds.Height < 36 {
		t.Fatalf("new note action does not span the library: width=%v action=%+v", s.notes.page.library.Pos, add.Bounds)
	}
	row, ok := noteTreeRowHit(&c, &s.notes, s.notes.selected)
	if !ok || add.Bounds.Y+add.Bounds.Height > row.Bounds.Y {
		t.Fatalf("new note action is not above the list: action=%+v row=%+v", add.Bounds, row.Bounds)
	}
}

// With no note chosen, the editor side invites the user to pick or create one
// rather than leaving a bare line, and an empty library says where to start.
func TestNotesEmptyStatesInviteTheFirstNote(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	want := map[string]bool{
		s.noteText("Start with a note", "从一篇笔记开始"):                           false,
		s.noteText("Your first working note starts here.", "第一条工作记录，从这里开始。"): false,
	}
	description := false
	for _, text := range c.Texts() {
		if _, ok := want[text.Value]; ok {
			want[text.Value] = true
		}
		description = description || strings.Contains(text.Value, s.noteText("Select a note", "从目录选择"))
	}
	for value, found := range want {
		if !found {
			t.Fatalf("empty state does not show %q", value)
		}
	}
	if !description {
		t.Fatal("empty state does not explain what to do")
	}
}

// A note's name is centered in its tree row: the row's icon, name and action
// button share one vertical centre.
func TestNotesTreeTitleIsVerticallyCenteredInItsRow(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.notes.list = []savedNote{{ID: "centered", Title: "Centered note", Body: "body"}}
	s.Click(hitNotes)
	s.selectNote("centered")
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	row, ok := noteTreeRowHit(&c, &s.notes, "centered")
	if !ok {
		t.Fatal("the note row is not painted")
	}
	for _, text := range c.Texts() {
		if text.Value != "Centered note" {
			continue
		}
		if text.Bounds.Y < row.Bounds.Y || text.Bounds.Y+text.Bounds.Height > row.Bounds.Y+row.Bounds.Height {
			t.Fatalf("note name %+v is not inside its row %+v", text.Bounds, row.Bounds)
		}
		nameCenter := text.Bounds.Y + text.Bounds.Height/2
		rowCenter := row.Bounds.Y + row.Bounds.Height/2
		if math.Abs(float64(nameCenter-rowCenter)) > 1 {
			t.Fatalf("note name centre %.1f is not the row centre %.1f", nameCenter, rowCenter)
		}
		return
	}
	t.Fatal("the note name is not painted")
}

func TestNotesModesAndDirectoryPreferencesSurviveReopening(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.Click(hitNoteModes.At(2))
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	if s.notes.previewBounds.Width <= 0 {
		t.Fatal("split preview missing")
	}
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 800, Height: 700})
	if s.notes.previewBounds.Width > 0 {
		t.Fatal("narrow document still splits")
	}
	if _, ok := hitByID(&c, hitNoteBody); !ok {
		t.Fatal("narrow split fallback lost source")
	}
	s.Click(hitNoteTreeToggle)
	if !s.notes.view.drawer {
		t.Fatal("narrow library did not open drawer")
	}
	s.Click(hitNoteDrawerDismiss)
	s.save()
	reopened := newTestState(t, s.options)
	if reopened.notes.view.mode != noteSplit || reopened.notes.view.width != 300 {
		t.Fatal("view preference was overwritten by automatic collapse")
	}
}
func TestNotesFormattingKeepsSelectionUndoAndComposition(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	e := s.EditorFor(hitNoteBody)
	e.Insert("检查日志")
	e.SetSelection(text.Range{Start: 0, End: 2})
	s.Click(hitNoteFormats.At(1))
	if e.String() != "**检查**日志" {
		t.Fatalf("formatting selection = %q", e.String())
	}
	if !e.Undo() || e.String() != "检查日志" {
		t.Fatal("formatting did not undo in one step")
	}
	e.MoveTo(e.RuneCount(), false)
	e.SetMarkedText("输入", text.Range{End: 2})
	before := e.String()
	s.Click(hitNoteFormats.At(1))
	if e.String() != before {
		t.Fatal("formatting modified a composition")
	}
}
func TestNotesTreeTargetsRemainStableAfterDeletingAnotherNote(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	first := s.notes.selected
	s.Click(hitNoteAdd)
	second := s.notes.selected
	s.Click(hitNoteAdd)
	clickNoteTreeRow(t, s, &s.notes, first)
	s.Click(hitNoteDelete)
	s.Click(hitNoteConfirm)
	clickNoteTreeRow(t, s, &s.notes, second)
	if s.notes.selected != second || s.noteIndex(first) >= 0 {
		t.Fatal("tree token selected a shifted array item")
	}
}

func TestNotesSidePanelScrollBelongsToTheVisibleDocument(t *testing.T) {
	s, _, _, _ := liveSession(t)
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.EditorFor(hitNoteBody).Insert(strings.Repeat("A paragraph.\n\n", 100))
	s.save()
	s.activeSection = sectionTerminal
	s.Click(hitNotesPanel)
	s.Click(hitNoteSideLinked)
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1200, Height: 800})
	clickNoteTreeRow(t, s, &s.noteSidebarState().content, s.notes.selected)
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 1200, Height: 800})
	n := &s.noteSidebarState().content
	b := n.previewBounds
	config := s.WindowConfig(app.Config{OnScroll: s.Scroll})
	config.OnScroll(gfx.ScrollEvent{X: b.X + 20, Y: b.Y + 40, DeltaY: 100, Viewport: gfx.Size{Width: 1200, Height: 800}})
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 1200, Height: 800})
	if n.previewScroll <= 0 {
		t.Fatal("document wheel did not scroll the preview")
	}
}

func TestNotesFolderCreationCommitsOnTabAndDropAfterAppends(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteFolderAdd)
	s.EditorFor(hitNoteInline).Insert("Alpha")
	s.Key(gfx.KeyEvent{Target: hitNoteInline, Key: gfx.KeyTab})
	if len(s.notes.folders) != 1 {
		t.Fatal("Tab left folder creation pending")
	}
	a := s.notes.folders[0].ID
	s.notes.folder = ""
	s.Click(hitNoteFolderAdd)
	s.EditorFor(hitNoteInline).Insert("Beta")
	s.Key(gfx.KeyEvent{Target: hitNoteInline, Key: gfx.KeyEnter})
	b := s.notes.folders[1].ID
	if !s.moveNote(a, "", "", true) {
		t.Fatal("append move rejected")
	}
	fa, _ := s.noteFolder(a)
	fb, _ := s.noteFolder(b)
	if fa.Order <= fb.Order {
		t.Fatal("drop after last folder inserted before it")
	}
}
func TestNotesCodeBlockFormattingStartsAtLineBoundaries(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	e := s.EditorFor(hitNoteBody)
	e.Insert("abc")
	e.MoveTo(1, false)
	s.Click(hitNoteFormats.At(4))
	if e.String() != "```\nabc\n```" {
		t.Fatalf("invalid fenced block: %q", e.String())
	}
}

func TestNotesEmptyTreeSpaceSelectsRootWithoutClosingDocument(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.notes.folders = []savedNoteFolder{{ID: "folder", Name: "Folder"}}
	s.notes.folder = "folder"
	s.Click(hitNoteAdd)
	selected := s.notes.selected
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&c, tree.ID(notesHits, "all")); ok {
		t.Fatal("all notes row still exists")
	}
	r := s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitNoteListScroll})
	if !r.Handled || s.notes.folder != "" || s.notes.selected != selected {
		t.Fatal("empty space did not select root while retaining document")
	}
	s.Click(hitNoteAdd)
	if s.notes.draft.Folder != "" {
		t.Fatal("new note did not use root")
	}
	s.Click(hitNoteFolderAdd)
	if s.notes.view.inlineParent != "" {
		t.Fatal("new folder did not use root")
	}
}

func TestNotesExpandActionMatchesFolderState(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.notes.folders = []savedNoteFolder{{ID: "a"}, {ID: "b"}}
	s.notes.view.expanded["a"] = true
	if !s.noteCanExpandAll() {
		t.Fatal("partial expansion must offer expand all")
	}
	s.Click(hitNoteExpandAll)
	if s.noteCanExpandAll() {
		t.Fatal("expand all did not open every folder")
	}
	s.Click(hitNoteExpandAll)
	if s.notes.view.expanded["a"] || s.notes.view.expanded["b"] {
		t.Fatal("collapse all did not close every folder")
	}
}

func TestNotesTerminalScopeSwitchPreservesDocument(t *testing.T) {
	s, _, _, _ := liveSession(t)
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	selected := s.notes.selected
	s.activeSection = sectionTerminal
	s.Click(hitNotesPanel)
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1200, Height: 800})
	if _, ok := hitByID(&c, hitNoteSideLinked); !ok {
		t.Fatal("scope switch missing")
	}
	for _, id := range []gfx.ID{hitNoteTreeMore, tree.ID(notesHits, "all"), hitNoteAdd, hitNotes} {
		if _, ok := hitByID(&c, id); ok {
			t.Fatal("editing action remains in sidebar")
		}
	}
	s.Click(hitNoteSideLinked)
	if !s.noteSidebarState().content.sideAll {
		t.Fatal("all scope not applied")
	}
	s.Click(hitNoteRows.At(s.notes.view.noteTokens[selected]))
	s.Click(hitNoteSideLinked)
	if s.noteSidebarState().content.sideAll || s.noteSidebarState().content.selected != selected || s.notes.selected != selected {
		t.Fatal("scope switch changed selection")
	}
}

func TestUnselectedMoreMenuFallbackAndLayer(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	// Deselect note to test unselected more menu positioning
	s.notes.selected = ""
	s.notes.view.menu = "note"
	s.notes.view.menuID = s.notes.list[0].ID
	s.notes.view.overlayFrom = hitNoteRowMore.At(0)
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1400, Height: 900})

	found := false
	for _, text := range c.Texts() {
		if text.Value == "Rename" {
			found = true
			if text.Bounds.X < 200 {
				t.Fatalf("Rename menu rendered at X=%.1f, overlapping workbench!", text.Bounds.X)
			}
			// The menu is a page menu: it paints on a widget layer above the
			// page rather than on the old canvas overlay's fixed layer.
			if text.Layer == 0 {
				t.Fatal("Rename menu painted under the page")
			}
		}
	}
	if !found {
		t.Fatal("Rename menu item not found in canvas texts")
	}

	// Test folder more menu click selects folder and anchors properly
	s.Click(hitNoteDismiss)
	s.Click(hitNoteFolderAdd)
	s.EditorFor(hitNoteInline).Insert("Folder")
	s.Key(gfx.KeyEvent{Target: hitNoteInline, Key: gfx.KeyEnter})
	if len(s.notes.folders) == 0 {
		t.Fatal("folder was not created")
	}
	folderID := s.notes.folders[0].ID
	s.notes.folder = ""
	s.notes.view.treeFolderSelection = false
	s.Click(hitNoteFolderMore.At(0))
	if !s.notes.view.treeFolderSelection || s.notes.folder != folderID {
		t.Fatalf("clicking folder more button did not select folder: folder=%q, treeFolder=%v", s.notes.folder, s.notes.view.treeFolderSelection)
	}
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	found = false
	for _, text := range c.Texts() {
		if text.Value == "Delete folder" {
			found = true
			if text.Bounds.X < 200 {
				t.Fatalf("folder Delete menu rendered at X=%.1f, overlapping workbench!", text.Bounds.X)
			}
			if text.Layer == 0 {
				t.Fatal("folder menu painted under the page")
			}
		}
	}
	if !found {
		t.Fatal("Delete folder menu item not found in canvas texts")
	}
}
