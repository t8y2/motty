package workbench

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

func TestNotesScrollbarActivityLingersAndIsIndependent(t *testing.T) {
	var c gfx.Canvas
	var bar, idle noteScrollbar
	sample := func(at time.Duration, over bool, offset float32) float32 {
		c.SetClock(func() time.Duration { return at })
		c.BeginFrame()
		defer c.EndFrame()
		return bar.fade(&c, hitNoteListThumb, offset, over, true)
	}
	if sample(0, false, 0) != 0 {
		t.Fatal("idle bar is visible")
	}
	sample(time.Second, true, 0)
	if sample(1200*time.Millisecond, true, 0) <= 0 || sample(time.Minute, true, 0) != 1 {
		t.Fatal("hover did not keep the scrollbar visible")
	}
	if sample(time.Minute+500*time.Millisecond, false, 0) != 1 {
		t.Fatal("bar disappeared before the linger elapsed")
	}
	sample(time.Minute+2*time.Second, false, 0)
	if sample(time.Minute+3*time.Second, false, 0) != 0 {
		t.Fatal("bar did not fade out")
	}
	bar.activity = true // A wheel gesture at the edge still counts as activity.
	sample(time.Minute+4*time.Second, false, 0)
	if sample(time.Minute+4500*time.Millisecond, false, 0) <= 0 {
		t.Fatal("wheel did not reveal bar")
	}
	if idle.fade(&c, hitNotePreviewThumb, 0, false, true) != 0 {
		t.Fatal("activity leaked into another area")
	}
	if bar.fade(&c, hitNoteListThumb, 0, true, false) != 0 || bar.lit {
		t.Fatal("non-overflowing area has an active bar")
	}
}

func TestNotesPreviewScrollbarDragsWithoutChangingDraft(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.body.Insert(strings.Repeat("paragraph\n\n", 300))
	s.notes.view.mode = notePreview
	var c gfx.Canvas
	at := time.Duration(0)
	paint := func() {
		c.SetClock(func() time.Duration { return at })
		c.BeginFrame()
		s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
		c.EndFrame()
	}
	paint()
	if _, ok := hitByID(&c, hitNotePreviewThumb); ok {
		t.Fatal("idle preview has an invisible scrollbar hit")
	}
	b := s.notes.previewBounds
	s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: b.X + 20, Y: b.Y + 20})
	at = time.Second
	paint()
	at = 1200 * time.Millisecond
	paint()
	geo := s.notes.view.previewArea.Geometry()
	if geo.VerticalTrack.Width > 6 || geo.Viewport.Width != b.Width {
		t.Fatalf("preview is not a thin overlay: %+v", geo)
	}
	if _, ok := hitByID(&c, hitNotePreviewThumb); !ok {
		t.Fatal("hover did not reveal preview bar")
	}
	before := s.notes.body.String()
	x, y := geo.VerticalThumb.X+2, geo.VerticalThumb.Y+2
	s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Target: hitNotePreviewThumb, X: x, Y: y})
	s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: x, Y: geo.VerticalTrack.Y + geo.VerticalTrack.Height + 100})
	if s.notes.previewScroll != geo.MaxOffsetY || !s.notes.view.previewDragging {
		t.Fatal("dragging did not reach the bottom")
	}
	s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerUp, X: x, Y: geo.VerticalTrack.Y})
	if s.notes.previewScroll != 0 || s.notes.view.previewDragging || s.notes.body.String() != before {
		t.Fatal("drag release changed draft or failed to reach top")
	}

}

func TestNotesTreeAndPickerScrollbarsKeepNativeGeometryAndReset(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	for i := 0; i < 80; i++ {
		s.notes.list = append(s.notes.list, savedNote{ID: fmt.Sprint(i), Title: fmt.Sprint(i)})
	}
	var c gfx.Canvas
	c.BeginFrame()
	s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	c.EndFrame()
	geo := s.notes.view.listArea.Geometry()
	if geo.MaxOffsetY <= 0 || geo.VerticalTrack.Width > 6 {
		t.Fatal("tree is not using native thin scrollbar geometry")
	}
	// Both picker painters use the same result-area helper; inspect its native
	// geometry and verify that dismissing the picker clears its capture/activity.
	v := &s.notes.view
	v.popup = "tags"
	v.popupBar.activity, v.tagPopupDragging = true, true
	v.previewDragging = true
	s.Click(hitNoteDismiss)
	if v.tagPopupDragging || v.popupBar.activity {
		t.Fatal("dismiss retained picker scroll state")
	}
	s.selectNoteWithoutSave(s.notes.selected)
	if v.previewDragging || v.scrollDrag || v.previewBar.lit || v.treeBar.lit {
		t.Fatal("selecting a note retained scrollbar state")
	}
}

func TestNotesPickerOverlayDrags(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	var tags []string
	for i := 0; i < 80; i++ {
		tags = append(tags, fmt.Sprintf("long tag %02d", i))
	}
	s.notes.tags.Insert(strings.Join(tags, ","))
	v := &s.notes.view
	v.popup = "tags"
	v.documentBounds.Width = 600
	v.overlayAnchor = gfx.Rect{X: 500, Y: 50, Width: 28, Height: 28}
	var c gfx.Canvas
	paint := func(at time.Duration) {
		c.SetClock(func() time.Duration { return at })
		c.BeginFrame()
		s.paintNoteTagPopup(&c, gfx.Rect{Width: 1400, Height: 900})
		c.EndFrame()
	}
	paint(0)
	geo := v.tagPopupArea.Geometry()
	if geo.MaxOffsetY <= 0 || geo.VerticalTrack.Width > 6 {
		t.Fatal("picker did not use thin overlay geometry")
	}
	if _, ok := hitByID(&c, hitNoteTagPickerThumb); ok {
		t.Fatal("idle picker intercepts clicks on its hidden track")
	}
	s.notesScroll(gfx.ScrollEvent{X: geo.Viewport.X + 20, Y: geo.Viewport.Y + 20, DeltaY: 80})
	paint(time.Second)
	paint(1200 * time.Millisecond)
	if _, ok := hitByID(&c, hitNoteTagPickerThumb); !ok {
		t.Fatal("wheel did not reveal picker bar")
	}
	geo = v.tagPopupArea.Geometry()
	x, y := geo.VerticalThumb.X+2, geo.VerticalThumb.Y+2
	s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Target: hitNoteTagPickerThumb, X: x, Y: y})
	s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerUp, X: x, Y: geo.VerticalTrack.Y + geo.VerticalTrack.Height + 100})
	if v.tagPopupScroll != geo.MaxOffsetY || v.tagPopupDragging {
		t.Fatal("picker thumb did not reach bottom and release")
	}
}
