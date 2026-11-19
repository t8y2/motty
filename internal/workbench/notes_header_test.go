package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// paintNotesPage paints the notes section in a window wide enough for the
// editor to be editorWidth pixels.
func paintNotesPage(s *State, editorWidth float32) *gfx.Canvas {
	size := gfx.Size{Width: editorWidth + navigationWidth + 2*workspaceBorder + sectionCardMargin + 300 + 8, Height: 900}
	var c gfx.Canvas
	s.Paint(&c, size)
	return &c
}

// paintNoteWindow paints the note's own window at width.
func paintNoteWindowAt(t *testing.T, s *State, width float32) *gfx.Canvas {
	t.Helper()
	var c gfx.Canvas
	s.fileWindows.paint(&c, gfx.Size{Width: width, Height: 700})
	return &c
}

func TestNotesLibraryHeaderIconsKeepModeIdentitiesAndPinState(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.body.Insert("shared draft")
	c := paintNotesPage(s, 800)
	ids := []gfx.ID{hitNoteTreeToggle, hitNoteTitle, hitNoteTagsOpen, hitNoteHostsOpen, hitNotePin, hitNoteModes.At(0), hitNoteModes.At(2), hitNoteModes.At(1)}
	previous := float32(-1)
	for _, id := range ids {
		hit, ok := hitByID(c, id)
		if !ok || hit.Bounds.X <= previous {
			t.Fatalf("wrong header position: %v %+v", id, hit)
		}
		if id != hitNoteTitle && (hit.Hint == "" || hit.Bounds.Height != 28) {
			t.Fatalf("missing icon hint or incorrect height: %+v", hit)
		}
		previous = hit.Bounds.X
	}
	for token, mode := range []noteMode{noteSource, notePreview, noteSplit} {
		s.Click(hitNoteModes.At(token))
		if mode == notePreview && s.notes.view.focus != hitNoteModes.At(token) {
			t.Fatal("preview focus points to a hidden control")
		}
		c = paintNotesPage(s, 800)
		hit, _ := hitByID(c, hitNoteModes.At(token))
		count := 0
		for _, item := range c.Hits() {
			if item.ID == hit.ID {
				count++
			}
		}
		if s.notes.view.mode != mode || count != 1 || s.notes.body.String() != "shared draft" {
			t.Fatal("mode identity, focus stops or draft changed")
		}
	}
	s.Click(hitNotePin)
	c = paintNotesPage(s, 800)
	pinned, _ := hitByID(c, hitNotePin)
	if pinned.Label != s.noteText("Unpin note", "取消置顶") {
		t.Fatal("pin state not reflected")
	}
	s.Click(hitNotePin)
	c = paintNotesPage(s, 800)
	unpinned, _ := hitByID(c, hitNotePin)
	if unpinned.Label != s.noteText("Pin note", "置顶笔记") {
		t.Fatal("unpin state not reflected")
	}
}

func TestNotesWindowHeaderStacksRightAligned(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.notes.list = []savedNote{{ID: "a", Title: "A", Body: "body"}}
	s.activeSection = sectionNotes
	s.selectNote("a")
	d := noteEditingWindow(t, s, "a")
	d.note.content.title.Insert(strings.Repeat("长标题", 30))
	c := paintNoteWindowAt(t, s, 340)
	title, _ := hitByID(c, hitNoteTitle)
	tagButton, _ := hitByID(c, hitNoteTagsOpen)
	lastMode, _ := hitByID(c, hitNoteModes.At(1))
	if title.Bounds.Width < 120 || tagButton.Bounds.Y <= title.Bounds.Y || lastMode.Bounds.X+lastMode.Bounds.Width > 340-12 {
		t.Fatalf("actions did not stack right aligned: title %+v tags %+v mode %+v", title.Bounds, tagButton.Bounds, lastMode.Bounds)
	}
}
func TestNotesUpdatedTimeFitsToolbarAndImagesMoveIntoHeader(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.draft.Updated = "2026-11-05T04:21:00Z"
	paint := func(width float32) (*gfx.Canvas, *gfx.Text) {
		c := paintNotesPage(s, width)
		for _, text := range c.Texts() {
			if text.Value == s.noteUpdatedLabel() {
				return c, &text
			}
		}
		return c, nil
	}
	c, stamp := paint(800)
	last, _ := hitByID(c, hitNoteFormats.At(len(noteFormatLabels)-1))
	if stamp == nil || stamp.Bounds.X < last.Bounds.X+last.Bounds.Width+12 {
		t.Fatalf("timestamp does not follow the toolbar: %+v", stamp)
	}
	// At this width all tools fit, but the timestamp does not.
	c, stamp = paint(520)
	if stamp != nil {
		t.Fatal("timestamp should be hidden")
	}
	if _, ok := hitByID(c, hitNoteFormats.At(len(noteFormatLabels)-1)); !ok {
		t.Fatal("timestamp displaced a tool")
	}
	s.notes.body.Insert("![example](https://example.com/image.png)")
	c, _ = paint(800)
	pin, _ := hitByID(c, hitNotePin)
	image, ok := hitByID(c, hitNoteLoadImages)
	mode, _ := hitByID(c, hitNoteModes.At(0))
	if !ok || image.Bounds.X <= pin.Bounds.X || image.Bounds.X >= mode.Bounds.X || image.Bounds.Y != pin.Bounds.Y || image.Bounds.Width != 28 {
		t.Fatal("image icon is not between pin and modes")
	}
	c, _ = paint(360)
	image, ok = hitByID(c, hitNoteLoadImages)
	if !ok {
		t.Fatal("narrow header lost the image action")
	}
	s.notes.view.mode = notePreview
	c, stamp = paint(800)
	if stamp != nil {
		t.Fatal("preview shows timestamp")
	}
	if _, ok := hitByID(c, hitNoteLoadImages); !ok {
		t.Fatal("preview lost image action")
	}
}
