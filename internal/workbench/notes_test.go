package workbench

import (
	"archive/zip"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/backup"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNotesDraftIsSharedAndSurvivesReopening(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.EditorFor(hitNoteTitle).Insert("排障记录")
	s.EditorFor(hitNoteBody).Insert("# 检查\n\n运行 `uptime`。")
	s.Click(hitNotesPanel)
	s.save()
	reopened := newTestState(t, s.options)
	reopened.Click(hitNotes)
	if len(reopened.notes.list) != 1 || reopened.notes.list[0].Body != "# 检查\n\n运行 `uptime`。" || reopened.notes.list[0].Title != "排障记录" {
		t.Fatalf("notes did not survive: %+v", reopened.notes.list)
	}
}

func TestNotesSyncRetainsConcurrentBodiesOnce(t *testing.T) {
	a := newTestState(t, freshOptions(t))
	b := newTestState(t, freshOptions(t))
	a.Click(hitNotes)
	a.Click(hitNoteAdd)
	a.EditorFor(hitNoteTitle).Insert("Runbook")
	a.EditorFor(hitNoteBody).Insert("base")
	a.save()
	exchange := func(to, from *State) {
		t.Helper()
		records, err := from.config.Records()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = to.mergeRemote(backup.Document{Format: configFormat, Records: records}, false); err != nil {
			t.Fatal(err)
		}
	}
	exchange(b, a)
	b.Click(hitNotes)
	b.selectNote(a.notes.selected)
	a.EditorFor(hitNoteBody).Insert(" A")
	a.save()
	b.EditorFor(hitNoteBody).Insert(" B") // Still a draft when sync arrives.
	exchange(b, a)
	exchange(a, b)
	exchange(b, a)
	exchange(a, b)
	for _, s := range []*State{a, b} {
		bodies := map[string]bool{}
		for _, n := range s.notes.list {
			bodies[n.Body] = true
		}
		if len(s.notes.list) != 2 || !bodies["base A"] || !bodies["base B"] {
			t.Fatalf("concurrent notes lost or duplicated: %+v", s.notes.list)
		}
	}
	// A causally later edit must not create another conflict.
	a.selectNote(a.notes.list[0].ID)
	a.EditorFor(hitNoteBody).Insert(" later")
	a.save()
	exchange(b, a)
	if len(b.notes.list) != 2 {
		t.Fatalf("ordinary edit produced conflict: %+v", b.notes.list)
	}
}

func TestNotesFoldersMoveWithoutCyclesAndKeepContentsOnDelete(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteFolderAdd)
	s.EditorFor(hitNoteInline).Insert("Production")
	s.Key(gfx.KeyEvent{Target: hitNoteInline, Key: gfx.KeyEnter})
	parent := s.notes.folders[0].ID
	s.Click(hitNoteFolderRows.At(0))
	s.Click(hitNoteFolderAdd)
	s.EditorFor(hitNoteInline).Insert("Runbooks")
	s.Key(gfx.KeyEvent{Target: hitNoteInline, Key: gfx.KeyEnter})
	child := s.notes.folders[1].ID
	if s.moveNote(parent, child, "", true) {
		t.Fatal("allowed cyclic folder move")
	}
	s.Click(hitNoteAdd)
	id := s.notes.selected
	s.EditorFor(hitNoteBody).Insert("Keep this")
	if !s.moveNote(id, child, "", false) {
		t.Fatal("move rejected")
	}
	s.Click(hitNoteFolderRows.At(1))
	s.Click(hitNoteFolderRemove)
	s.Click(hitNoteConfirm)
	if len(s.notes.folders) != 1 || s.notes.list[0].Folder != parent || s.notes.list[0].Body != "Keep this" {
		t.Fatalf("folder deletion lost content: %+v", s.notes)
	}
	s.Click(hitNoteDelete)
	s.Click(hitNoteCancel)
	if len(s.notes.list) != 1 {
		t.Fatal("cancel deleted note")
	}
}

func TestNotesMarkdownExchangeAndOversizeDraft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Runbook.md")
	if err := os.WriteFile(path, []byte("# Steps\n\nCheck logs."), 0600); err != nil {
		t.Fatal(err)
	}
	o := freshOptions(t)
	o.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path, path}, nil }
	out := filepath.Join(dir, "notes.zip")
	o.SaveFile = func(app.SaveFileOptions) (string, error) { return out, nil }
	s := newTestState(t, o)
	s.Click(hitNotes)
	s.Click(hitNoteImport)
	s.Click(hitNoteExportAll)
	z, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if len(z.File) != 2 || z.File[0].Name == z.File[1].Name {
		t.Fatal("duplicate title overwrote export")
	}
	s.EditorFor(hitNoteBody).Insert(strings.Repeat("x", 1<<20))
	s.Click(hitNoteAdd)
	if len(s.notes.list) != 2 || s.notes.error == "" || !strings.Contains(s.EditorFor(hitNoteBody).String(), strings.Repeat("x", 1024)) {
		t.Fatal("oversize draft was lost")
	}
}
func TestDeletingAHostKeepsItsNotesAndRemovesAssociation(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.Click(hitNoteHostRows.At(s.noteTagToken(s.hosts[0].CatalogID)))
	s.EditorFor(hitNoteBody).Insert("Keep me")
	s.save()
	s.hostActionIDs = []uint32{s.hosts[0].ID}
	s.confirmDeleteHosts()
	if len(s.notes.list) != 1 || s.notes.list[0].Body != "Keep me" || noteHostCount(s.notes.list[0].Tags) != 0 {
		t.Fatalf("host deletion changed notes incorrectly: %+v", s.notes.list)
	}
}

func TestNotesRemoteImagesWaitForExplicitLoad(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusNotFound) }))
	defer server.Close()
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.EditorFor(hitNoteBody).Insert("![diagram](" + server.URL + "/image.png)")
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1200, Height: 800})
	if requests.Load() != 0 {
		t.Fatal("preview fetched image without consent")
	}
	s.Click(hitNoteLoadImages)
	deadline := time.Now().Add(2 * time.Second)
	for requests.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if requests.Load() != 1 {
		t.Fatal("explicit image load did not request image")
	}
}

func TestNotesRefreshProtectsAnotherWindowsDraftAndBackupRestoresNotes(t *testing.T) {
	a := newTestState(t, freshOptions(t))
	a.Click(hitNotes)
	a.Click(hitNoteAdd)
	a.EditorFor(hitNoteBody).Insert("base")
	a.save()
	b := newTestState(t, a.options)
	b.Click(hitNotes)
	a.EditorFor(hitNoteBody).Insert(" A")
	a.save()
	b.EditorFor(hitNoteBody).Insert(" B")
	if err := b.reloadShared(); err != nil {
		t.Fatal(err)
	}
	bodies := map[string]bool{}
	for _, n := range b.notes.list {
		bodies[n.Body] = true
	}
	if !bodies["base A"] || !bodies["base B"] {
		t.Fatalf("window refresh lost draft: %+v", b.notes.list)
	}
	document, err := b.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	restored := newTestState(t, freshOptions(t))
	if _, err = restored.applyBackup(document); err != nil {
		t.Fatal(err)
	}
	if len(restored.notes.list) != 2 {
		t.Fatal("backup did not restore notes")
	}
}

func TestNotesFailedSaveStillProtectsDraftOnRefresh(t *testing.T) {
	a := newTestState(t, freshOptions(t))
	a.Click(hitNotes)
	a.Click(hitNoteAdd)
	a.EditorFor(hitNoteBody).Insert("base")
	a.save()
	b := newTestState(t, a.options)
	b.Click(hitNotes)
	a.EditorFor(hitNoteBody).Insert(" A")
	a.save()
	b.EditorFor(hitNoteBody).Insert(" B")
	b.save()
	if err := b.reloadShared(); err != nil {
		t.Fatal(err)
	}
	bodies := map[string]bool{}
	for _, n := range b.notes.list {
		bodies[n.Body] = true
	}
	if !bodies["base A"] || !bodies["base B"] {
		t.Fatalf("failed save draft was replaced: %+v", b.notes.list)
	}
	for _, n := range b.notes.list {
		if n.Body == "base B" && !strings.Contains(n.Title, "conflict") {
			t.Fatal("conflict timestamp title missing")
		}
	}
}
func TestNotesConcurrentRevertsAreNotMistakenForLaterEdits(t *testing.T) {
	a := newTestState(t, freshOptions(t))
	b := newTestState(t, freshOptions(t))
	a.Click(hitNotes)
	a.Click(hitNoteAdd)
	replace := func(s *State, body string) {
		e := s.EditorFor(hitNoteBody)
		e.SetSelection(text.Range{End: e.RuneCount()})
		e.Insert(body)
		s.save()
	}
	exchange := func(to, from *State) {
		r, err := from.config.Records()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = to.mergeRemote(backup.Document{Format: configFormat, Records: r}, false); err != nil {
			t.Fatal(err)
		}
	}
	replace(a, "x")
	replace(a, "y")
	replace(a, "z")
	exchange(b, a)
	b.Click(hitNotes)
	b.selectNote(a.notes.selected)
	replace(a, "x")
	replace(b, "y")
	exchange(a, b)
	exchange(b, a)
	bodies := map[string]bool{}
	for _, n := range a.notes.list {
		bodies[n.Body] = true
	}
	if !bodies["x"] || !bodies["y"] {
		t.Fatalf("concurrent revert lost: %+v", a.notes.list)
	}
}

func TestNotesFailedTitleSaveSurvivesRefresh(t *testing.T) {
	a := newTestState(t, freshOptions(t))
	a.Click(hitNotes)
	a.Click(hitNoteAdd)
	a.EditorFor(hitNoteTitle).Insert("base")
	a.save()
	b := newTestState(t, a.options)
	b.Click(hitNotes)
	a.EditorFor(hitNoteTitle).Insert(" A")
	a.save()
	b.EditorFor(hitNoteTitle).Insert(" B")
	b.save()
	if err := b.reloadShared(); err != nil {
		t.Fatal(err)
	}
	if b.notes.list[0].Title != "base B" {
		t.Fatalf("pending title lost: %+v", b.notes.list)
	}
}

// A save that cannot reach the store reports itself, and the notice goes away
// as soon as a later save succeeds. The path that reports a refusal and the
// path that clears it share one wording, so a refusal from any of them can be
// cleared instead of leaving a stale warning over a note that is now stored.
func TestNotesSaveFailureNoticeClearsAfterRetry(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.EditorFor(hitNoteBody).Insert("first")
	s.save()
	s.pauseSave = 1
	s.EditorFor(hitNoteBody).Insert(" second")
	if s.selectNote("") {
		t.Fatal("note switch was accepted while the store was unavailable")
	}
	if s.notes.error != s.noteSaveFailure() {
		t.Fatalf("refusal notice: %q", s.notes.error)
	}
	s.pauseSave = 0
	s.save()
	if s.hasUnsavedNotes() {
		t.Fatalf("draft was not stored: %+v", s.notes.list)
	}
	if s.notes.error != "" {
		t.Fatalf("stale refusal notice over a stored note: %q", s.notes.error)
	}
}

// Text inside an IME composition is deliberately left unflushed, so pending
// text alone is not a failure. A save made in that state must not claim the
// notes could not be saved.
func TestNoteCompositionDoesNotReportSaveFailure(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.EditorFor(hitNoteBody).Insert("base")
	s.save()
	s.notes.body.SetMarkedText("输入", text.Range{End: 2})
	if !s.noteComposing() {
		t.Fatal("setup did not enter a composition")
	}
	s.save()
	if s.notes.error != "" {
		t.Fatalf("composition reported a save failure: %q", s.notes.error)
	}
}
