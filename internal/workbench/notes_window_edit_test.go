package workbench

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func noteEditingWindow(t *testing.T, s *State, id string) *fileDocument {
	t.Helper()
	d := &fileDocument{owner: s, cacheKey: "note:" + id, ready: true, local: true, note: &noteWindow{id: id}}
	if s.fileWindows.documents == nil {
		s.fileWindows.documents = map[string]*fileDocument{}
	}
	s.fileWindows.documents[d.cacheKey] = d
	s.fileWindows.active = d.cacheKey
	s.fileWindows.order = append(s.fileWindows.order, d.cacheKey)
	d.syncNoteWindow()
	return d
}
func noteTextEvent(t *testing.T, d *fileDocument, id gfx.ID, event app.TextEvent) app.TextState {
	t.Helper()
	d.owner.fileWindows.active = d.cacheKey
	input := d.textInput(app.TextInputContext{ID: id, NavigationKeys: true})
	if input == nil || input.OnEvent == nil {
		t.Fatal("missing note native input")
	}
	state, err := input.OnEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
func TestNoteWindowsEditIndependentNotesAndShareUndo(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.notes.list = []savedNote{{ID: "a", Title: "A", Body: "alpha"}, {ID: "b", Title: "B", Body: "beta"}}
	s.activeSection = sectionNotes
	s.selectNote("a")
	b := noteEditingWindow(t, s, "b")
	b.clickNoteWindow(hitNoteModes.At(0))
	noteTextEvent(t, b, hitNoteBody, app.TextEvent{Kind: app.TextInsert, Text: " window"})
	if s.notes.selected != "a" || s.notes.body.String() != "alpha" || s.noteBrowserNote(s.notes.list[1]).Body != "beta window" {
		t.Fatal("window edit selected or changed main note")
	}
	s.fileWindows.active = ""
	a := noteEditingWindow(t, s, "a")
	a.clickNoteWindow(hitNoteModes.At(0))
	main := s.NotesInput(app.TextInputContext{ID: hitNoteBody})
	main.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: " main"})
	s.notes.body.BreakUndoGroup()
	noteTextEvent(t, a, hitNoteBody, app.TextEvent{Kind: app.TextInsert, Text: " window"})
	if &a.note.content.body != &s.notes.body || s.notes.body.String() != "alpha main window" {
		t.Fatal("same note did not share editor")
	}
	stale := a.textInput(app.TextInputContext{ID: hitNoteBody}).OnEvent
	s.fileWindows.active = b.cacheKey
	stale(app.TextEvent{Kind: app.TextInsert, Text: "stale tab"})
	if s.notes.body.String() != "alpha main window" {
		t.Fatal("inactive tab accepted queued input")
	}
	main = s.NotesInput(app.TextInputContext{ID: hitNoteBody})
	main.OnEvent(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
	if a.note.content.body.String() != "alpha main" {
		t.Fatal("cross-view undo did not undo latest note edit")
	}
	noteTextEvent(t, b, hitNoteBody, app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
	if b.note.content.body.String() != "beta" || s.notes.body.String() != "alpha main" {
		t.Fatal("undo leaked between notes")
	}
	s.save()
	if s.hasUnsavedNotes() || s.notes.list[0].Body != "alpha main" || s.notes.list[1].Body != "beta" {
		t.Fatal("multi-draft save failed")
	}
}

func TestNoteWindowSelectionsCompositionAndStaleEvents(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.notes.list = []savedNote{{ID: "a", Title: "A", Body: "abcdef"}}
	s.activeSection = sectionNotes
	s.selectNote("a")
	s.activateNoteView(&s.notes)
	s.notes.body.SetSelection(text.Range{Start: 4, End: 2})
	d := noteEditingWindow(t, s, "a")
	d.note.content.view.mode = noteSource
	noteTextEvent(t, d, hitNoteBody, app.TextEvent{Kind: app.TextSetSelection, Selection: app.TextRange{Start: 6, End: 6}})
	noteTextEvent(t, d, hitNoteBody, app.TextEvent{Kind: app.TextInsert, Text: "!"})
	s.activateNoteView(&s.notes)
	if s.notes.body.Caret() != 2 {
		t.Fatal("another window moved main caret")
	}
	input := s.NotesInput(app.TextInputContext{ID: hitNoteBody})
	if !input.State.SelectionReversed {
		t.Fatal("view activation reversed a backwards selection")
	}
	input.OnEvent(app.TextEvent{Kind: app.TextSetMarked, Text: "输入", MarkedSelection: app.TextRange{Start: 2, End: 2}})
	if d.textInput(app.TextInputContext{ID: hitNoteBody}) != nil {
		t.Fatal("another view took composing editor")
	}
	input = s.NotesInput(app.TextInputContext{ID: hitNoteBody})
	input.OnEvent(app.TextEvent{Kind: app.TextUnmark})
	stale := d.textInput(app.TextInputContext{ID: hitNoteBody}).OnEvent
	s.notes.list = nil
	d.syncNoteWindow()
	stale(app.TextEvent{Kind: app.TextInsert, Text: "resurrect"})
	s.save()
	if len(s.notes.list) != 0 || !d.note.missing {
		t.Fatal("stale native event restored deleted note")
	}
}

func TestNoteWindowBottomLayoutModesAndPopovers(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.notes.list = []savedNote{{ID: "a", Title: "A", Body: "# header\n![image](https://example.com/a.png)", Tags: []string{"tag"}}}
	d := noteEditingWindow(t, s, "a")
	var c gfx.Canvas
	paint := func(w float32) { c.Reset(); d.paint(&c, gfx.Size{Width: w, Height: 600}) }
	paint(900)
	title, _ := hitByID(&c, hitNoteTitle)
	if title.Bounds.Y < 500 {
		t.Fatal("title not at bottom")
	}
	if _, ok := hitByID(&c, hitNoteFormats.At(0)); ok {
		t.Fatal("preview has format toolbar")
	}
	if _, ok := hitByID(&c, hitNoteTagRemove.At(0)); ok {
		t.Fatal("selected tag strip leaked into window")
	}
	previous := float32(0)
	for _, id := range []gfx.ID{hitNoteTagsOpen, hitNoteHostsOpen, hitNotePin, hitNoteLoadImages, hitNoteModes.At(0), hitNoteModes.At(2), hitNoteModes.At(1)} {
		h, ok := hitByID(&c, id)
		if !ok || h.Bounds.X <= previous {
			t.Fatalf("missing or misordered action %v", id)
		}
		previous = h.Bounds.X
	}
	d.clickNoteWindow(hitNoteModes.At(2))
	paint(900)
	body, _ := hitByID(&c, hitNoteBody)
	format, _ := hitByID(&c, hitNoteFormats.At(0))
	if body.Bounds.Height == 0 || format.Bounds.Y >= body.Bounds.Y || d.note.content.previewBounds.Height == 0 {
		t.Fatal("split/editor/format layout missing")
	}
	d.clickNoteWindow(hitNoteTagsOpen)
	paint(900)
	field, ok := hitByID(&c, hitNotePicker)
	if !ok || field.Bounds.Y >= title.Bounds.Y {
		t.Fatal("bottom tag popup did not open upwards")
	}
	d.noteWindowKey(gfx.KeyEvent{Key: gfx.KeyEscape})
	paint(380)
	if d.note.content.view.mode != noteSplit || d.note.content.previewBounds.Width != 0 {
		t.Fatal("narrow split did not degrade without changing preference")
	}
	title, _ = hitByID(&c, hitNoteTitle)
	tags, _ := hitByID(&c, hitNoteTagsOpen)
	if tags.Bounds.Y <= title.Bounds.Y {
		t.Fatal("narrow footer did not wrap")
	}
	paint(900)
	if d.note.content.previewBounds.Width == 0 || s.notes.view.mode == noteSplit {
		t.Fatal("window mode restoration changed main preference")
	}
}

func TestNoteWindowAutosavesWithoutMainEditorAndRetainsFailedDraft(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.notes.list = []savedNote{{ID: "a", Title: "A", Body: "body"}}
	d := noteEditingWindow(t, s, "a")
	d.note.content.view.mode = noteSource
	noteTextEvent(t, d, hitNoteBody, app.TextEvent{Kind: app.TextInsert, Text: " unsaved"})
	var c gfx.Canvas
	c.SetClock(func() time.Duration { return time.Second })
	c.BeginFrame()
	c.BeginEventPass(gfx.ID{})
	d.paint(&c, gfx.Size{Width: 800, Height: 600})
	c.EndEventPass()
	c.EndFrame()
	c.RunAfterPaint()
	c.Reset()
	c.SetClock(func() time.Duration { return 2 * time.Second })
	c.BeginFrame()
	c.BeginEventPass(gfx.ID{})
	d.paint(&c, gfx.Size{Width: 800, Height: 600})
	c.EndEventPass()
	c.EndFrame()
	c.RunAfterPaint()
	if s.hasUnsavedNotes() || s.notes.list[0].Body != "body unsaved" {
		t.Fatal("window-only edit did not autosave")
	}
	s.pauseSave = 1
	noteTextEvent(t, d, hitNoteBody, app.TextEvent{Kind: app.TextInsert, Text: " retained"})
	stale := d.textInput(app.TextInputContext{ID: hitNoteBody}).OnEvent
	d.requestClose(func() { d.dispose() })
	stale(app.TextEvent{Kind: app.TextInsert, Text: " stale"})
	if !s.hasUnsavedNotes() || !strings.HasSuffix(s.noteBuffer("a").body.String(), "retained") {
		t.Fatal("failed draft lost at close")
	}
	s.pauseSave = 0
	s.save()
	if s.hasUnsavedNotes() || !strings.HasSuffix(s.notes.list[0].Body, "retained") {
		t.Fatal("retained draft could not retry")
	}
}

func TestNoteCloseFailureRetryCancelAndDiscard(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.activeSection = sectionNotes
	s.notes.list = []savedNote{{ID: "a", Body: "saved"}}
	s.selectNote("a")
	s.pauseSave = 1
	s.notes.body.Insert(" pending")
	allowed := false
	if !s.ConfirmDocumentClose(func() { allowed = true }) || allowed || s.noteExit == nil {
		t.Fatal("failed notes did not block quit")
	}
	s.clickNoteExit(hitNoteExitCancel)
	if s.noteExit != nil || allowed || !s.hasUnsavedNotes() {
		t.Fatal("cancel discarded changes")
	}
	s.ConfirmDocumentClose(func() { allowed = true })
	s.pauseSave = 0
	s.clickNoteExit(hitNoteExitRetry)
	if !allowed || s.hasUnsavedNotes() {
		t.Fatal("retry did not resume close")
	}
	s.pauseSave = 1
	s.notes.body.Insert(" discard")
	allowed = false
	s.ConfirmDocumentClose(func() { allowed = true })
	s.clickNoteExit(hitNoteExitDiscard)
	if !allowed || s.hasUnsavedNotes() || s.notes.body.String() != "saved pending" {
		t.Fatal("discard did not restore saved note")
	}
}

func TestNoteWindowAssociationsFollowCatalogChanges(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.notes.list = []savedNote{{ID: "a", Tags: []string{"ordinary", encodeNoteHostGroup("A/DB")}}}
	d := noteEditingWindow(t, s, "a")
	d.note.content.view.mode = noteSource
	noteTextEvent(t, d, hitNoteBody, app.TextEvent{Kind: app.TextInsert, Text: "pending"})
	h := s.hosts[0]
	id := h.CatalogID
	s.withNoteView(&d.note.content, func() { s.toggleNoteHost(id) })
	if !noteHasHost(noteWords(d.note.content.tags.String()), id) {
		t.Fatal("window host selection missed shared draft")
	}
	s.unlinkNoteHost(id)
	if noteHasHost(noteWords(d.note.content.tags.String()), id) || !slices.Contains(noteWords(d.note.content.tags.String()), "ordinary") {
		t.Fatal("host cleanup missed window draft")
	}
	s.updateNoteBufferTags("a", []string{"ordinary", encodeNoteHostGroup("B/DB")})
	if !noteHasGroup(noteWords(d.note.content.tags.String()), "B/DB") || d.note.content.body.String() != "pending" {
		t.Fatal("group update overwrote body")
	}
}

func TestNoteWindowInputQueriesDoNotStealEditingOrBreakTypingUndo(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.activeSection = sectionNotes
	s.notes.list = []savedNote{{ID: "a", Body: "base"}}
	s.selectNote("a")
	d := noteEditingWindow(t, s, "a")
	d.note.content.view.mode = noteSource
	noteTextEvent(t, d, hitNoteBody, app.TextEvent{Kind: app.TextInsert, Text: "x"})
	for range 3 {
		if s.NotesInput(app.TextInputContext{ID: hitNoteBody}) == nil {
			t.Fatal("main input missing")
		}
	}
	if d.note.content.owner != &d.note.content {
		t.Fatal("background input query claimed editor")
	}
	noteTextEvent(t, d, hitNoteBody, app.TextEvent{Kind: app.TextInsert, Text: "y"})
	noteTextEvent(t, d, hitNoteBody, app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
	if d.note.content.body.String() != "base" {
		t.Fatal("background query split continuous typing undo")
	}
}

func TestNoteWindowConflictRefreshPreservesOtherDraftAndAssociations(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.activeSection = sectionNotes
	s.notes.list = []savedNote{{ID: "a", Title: "A", Body: "alpha", Revision: "a0"}, {ID: "b", Title: "B", Body: "beta", Revision: "b0"}}
	s.selectNote("a")
	s.save()
	d := noteEditingWindow(t, s, "b")
	d.note.content.view.mode = noteSource
	noteTextEvent(t, d, hitNoteBody, app.TextEvent{Kind: app.TextInsert, Text: " local"})
	incoming := slices.Clone(s.notes.list)
	incoming[1].Body = "remote"
	incoming[1].Revision = "remote-r"
	s.restoreNotes(incoming, nil)
	if d.note.id == "b" || d.note.content.selected != d.note.id || s.notes.selected != "a" {
		t.Fatal("conflict did not rebind only its window")
	}
	s.flushNoteDraft()
	if s.notes.list[s.noteIndex("b")].Body != "remote" || d.note.content.body.String() != "beta local" || !strings.Contains(d.note.content.title.String(), "conflict") {
		t.Fatal("window conflict lost local or remote content")
	}
	old := d.note.content.noteDraft
	s.fileWindows.active = d.cacheKey
	s.withNoteView(&d.note.content, func() { s.setNoteTags([]string{"after conflict"}) })
	if d.note.content.noteDraft != old || s.noteIndex(d.note.id) < 0 {
		t.Fatal("conflict lost buffer identity")
	}
}

func TestNoteMetadataRefreshPreservesSharedUndo(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.title.Insert("Saved title")
	s.replaceNoteTagEditor([]string{"saved-tag"})
	s.save()
	window := noteEditingWindow(t, s, s.notes.selected)
	s.notes.body.Insert("pending body")
	incoming := slices.Clone(s.notes.list)
	incoming[0].Title = "Remote title"
	incoming[0].Tags = []string{"remote-tag"}
	s.restoreNotes(incoming, s.notes.folders)
	if window.note.content.noteDraft != s.notes.noteDraft || s.notes.body.String() != "pending body" {
		t.Fatal("refresh replaced the shared draft or lost body edits")
	}
	for _, test := range []struct {
		name, want string
		editor     *text.Editor
	}{{"title", "Saved title", &s.notes.title}, {"tags", "saved-tag", &s.notes.tags}} {
		t.Run(test.name, func(t *testing.T) {
			if !test.editor.Undo() || test.editor.String() != test.want {
				t.Fatalf("undo after remote refresh = %q, want %q", test.editor.String(), test.want)
			}
		})
	}
}
