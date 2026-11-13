package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	layoutcore "motty/internal/workspace"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNotesSidebarExpandAllIsIndependentAndDisabledDuringSearch(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.notes.folders = []savedNoteFolder{{ID: "parent"}, {ID: "child", Parent: "parent"}}
	n := &s.noteSidebarState().content
	s.clickNoteSidebar(hitNoteSideExpand)
	for _, folder := range s.notes.folders {
		if !n.view.expanded[folder.ID] || s.notes.view.expanded[folder.ID] {
			t.Fatal("expand all must affect only the sidebar")
		}
	}
	n.query.Insert("needle")
	var c gfx.Canvas
	s.paintNoteSidebarTree(&c, gfx.Rect{Width: 400, Height: 400})
	if hit, ok := hitByID(&c, hitNoteSideExpand); !ok || !hit.Disabled {
		t.Fatal("expand button must be disabled during search")
	}
	s.clickNoteSidebar(hitNoteSideExpand)
	if !n.view.expanded["child"] {
		t.Fatal("search changed expansion state")
	}
	n.query = text.Editor{}
	s.clickNoteSidebar(hitNoteSideExpand)
	if n.view.expanded["parent"] || n.view.expanded["child"] {
		t.Fatal("second click must collapse all")
	}
}

func TestNotesSidebarReadOnlyFilteringAndSharedDraft(t *testing.T) {
	s, _, _, _ := liveSession(t)
	session, _ := s.sessionByIDPointer(s.activeSessionID)
	s.hosts = []Host{session.host}
	hostTag := encodeNoteHost(noteHost{CatalogID: session.host.CatalogID, Label: "Server"})
	s.notes.folders = []savedNoteFolder{{ID: "parent", Name: "Parent"}, {ID: "child", Name: "Child", Parent: "parent"}}
	s.notes.list = []savedNote{{ID: "pinned", Title: "Pinned", Pinned: true, Tags: []string{"needle"}, Folder: "child"}, {ID: "linked", Title: "Linked", Tags: []string{hostTag}}, {ID: "other", Title: "Other", Body: "saved"}}
	s.Click(hitNotes)
	s.selectNote("other")
	s.notes.body.Insert(" unsaved")
	before := s.notes.body.String()
	s.activeSection = sectionTerminal
	s.Click(hitNotesPanel)
	v := s.noteSidebarState()
	n := &v.content
	var c gfx.Canvas
	paint := func() { c.Reset(); s.Paint(&c, gfx.Size{Width: 1200, Height: 800}) }
	paint()
	if n.selected != "" {
		t.Fatal("first open selected a note")
	}
	n.query.Insert("needle")
	paint()
	if _, ok := hitByID(&c, hitNoteFolderRows.At(s.notes.view.folderTokens["child"])); !ok {
		t.Fatal("missing ancestor")
	}
	if _, ok := hitByID(&c, hitNoteRows.At(s.notes.view.noteTokens["pinned"])); !ok {
		t.Fatal("pinned tag match missing")
	}
	if _, ok := hitByID(&c, hitNoteRows.At(s.notes.view.noteTokens["other"])); ok {
		t.Fatal("unrelated note passed filter")
	}
	s.Click(hitNoteRows.At(s.notes.view.noteTokens["pinned"]))
	paint()
	// Saving the per-tab view may flush the shared draft; browsing must not
	// change its contents or select a different note in the main editor.
	if s.notes.selected != "other" || s.notes.body.String() != before || s.notes.list[2].Body != before {
		t.Fatal("browser changed or switched the main draft")
	}
	s.Click(hitNoteSideLinked)
	n.query.SetSelection(text.Range{End: len([]rune(n.query.String()))})
	n.query.Insert("unsaved")
	paint()
	if _, ok := hitByID(&c, hitNoteRows.At(s.notes.view.noteTokens["other"])); !ok {
		t.Fatal("draft search is stale")
	}
	s.Click(hitNoteRows.At(s.notes.view.noteTokens["other"]))
	paint()
	if n.body.String() != before {
		t.Fatal("preview omitted shared draft")
	}
	s.notes.body.Insert(" live")
	paint()
	if !strings.HasSuffix(n.body.String(), " live") {
		t.Fatal("preview did not follow draft")
	}
	for _, id := range []gfx.ID{hitNoteAdd, hitNotePin, hitNoteDelete, hitNoteTreeMore} {
		s.Click(id)
	}
	if len(s.notes.list) != 3 || s.notes.selected != "other" || s.notes.view.menu != "" {
		t.Fatal("stale editing action mutated data")
	}
	if s.EditorFor(hitNoteBody) != nil || s.EditorFor(hitNoteTitle) != nil || s.EditorFor(hitNoteSearch) != &n.query {
		t.Fatal("sidebar exposes editable content")
	}
	s.Click(hitNoteSideLinked)
	paint()
	if n.selected != "other" {
		t.Fatal("filter hid open preview")
	}
	s.Click(hitNotesPanel)
	s.Click(hitNotesPanel)
	if n.selected != "other" || n.query.String() != "unsaved" || n.sideAll {
		t.Fatal("reopen lost browser state")
	}
	s.notes.list = s.notes.list[:2]
	paint()
	if n.selected != "" || n.imageSource != "" {
		t.Fatal("deleted note remained open")
	}
}

func TestNotesSidebarResizeScrollAndSelectionAreIndependent(t *testing.T) {
	s, _, _, _ := liveSession(t)
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.body.Insert(strings.Repeat("Paragraph **copy**\n\n", 120))
	key := s.notes.selected
	s.activeSection = sectionTerminal
	s.Click(hitNotesPanel)
	s.Click(hitNoteSideLinked)
	var c gfx.Canvas
	paint := func() { c.Reset(); s.Paint(&c, gfx.Size{Width: 1200, Height: 800}) }
	paint()
	s.Click(hitNoteRows.At(s.notes.view.noteTokens[key]))
	paint()
	v := s.noteSidebarState()
	n := &v.content
	if v.bounds.Width != 400 {
		t.Fatalf("default width %v", v.bounds.Width)
	}
	s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Target: hitNoteSideWidth, X: v.bounds.X, Y: v.bounds.Y + 60})
	s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: v.bounds.X - 100, Y: v.bounds.Y + 60})
	s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerUp, X: v.bounds.X - 100, Y: v.bounds.Y + 60})
	paint()
	if v.bounds.Width < 499 || v.bounds.Width > 501 {
		t.Fatalf("resize %v", v.bounds.Width)
	}
	y := v.bounds.Y + 200
	s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Target: hitNoteSideSplit, X: v.bounds.X + 20, Y: y})
	s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerUp, X: v.bounds.X + 20, Y: y})
	paint()
	if n.listBounds.Y+n.listBounds.Height > y+2 || n.previewBounds.Y < y {
		t.Fatal("split geometry mismatch")
	}
	s.notesScroll(gfx.ScrollEvent{X: n.previewBounds.X + 20, Y: n.previewBounds.Y + 20, DeltaY: 100})
	if n.previewScroll <= 0 || n.view.treeScroll != 0 || s.notes.previewScroll != 0 {
		t.Fatal("scroll state leaked")
	}
	n.view.previewSelect.Select(components.TextPoint{}, components.TextPoint{Offset: n.rendered.TextLen()})
	if n.rendered.SelectedText(n.view.previewSelect.In(0)) == "" {
		t.Fatal("rendered preview cannot be selected")
	}
	s.Click(hitNotesPanel)
	if v.widthDragging || v.splitDragging || n.view.previewSelect.Dragging() {
		t.Fatal("close retained capture")
	}
	s.Click(hitNotesPanel)
	paint()
	if v.width < 499 || n.previewScroll <= 0 {
		t.Fatal("reopen reset geometry or scroll")
	}
}

func TestNotesSidebarImageResultCannotOverwriteAnotherPreview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer server.Close()
	s, _, _, _ := liveSession(t)
	s.notes.list = []savedNote{{ID: "image", Title: "Image", Body: "![image](" + server.URL + "/test.png)"}, {ID: "other", Title: "Other", Body: "other"}}
	s.activeSection = sectionTerminal
	s.Click(hitNotesPanel)
	s.noteTokens()
	s.Click(hitNoteRows.At(s.notes.view.noteTokens["image"]))
	var canvas gfx.Canvas
	s.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	if _, ok := hitByID(&canvas, hitNoteLoadImages); !ok {
		t.Fatal("image action missing")
	}
	ready := make(chan struct{}, 1)
	s.options.Invalidate = func() {
		select {
		case ready <- struct{}{}:
		default:
		}
	}
	s.Click(hitNoteLoadImages)
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("image result was not queued")
	}
	s.Click(hitNoteRows.At(s.notes.view.noteTokens["other"]))
	s.drainAI()
	n := &s.noteSidebarState().content
	if n.error != "" || n.imageSource != "" || len(n.images) != 0 || n.body.String() != "other" {
		t.Fatal("old image result overwrote current preview")
	}
	if s.notes.error != "" || len(s.notes.images) != 0 {
		t.Fatal("sidebar image result leaked to editor")
	}
}

func TestNotesSidebarUsesFullWorkspaceHeight(t *testing.T) {
	for _, mode := range []string{"ordinary", "workspace", "tabs"} {
		t.Run(mode, func(t *testing.T) {
			world, options := newWorld(t)
			s := newTestState(t, options)
			if mode == "ordinary" {
				connect(t, s, world)
			} else {
				id := launchTemporaryWorkspace(t, s, 0, 1)
				if mode == "tabs" {
					s.managedWorkspaceLayouts[id] = layoutcore.LayoutSpec{Kind: layoutcore.LayoutTabs}
				}
			}
			s.Click(hitNotesPanel)
			size := gfx.Size{Width: 1400, Height: 900}
			c, _ := paintAt(s, size)
			base, v := s.baseWorkspacePixelBounds(size), s.noteSidebarState()
			if v.bounds.Y != base.Y || v.bounds.Height != base.Height || v.resizeBounds.Y != base.Y || v.resizeBounds.Height != base.Height {
				t.Fatalf("sidebar=%+v resize=%+v base=%+v", v.bounds, v.resizeBounds, base)
			}
			close := panelHit(t, c, hitNoteSideClose)
			if close.Bounds.Y < base.Y || close.Bounds.Y+close.Bounds.Height > base.Y+40 {
				t.Fatalf("close button below sidebar header: %+v", close.Bounds)
			}
		})
	}
}

func TestNotesSidebarHeaderWrapsWithoutOverlappingControls(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	for _, width := range []float32{400, 280, 160, 90} {
		var c gfx.Canvas
		s.paintNoteSidebarTree(&c, gfx.Rect{Width: width, Height: 400})
		search := panelHit(t, &c, hitNoteSearch).Bounds
		linked := panelHit(t, &c, hitNoteSideLinked).Bounds
		close := panelHit(t, &c, hitNoteSideClose).Bounds
		if linked.X+linked.Width > close.X || close.X+close.Width > width || search.Width < 0 {
			t.Fatalf("width=%v search=%+v linked=%+v close=%+v", width, search, linked, close)
		}
		if width == 400 {
			if search.Y != linked.Y || search.X+search.Width > linked.X {
				t.Fatal("normal header overlaps or wraps")
			}
		} else if search.Y < linked.Y+linked.Height || s.noteSidebarState().content.listBounds.Y < search.Y+search.Height {
			t.Fatalf("wrapped header overlaps: search=%+v linked=%+v", search, linked)
		}
	}
}

func TestNotesSidebarCloseKeyPreservesBrowserState(t *testing.T) {
	for _, key := range []gfx.Key{gfx.KeyEnter, gfx.KeySpace} {
		s, _, _, _ := liveSession(t)
		// Closing now saves, and the save syncs the browser selection against
		// the library, so the selected note must actually exist.
		s.notes.list = []savedNote{{ID: "note", Body: "body"}}
		s.activeSection = sectionTerminal
		s.Click(hitNotesPanel)
		v := s.noteSidebarState()
		v.content.query.Insert("needle")
		v.content.sideAll = true
		v.content.selected = "note"
		v.content.view.treeScroll = 42
		v.content.previewScroll = 84
		v.width, v.fraction = 450, .6
		v.widthDragging, v.splitDragging = true, true
		size := gfx.Size{Width: 1400, Height: 900}
		before := s.workspacePixelBounds(size).Width
		result := s.noteSidebarKey(gfx.KeyEvent{Target: hitNoteSideClose, Key: key})
		if !result.Handled || s.notesPanelOpen() || v.widthDragging || v.splitDragging || s.workspacePixelBounds(size).Width <= before {
			t.Fatal("close did not hide sidebar and release its width/capture")
		}
		s.Click(hitNotesPanel)
		if s.noteSidebarState() != v || v.content.query.String() != "needle" || !v.content.sideAll || v.content.selected != "note" || v.content.view.treeScroll != 42 || v.content.previewScroll != 84 || v.width != 450 || v.fraction != .6 {
			t.Fatal("reopening lost browser state")
		}
	}
}
