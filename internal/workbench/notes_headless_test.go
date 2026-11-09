//go:build darwin || windows || linux

package workbench

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"os"
	"testing"
)

func TestNotesNativeEditorPreviewAndComposition(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	var input app.TextInput
	v, err := headless.Open(app.Config{Title: "Notes", Width: 1200, Height: 800, Tree: Tree(), Paint: s.Paint, OnPointer: s.Pointer, OnKey: s.Key, OnScroll: s.Scroll, OnResize: s.Resize, TextInput: func(c app.TextInputContext) *app.TextInput {
		e := s.EditorFor(c.ID)
		if e == nil {
			return nil
		}
		input = app.TextInput{Editor: e}
		return &input
	}}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err = v.ClickLabel("Title"); err != nil {
		t.Fatal(err)
	}
	if err = v.Type("Runbook"); err != nil {
		t.Fatal(err)
	}
	if err = v.ClickLabel("Markdown source"); err != nil {
		t.Fatal(err)
	}
	if err = v.Type("# Steps\n\nCheck `uptime`."); err != nil {
		t.Fatal(err)
	}
	s.EditorFor(hitNoteBody).SetMarkedText("检查", text.Range{End: 2})
	s.Click(hitNoteAdd)
	if len(s.notes.list) != 1 {
		t.Fatal("composition was saved or discarded")
	}
	s.EditorFor(hitNoteBody).Unmark()
	s.save()
	if err = v.Rest(); err != nil {
		t.Fatal(err)
	}
	if err = v.ClickLabel("New folder"); err != nil {
		t.Fatal(err)
	}
	if err = v.ClickLabel("Name"); err != nil {
		t.Fatal(err)
	}
	if err = v.Type("Archive"); err != nil {
		t.Fatal(err)
	}
	if err = v.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	if err = v.Rest(); err != nil {
		t.Fatal(err)
	}
	row, ok := hitByIDSnapshot(v.Snapshot().Hits, hitNoteRows.At(0))
	if !ok {
		t.Fatal("note row missing")
	}
	folder, ok := hitByIDSnapshot(v.Snapshot().Hits, hitNoteFolderRows.At(0))
	if !ok {
		t.Fatal("folder missing")
	}
	x, y := row.Bounds.X+row.Bounds.Width/2, row.Bounds.Y+row.Bounds.Height/2
	tx, ty := folder.Bounds.X+folder.Bounds.Width/2, folder.Bounds.Y+folder.Bounds.Height/2
	if err = v.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if err = v.Move(tx, ty); err != nil {
		t.Fatal(err)
	}
	if err = v.Release(tx, ty); err != nil {
		t.Fatal(err)
	}
	if s.notes.list[0].Folder != s.notes.folders[0].ID {
		t.Fatal("physical drag did not move note")
	}
	for _, id := range []gfx.ID{hitNoteBody, hitNoteTitle, hitNotePin} {
		h, ok := hitByIDSnapshot(v.Snapshot().Hits, id)
		if !ok || h.Bounds.X < 0 || h.Bounds.Y < 0 || h.Bounds.X+h.Bounds.Width > 1200 || h.Bounds.Y+h.Bounds.Height > 800 {
			t.Fatalf("control clipped: %v %+v", id, h.Bounds)
		}
	}

	if path := os.Getenv("MOTTY_NOTES_SCREENSHOT"); path != "" {
		if err = v.Screenshot(path); err != nil {
			t.Fatal(err)
		}
	}
	if !slicesNoteHit(v.Snapshot().Hits, hitNoteBody) {
		t.Fatal("native source editor missing")
	}
}
func slicesNoteHit(hits []gfx.Hit, id gfx.ID) bool {
	for _, h := range hits {
		if h.ID == id {
			return true
		}
	}
	return false
}
