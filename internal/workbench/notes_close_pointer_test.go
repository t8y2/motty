package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestNoteExitDialogPointer(t *testing.T) {
	for _, window := range []string{"main", "document"} {
		for _, action := range []struct {
			name string
			id   gfx.ID
		}{
			{"cancel", hitNoteExitCancel},
			{"close", hitNoteExitCancel},
			{"retry", hitNoteExitRetry},
			{"discard", hitNoteExitDiscard},
		} {
			t.Run(window+"/"+action.name, func(t *testing.T) {
				s := newTestState(t, testOptions(t))
				s.activeSection = sectionNotes
				s.notes.list = []savedNote{{ID: "a", Body: "saved"}}
				s.selectNote("a")
				s.save()
				paint, pointer := s.Paint, s.Pointer
				if window == "document" {
					d := noteEditingWindow(t, s, "a")
					paint, pointer = d.paint, d.pointer
				}
				s.pauseSave = 1
				s.notes.body.Insert(" pending")
				allowed := false
				if !s.ConfirmDocumentClose(func() { allowed = true }) || s.noteExit == nil {
					t.Fatal("missing unsaved notes dialog")
				}
				v, err := headless.Open(app.Config{Title: "Notes", Width: 1200, Height: 800, Tree: Tree(), Paint: paint, OnPointer: pointer}, nil, headless.Options{})
				if err != nil {
					t.Fatal(err)
				}
				defer v.Close()
				for _, kind := range []gfx.PointerEventKind{gfx.PointerDown, gfx.PointerUp} {
					if !pointer(gfx.PointerEvent{Kind: kind, Target: hitNoteAdd}).Handled {
						t.Fatal("background pointer event was not blocked")
					}
				}
				var hit gfx.Hit
				ok := false
				for _, candidate := range v.Snapshot().Hits {
					if candidate.ID == action.id && (candidate.Label == "Close dialog") == (action.name == "close") {
						hit, ok = candidate, true
						break
					}
				}
				if !ok {
					t.Fatal("dialog button missing")
				}
				if action.id == hitNoteExitRetry {
					s.pauseSave = 0
				}
				if err := v.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				if s.noteExit != nil {
					t.Fatal("dialog button click did not dismiss dialog")
				}
				if action.id == hitNoteExitCancel {
					if allowed || !s.hasUnsavedNotes() {
						t.Fatal("cancel did not retain draft and prevent closing")
					}
				} else if !allowed || s.hasUnsavedNotes() {
					t.Fatal("retry/discard did not resume closing")
				}
			})
		}
	}
}
