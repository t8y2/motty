package workbench

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

func TestNoteWindowSharesTabsAndReadsLiveDraftWithoutFileIO(t *testing.T) {
	host := &documentTestHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { host.Quit(); app.UseApplicationRunner(nil) })
	if err := app.RunApplication(func(application *app.Application) error {
		s := newTestState(t, testOptions(t))
		s.options.OpenContentWindow = application.OpenContentWindow
		s.notes.list = []savedNote{{ID: "note", Title: "Saved", Body: "old"}, {ID: "second", Title: "Second", Body: "other"}}
		s.selectNote("note")
		s.notes.body.Insert(" live")
		mainBody := s.notes.body.String()
		m := &s.fileWindows
		file := &fileDocument{owner: s, cacheKey: "file", name: "file.txt", ready: true, local: true}
		m.documents = map[string]*fileDocument{file.cacheKey: file}
		if err := m.present(file); err != nil {
			return err
		}
		s.openNoteWindow("note")
		d := m.activeDocument()
		if d == nil || d.note == nil || len(m.order) != 2 || d.window != file.window {
			t.Fatal("note did not share the existing window")
		}
		m.activate(file.cacheKey)
		s.noteTokens()
		s.noteSidebarPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitNoteRows.At(s.notes.view.noteTokens["note"]), ClickCount: 2})
		if m.activeDocument() != d {
			t.Fatal("double click did not activate note")
		}
		m.activate(file.cacheKey)
		s.noteSidebarState().content.selected = "note"
		s.clickNoteSidebar(hitNoteSideOpen)
		if m.activeDocument() != d {
			t.Fatal("open action did not activate note")
		}
		if len(m.order) != 2 {
			t.Fatal("duplicate note tab")
		}
		m.activate(file.cacheKey)
		s.noteSidebarState().content.selected = "second"
		s.clickNoteSidebar(hitNoteSideRowOpen.At(s.notes.view.noteTokens["note"]))
		if m.activeDocument() != d || len(m.order) != 2 || s.noteSidebarState().content.selected != "second" {
			t.Fatal("row open must activate the existing window without changing the preview")
		}
		var c gfx.Canvas
		m.paint(&c, gfx.Size{Width: 800, Height: 600})
		if d.note.content.body.String() != mainBody {
			t.Fatal("preview missed live draft")
		}
		for _, id := range []gfx.ID{hitDocumentInput, hitDocumentSave, hitDocumentReload, hitDocumentEncoding} {
			if _, ok := hitByID(&c, id); ok {
				t.Fatalf("file control leaked: %v", id)
			}
		}
		if _, ok := hitByID(&c, hitNotePreviewText); !ok {
			t.Fatal("missing selectable Markdown")
		}
		if d.textInput(app.TextInputContext{ID: hitDocumentInput}) != nil {
			t.Fatal("note exposes text input")
		}
		d.click(hitDocumentSave)
		d.shortcut(gfx.KeyEvent{Text: "s", Modifiers: keymap.Primary})
		if d.dirty() || s.notes.list[0].Body != mainBody || s.notes.body.String() != mainBody {
			t.Fatal("note save affected file editor or lost draft")
		}
		var copied string
		s.options.SetClipboardText = func(value string) error { copied = value; return nil }
		d.shortcut(gfx.KeyEvent{Target: hitNotePreviewText, Text: "a", Modifiers: keymap.Primary})
		d.shortcut(gfx.KeyEvent{Target: hitNotePreviewText, Text: "c", Modifiers: keymap.Primary})
		if !strings.Contains(copied, "live") {
			t.Fatalf("copy missed content: %q", copied)
		}
		s.notes.body.Insert(" updated")
		s.syncNoteWindows()
		if !strings.HasSuffix(d.note.content.body.String(), "updated") {
			t.Fatal("live update missing")
		}
		d.note.content.previewScroll = 17
		d.note.content.view.previewSelect.Select(components.TextPoint{}, components.TextPoint{Offset: 2})
		s.openNoteWindow("second")
		s.openNoteWindow("note")
		if m.activeDocument() != d || d.note.content.previewScroll != 17 || d.note.content.view.previewSelect.Focus.Offset != 2 {
			t.Fatal("tab reuse lost state")
		}
		s.notes.list = s.notes.list[1:]
		s.syncNoteWindows()
		c.Reset()
		m.paint(&c, gfx.Size{Width: 600, Height: 420})
		if !d.note.missing || d.note.content.body.String() != "" {
			t.Fatal("deleted note retained content")
		}
		if _, ok := hitByID(&c, hitNotePreviewText); ok {
			t.Fatal("deleted note has stale content hit")
		}
		m.closeActiveTab()
		if len(m.order) != 2 || m.documents["file"] != file || s.notes.selected != "note" {
			t.Fatal("closing note changed other documents")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNotesSidebarEmptyPreviewAndOpenEntry(t *testing.T) {
	s, _, _, _ := liveSession(t)
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	id := s.notes.selected
	s.activeSection = sectionTerminal
	s.Click(hitNotesPanel)
	s.noteSidebarState().content.sideAll = true
	var c gfx.Canvas
	paint := func() { c.Reset(); s.Paint(&c, gfx.Size{Width: 1200, Height: 800}) }
	paint()
	v := s.noteSidebarState()
	full := v.content.listBounds.Height
	if _, ok := hitByID(&c, hitNoteSideSplit); ok {
		t.Fatal("empty preview has split handle")
	}
	if _, ok := hitByID(&c, hitNoteSideOpen); ok {
		t.Fatal("empty preview has open action")
	}
	s.Click(hitNoteRows.At(s.notes.view.noteTokens[id]))
	v.fraction = .4
	paint()
	if _, ok := hitByID(&c, hitNoteSideOpen); !ok {
		t.Fatal("missing open action")
	}
	if v.content.listBounds.Height >= full || v.content.previewBounds.Height == 0 {
		t.Fatal("preview did not restore split")
	}
	v.content.query.Insert("no match")
	paint()
	if v.content.selected != id || v.content.previewBounds.Height == 0 {
		t.Fatal("filter closed preview")
	}
	s.notes.list = nil
	paint()
	if v.content.previewBounds.Height != 0 || v.fraction != .4 || v.content.listBounds.Height != full {
		t.Fatal("delete did not restore full directory")
	}
}

func TestNoteWindowDiscardsQueuedImageResultsAfterChangeAndClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer server.Close()
	host := &documentTestHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { host.Quit(); app.UseApplicationRunner(nil) })
	if err := app.RunApplication(func(application *app.Application) error {
		s := newTestState(t, testOptions(t))
		s.options.OpenContentWindow = application.OpenContentWindow
		s.notes.list = []savedNote{{ID: "image", Title: "Image", Body: "![image](" + server.URL + ")"}}
		s.openNoteWindow("image")
		d := s.fileWindows.activeDocument()
		queued := make(chan struct{}, 1)
		s.options.Invalidate = func() {
			select {
			case queued <- struct{}{}:
			default:
			}
		}
		load := func() {
			d.click(hitNoteLoadImages)
			select {
			case <-queued:
			case <-time.After(2 * time.Second):
				t.Fatal("image result not queued")
			}
		}
		load()
		s.notes.list[0].Body += " updated"
		s.syncNoteWindows()
		s.drainAI()
		if d.note.content.error != "" || d.note.content.imageSource != "" {
			t.Fatal("stale image changed updated preview")
		}
		load()
		s.fileWindows.closeActiveTab()
		s.openNoteWindow("image")
		s.drainAI()
		current := s.fileWindows.activeDocument()
		if !d.closed || d.note.content.imageToken == 0 || current == d || current.note.content.error != "" {
			t.Fatal("closed tab image result leaked")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
