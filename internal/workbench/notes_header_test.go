package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestNotesLibraryHeaderIconsKeepModeIdentitiesAndPinState(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.body.Insert("shared draft")
	paint := func(width float32) *gfx.Canvas {
		t.Helper()
		c := &gfx.Canvas{}
		s.paintNoteEditor(c, gfx.Rect{Width: width, Height: 700})
		return c
	}
	c := paint(800)
	ids := []gfx.ID{hitNoteTreeToggle, hitNoteTitle, hitNoteTagsOpen, hitNoteHostsOpen, hitNotePin, hitNoteModes.At(0), hitNoteModes.At(2), hitNoteModes.At(1)}
	previous := float32(-1)
	for _, id := range ids {
		hit, ok := hitByID(c, id)
		if !ok || hit.Bounds.X <= previous || hit.Bounds.Y != 8 {
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
		c = paint(800)
		hit, _ := hitByID(c, hitNoteModes.At(token))
		count := 0
		for _, item := range c.Hits() {
			if item.ID == hit.ID {
				count++
			}
		}
		if s.notes.view.mode != mode || !hit.Selected || count != 1 || s.notes.body.String() != "shared draft" {
			t.Fatal("mode identity, focus stops or draft changed")
		}
	}
	s.Click(hitNotePin)
	c = paint(800)
	pinned, _ := hitByID(c, hitNotePin)
	if !pinned.Selected || pinned.Label != s.noteText("Unpin note", "取消置顶") {
		t.Fatal("pin state not reflected")
	}
	s.Click(hitNotePin)
	c = paint(800)
	unpinned, _ := hitByID(c, hitNotePin)
	if unpinned.Selected || unpinned.Label != s.noteText("Pin note", "置顶笔记") {
		t.Fatal("unpin state not reflected")
	}
}

func TestNotesLibraryHeaderStacksAndHidesEmptyTagArea(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.title.Insert(strings.Repeat("长标题", 30))
	var c gfx.Canvas
	s.paintNoteEditor(&c, gfx.Rect{Width: 380, Height: 700})
	title, _ := hitByID(&c, hitNoteTitle)
	tagButton, _ := hitByID(&c, hitNoteTagsOpen)
	lastMode, _ := hitByID(&c, hitNoteModes.At(1))
	if title.Bounds.Width < 120 || tagButton.Bounds.Y != title.Bounds.Y+36 || lastMode.Bounds.X+lastMode.Bounds.Width != 364 {
		t.Fatal("actions did not stack right aligned")
	}
	emptyY := s.notes.sourceBounds.Y
	s.setNoteTags([]string{"ordinary"})
	c.Reset()
	s.paintNoteEditor(&c, gfx.Rect{Width: 380, Height: 700})
	chip, _ := hitByID(&c, hitNoteTagHints.At(s.noteTagToken("ordinary")))
	if chip.Bounds.X != 20 || s.notes.sourceBounds.Y != emptyY+37 {
		t.Fatal("tag row is not full width or empty row consumes space")
	}
}

func TestNotesUpdatedTimeFitsToolbarAndImagesMoveIntoHeader(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.draft.Updated = "2026-11-05T04:21:00Z"
	paint := func(width float32) (*gfx.Canvas, *gfx.Text) {
		c := &gfx.Canvas{}
		c.SetMeasurer(func(value string, _, _ float32) float32 { return float32(len([]rune(value))) * 6 })
		s.paintNoteEditor(c, gfx.Rect{Width: width, Height: 700})
		for _, text := range c.Texts() {
			if text.Value == s.noteUpdatedLabel() {
				return c, &text
			}
		}
		return c, nil
	}
	c, stamp := paint(800)
	last, _ := hitByID(c, hitNoteFormats.At(len(noteFormatLabels)-1))
	if stamp == nil || stamp.Bounds.X < last.Bounds.X+last.Bounds.Width+12 || stamp.Bounds.Y+stamp.Bounds.Height/2 != last.Bounds.Y+last.Bounds.Height/2 || stamp.Bounds.X+stamp.Bounds.Width != 784 {
		t.Fatalf("timestamp does not align with toolbar: %+v", stamp)
	}
	if s.notes.sourceBounds.Y+s.notes.sourceBounds.Height != 684 {
		t.Fatal("main footer space was not reclaimed")
	}
	// At this width all tools fit, but the timestamp does not.
	c, stamp = paint(550)
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
	c, _ = paint(400)
	title, _ := hitByID(c, hitNoteTitle)
	image, _ = hitByID(c, hitNoteLoadImages)
	if image.Bounds.Y != title.Bounds.Y+36 {
		t.Fatal("image action did not stack with header actions")
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
