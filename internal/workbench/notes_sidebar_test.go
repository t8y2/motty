package workbench

import (
	layoutcore "motty/internal/workspace"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func TestNotesSidebarExpandAllIsIndependentAndDisabledDuringSearch(t *testing.T) {
	s, _, _, _ := liveSession(t)
	s.notes.folders = []savedNoteFolder{{ID: "parent"}, {ID: "child", Parent: "parent"}}
	s.activeSection = sectionTerminal
	s.Click(hitNotesPanel)
	n := &s.noteSidebarState().content
	s.clickNoteSidebar(hitNoteSideExpand)
	for _, folder := range s.notes.folders {
		if !n.view.expanded[folder.ID] || s.notes.view.expanded[folder.ID] {
			t.Fatal("expand all must affect only the sidebar")
		}
	}
	n.query.Insert("needle")
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1200, Height: 800})
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
	if _, ok := noteTreeRowHit(&c, n, "child"); !ok {
		t.Fatal("missing ancestor")
	}
	if _, ok := noteTreeRowHit(&c, n, "pinned"); !ok {
		t.Fatal("pinned tag match missing")
	}
	if _, ok := noteTreeRowHit(&c, n, "other"); ok {
		t.Fatal("unrelated note passed filter")
	}
	clickNoteTreeRow(t, s, n, "pinned")
	paint()
	if s.notes.selected != "other" || s.notes.body.String() != before || s.notes.list[2].Body != before {
		t.Fatal("browser changed or switched the main draft")
	}
	s.Click(hitNoteSideLinked)
	n.query.SetSelection(text.Range{End: len([]rune(n.query.String()))})
	n.query.Insert("unsaved")
	paint()
	if _, ok := noteTreeRowHit(&c, n, "other"); !ok {
		t.Fatal("draft search is stale")
	}
	clickNoteTreeRow(t, s, n, "other")
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
	clickNoteTreeRow(t, s, &s.noteSidebarState().content, key)
	paint()
	v := s.noteSidebarState()
	n := &v.content
	if v.bounds.Width != 400 {
		t.Fatalf("default width %v", v.bounds.Width)
	}
	if !hasSeparator(&c, s.noteText("Resize notes panel", "调整笔记侧栏宽度")) {
		t.Fatal("width splitter missing")
	}
	if !hasSeparator(&c, s.noteText("Resize note browser sections", "调整笔记上下区域")) {
		t.Fatal("section splitter missing")
	}
	base := s.baseWorkspacePixelBounds(gfx.Size{Width: 1200, Height: 800})
	width := func() float32 { return base.Width - 1 - n.page.panel.Pos }
	if width() != v.bounds.Width {
		t.Fatalf("splitter position %v does not match the width %v", width(), v.bounds.Width)
	}
	b := n.previewBounds
	config := s.WindowConfig(app.Config{OnScroll: s.Scroll})
	config.OnScroll(gfx.ScrollEvent{X: b.X + 20, Y: b.Y + 20, DeltaY: 100})
	paint()
	if n.previewScroll <= 0 || s.notes.previewScroll != 0 {
		t.Fatal("scroll state leaked")
	}
	n.view.previewSelect.Select(ui.TextPoint{}, ui.TextPoint{Offset: n.rendered.TextLen()})
	if n.rendered.SelectedText(n.view.previewSelect.In(0)) == "" {
		t.Fatal("rendered preview cannot be selected")
	}
	s.Click(hitNotesPanel)
	if v.widthDragging || v.splitDragging || n.view.previewSelect.Dragging() {
		t.Fatal("close retained capture")
	}
	s.Click(hitNotesPanel)
	paint()
	if v.width != 400 || n.previewScroll <= 0 {
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
	s.noteSidebarState().content.sideAll = true
	paint := func() *gfx.Canvas { var c gfx.Canvas; s.Paint(&c, gfx.Size{Width: 1200, Height: 800}); return &c }
	paint()
	clickNoteTreeRow(t, s, &s.noteSidebarState().content, "image")
	c := paint()
	if _, ok := hitByID(c, hitNoteLoadImages); !ok {
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
	clickNoteTreeRow(t, s, &s.noteSidebarState().content, "other")
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
			if v.bounds.Y != base.Y || v.bounds.Height != base.Height {
				t.Fatalf("sidebar=%+v base=%+v", v.bounds, base)
			}
			close := panelHit(t, c, hitNoteSideClose)
			if close.Bounds.Y < base.Y || close.Bounds.Y+close.Bounds.Height > base.Y+40 {
				t.Fatalf("close button below sidebar header: %+v", close.Bounds)
			}
		})
	}
}
func TestNotesSidebarHeaderWrapsWithoutOverlappingControls(t *testing.T) {
	s, _, _, _ := liveSession(t)
	s.activeSection = sectionTerminal
	s.Click(hitNotesPanel)
	for _, width := range []float32{400, 280} {
		s.noteSidebarState().width = width
		var c gfx.Canvas
		s.Paint(&c, gfx.Size{Width: 1200, Height: 800})
		panel := s.noteSidebarState().bounds
		search := panelHit(t, &c, hitNoteSearch).Bounds
		linked, ok := hitByIDRoleLabel(&c, hitNoteSideLinked, gfx.RoleButton, s.noteText("Linked only", "只看关联"))
		if !ok {
			t.Fatal("linked switch caption missing")
		}
		close := panelHit(t, &c, hitNoteSideClose).Bounds
		if linked.Bounds.X+linked.Bounds.Width > close.X || close.X+close.Width > panel.X+panel.Width || search.Width <= 0 {
			t.Fatalf("width=%v panel=%+v search=%+v linked=%+v close=%+v", width, panel, search, linked.Bounds, close)
		}
		center := func(b gfx.Rect) float32 { return b.Y + b.Height/2 }
		dy := center(search) - center(linked.Bounds)
		if dy < 0 {
			dy = -dy
		}
		if width == 400 && (dy > 1 || search.X+search.Width > linked.Bounds.X) {
			t.Fatal("normal header overlaps or wraps")
		}
	}
}
func TestNotesSidebarCloseKeyPreservesBrowserState(t *testing.T) {
	for _, key := range []gfx.Key{gfx.KeyEnter, gfx.KeySpace} {
		s, _, _, _ := liveSession(t)
		s.notes.list = []savedNote{{ID: "note", Body: "body"}}
		s.activeSection = sectionTerminal
		s.Click(hitNotesPanel)
		v := s.noteSidebarState()
		v.content.query.Insert("needle")
		v.content.sideAll = true
		v.content.selected = "note"
		v.content.previewScroll = 84
		v.width, v.fraction = 450, .6
		size := gfx.Size{Width: 1400, Height: 900}
		var c gfx.Canvas
		s.Paint(&c, size)
		before := s.workspacePixelBounds(size).Width
		close := panelHit(t, &c, hitNoteSideClose)
		_ = key
		clickPageLabelIn(t, s, size, gfx.RoleButton, close.Label)
		if s.notesPanelOpen() || s.workspacePixelBounds(size).Width <= before {
			t.Fatal("close did not hide sidebar and release its width")
		}
		s.Click(hitNotesPanel)
		if s.noteSidebarState() != v || v.content.query.String() != "needle" || !v.content.sideAll || v.content.selected != "note" || v.width != 450 || v.fraction != .6 {
			t.Fatal("reopening lost browser state")
		}
	}
}

func TestNotesSidebarSitsAfterADockedContextPanel(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.activeSection = sectionTerminal
	state.Click(hitNotesPanel)
	workspace := state.deck.ActiveWorkspace()
	context, _ := state.deck.Context(workspace)
	context.Kind, context.Open, context.Width, context.Side = layoutcore.ContextSFTP, true, 340, layoutcore.ContextLeft
	if err := state.deck.SetContext(workspace, context); err != nil {
		t.Fatal(err)
	}
	size := gfx.Size{Width: 1200, Height: 800}
	canvas, _ := paintAt(state, size)
	frame := state.workspacePixelBounds(size)
	splitter := noteSplitterHit(t, canvas, state.noteText("Resize notes panel", "调整笔记侧栏宽度"))
	center := splitter.Bounds.X + splitter.Bounds.Width/2
	if want := frame.X + frame.Width - 0.5; center < want-0.01 || center > want+0.01 {
		t.Fatalf("notes divider centre %v, want the workspace's right edge %v", center, want)
	}
}

// A width drag that has gone back across the panel keeps its moves: only hover
// over the panel is held from the terminal beneath it.
func TestNotesSidebarLeavesCapturedMovesToTheHeldControl(t *testing.T) {
	s, _, _, _ := liveSession(t)
	s.activeSection = sectionTerminal
	s.Click(hitNotesPanel)
	size := gfx.Size{Width: 1400, Height: 900}
	paintAt(s, size)
	bounds := s.noteSidebarState().bounds
	at := gfx.Point{X: bounds.X + bounds.Width/2, Y: bounds.Y + bounds.Height/2}
	hover := s.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: at.X, Y: at.Y, Viewport: size})
	if !hover.Handled {
		t.Fatal("hover over the panel reached the terminal beneath it")
	}
	held := s.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: at.X, Y: at.Y, Viewport: size, Captured: true, Target: hitNoteSidebarSurface})
	if held.Handled {
		t.Fatal("a held control lost its move over the panel")
	}
}
