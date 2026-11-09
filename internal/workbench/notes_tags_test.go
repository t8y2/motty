package workbench

import (
	"fmt"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"slices"
	"strings"
	"testing"
)

func TestNotesTagsUseLibraryAndCurrentDraft(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	current := s.notes.selected
	s.setNoteTags([]string{"old", "shared"})
	s.Click(hitNoteAdd)
	s.setNoteTags([]string{"other", "shared"})
	s.selectNote(current)
	s.notes.tags.SetSelection(text.Range{End: s.notes.tags.RuneCount()})
	s.notes.tags.Insert("draft, shared")
	if got := s.noteTagCandidates(); !slices.Equal(got, []string{"draft", "other", "shared"}) {
		t.Fatalf("candidates: %v", got)
	}
	var initial gfx.Canvas
	s.Paint(&initial, gfx.Size{Width: 1000, Height: 700})
	s.Click(hitNoteTagsOpen)
	token := s.noteTagToken("other")
	s.notes.view.picker.Insert("OTHER")
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1000, Height: 700})
	if _, ok := hitByID(&c, hitNoteTagRows.At(token)); !ok {
		t.Fatalf("case insensitive search missed existing tag: area=%+v", s.notes.view.tagPopupArea.Geometry())
	}
	s.Click(hitNoteTagRows.At(token))
	if !slices.Contains(noteWords(s.notes.tags.String()), "other") || s.notes.view.popup != "tags" {
		t.Fatal("selection did not retain popover")
	}
	s.Click(hitNoteTagRows.At(s.noteTagToken("draft")))
	if slices.Contains(s.noteTagCandidates(), "draft") {
		t.Fatal("last removed association remains in candidates")
	}
	s.Click(hitNoteTagRemove.At(token))
	if slices.Contains(noteWords(s.notes.tags.String()), "other") {
		t.Fatal("chip did not remove the bound tag")
	}
	s.activeSection = sectionTerminal
	s.notes.panel = true
	if !slices.Equal(s.notes.draft.Tags, []string{"shared"}) {
		t.Fatal("entry points do not share tag edits")
	}
}

func TestNotesTagCreationCompositionAndDismissal(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.Click(hitNoteTagsOpen)
	s.notes.view.picker.Insert(" one, one, two ")
	s.notesKey(gfx.KeyEvent{Target: hitNotePicker, Key: gfx.KeyEnter})
	if !slices.Equal(s.notes.draft.Tags, []string{"one", "two"}) || s.notes.view.picker.String() != "" {
		t.Fatal("creation did not normalize or clear input")
	}
	s.notes.view.picker.SetMarkedText("输入", text.Range{End: 2})
	s.notesKey(gfx.KeyEvent{Target: hitNotePicker, Key: gfx.KeyEnter})
	if len(s.notes.draft.Tags) != 2 {
		t.Fatal("Enter created tag during composition")
	}
	r := s.notesKey(gfx.KeyEvent{Key: gfx.KeyEscape})
	if s.notes.view.popup != "" || r.Focus != hitNoteTagsOpen {
		t.Fatal("Escape did not restore focus")
	}
	s.Click(hitNoteTagsOpen)
	s.Click(hitNoteDismiss)
	if s.notes.view.popup != "" || s.notes.view.focus != hitNoteTagsOpen {
		t.Fatal("outside dismissal did not restore focus")
	}
}

func TestNotesTagsWrapWithoutOverlappingActions(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.setNoteTags([]string{"first", strings.Repeat("long tag ", 12), "last"})
	s.notes.view.tagsExpanded = true
	for _, width := range []float32{320, 480} {
		var c gfx.Canvas
		s.paintNoteEditor(&c, gfx.Rect{Width: width, Height: 600})
		first, _ := hitByID(&c, hitNoteTagRemove.At(s.noteTagToken("first")))
		last, ok := hitByID(&c, hitNoteTagRemove.At(s.noteTagToken("last")))
		if !ok || last.Bounds.Y <= first.Bounds.Y {
			t.Fatal("tags did not wrap into reachable rows")
		}
		for _, tag := range noteWords(s.notes.tags.String()) {
			hit, ok := hitByID(&c, hitNoteTagRemove.At(s.noteTagToken(tag)))
			if !ok || hit.Bounds.X < 20 || hit.Bounds.X+hit.Bounds.Width > width-20 {
				t.Fatal("tag removal exceeds the full-width tag area")
			}
		}
		if s.notes.sourceBounds.Y < last.Bounds.Y+last.Bounds.Height {
			t.Fatal("body overlaps wrapped tags")
		}
	}
}

func TestNotesTagLayoutPacksAndTruncates(t *testing.T) {
	var c gfx.Canvas
	items, height := layoutNoteTags(&c, []string{"one", "two", "an extremely long tag that cannot fit inside this row"}, 160, 12, 32)
	if items[0].bounds.Y != items[1].bounds.Y || items[1].bounds.X <= items[0].bounds.X {
		t.Fatal("short labels are not packed left to right")
	}
	if !items[2].truncated || items[2].bounds.Width > 160 || height <= 28 {
		t.Fatal("long tag is not constrained and wrapped")
	}
	if items[2].tag == items[2].label {
		t.Fatal("full tag value was lost on truncation")
	}
}

func TestNotesTagPopoverAdaptsAndScrollsResults(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	tags := []string{}
	for i := 0; i < 40; i++ {
		tags = append(tags, fmt.Sprintf("tag-%02d", i))
	}
	s.notes.list = append(s.notes.list, savedNote{ID: "other", Tags: tags})
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1000, Height: 700})
	s.Click(hitNoteTagsOpen)
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 1000, Height: 700})
	large := s.notes.view.tagPopupArea.Geometry()
	if large.MaxOffsetY <= 0 || large.Bounds.Height >= 420 {
		t.Fatal("tag results are not height limited")
	}
	first, _ := hitByID(&c, hitNoteTagRows.At(s.noteTagToken("tag-00")))
	second, _ := hitByID(&c, hitNoteTagRows.At(s.noteTagToken("tag-01")))
	if first.Bounds.Y != second.Bounds.Y || second.Bounds.X <= first.Bounds.X {
		t.Fatal("popup tags are not left aligned across a row")
	}
	s.notesScroll(gfx.ScrollEvent{DeltaY: 10000})
	s.notes.view.picker.Insert("tag-00")
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 1000, Height: 700})
	small := s.notes.view.tagPopupArea.Geometry()
	if small.OffsetY != 0 || small.Bounds.Height >= large.Bounds.Height {
		t.Fatal("filtered popup did not shrink and reset scrolling")
	}
	if _, ok := hitByID(&c, hitNoteTagRows.At(s.noteTagToken("tag-00"))); !ok {
		t.Fatal("filtered result is not reachable")
	}
}

func TestNotesTagPopupPositionsCreationAndDefaultOrder(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.setNoteTags([]string{"z-selected"})
	s.notes.list = append(s.notes.list, savedNote{ID: "other", Tags: []string{"a-unselected"}})
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	anchor, _ := c.HitBounds(hitNoteTagsOpen)
	s.Click(hitNoteTagsOpen)
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 1400, Height: 900})
	field, _ := hitByID(&c, hitNotePicker)
	add, ok := hitByID(&c, hitNoteTagSubmit)
	if !ok || add.Bounds.Y != field.Bounds.Y || add.Bounds.X <= field.Bounds.X+field.Bounds.Width {
		t.Fatal("add button is not beside input")
	}
	if field.Bounds.X < 0 || field.Bounds.X+field.Bounds.Width > 1400 || field.Bounds.Y < anchor.Y+anchor.Height || field.Bounds.Y > anchor.Y+anchor.Height+30 {
		t.Fatal("popup is not below trigger without header")
	}
	selected, _ := hitByID(&c, hitNoteTagRows.At(s.noteTagToken("z-selected")))
	other, _ := hitByID(&c, hitNoteTagRows.At(s.noteTagToken("a-unselected")))
	if other.Bounds.Y > selected.Bounds.Y || other.Bounds.Y == selected.Bounds.Y && other.Bounds.X >= selected.Bounds.X {
		t.Fatal("tag selection changed the default name order")
	}
}

func TestNotesTagLayoutPreservesFractionalWidthLabels(t *testing.T) {
	var c gfx.Canvas
	c.SetMeasurer(func(value string, _, _ float32) float32 {
		return float32(len([]rune(value))) * 5.1
	})
	for _, padding := range []float32{25.3, 32} {
		items, _ := layoutNoteTags(&c, []string{"a", "c", "f", "i", "x", "标签"}, 160, 12, padding)
		for _, item := range items {
			if item.truncated || item.label != item.tag {
				t.Fatalf("label %q incorrectly truncated to %q with padding %v", item.tag, item.label, padding)
			}
		}
		items, _ = layoutNoteTags(&c, []string{strings.Repeat("a", 100)}, 160, 12, padding)
		if !items[0].truncated || items[0].bounds.Width > 160 {
			t.Fatal("genuinely long label no longer truncates")
		}
	}
}

func TestNotesDetailTagsCollapseAndKeepRuntimePreference(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	tags := []string{"first", strings.Repeat("long ", 30), "last"}
	s.setNoteTags(tags)
	paint := func(width float32) *gfx.Canvas {
		c := &gfx.Canvas{}
		s.paintNoteEditor(c, gfx.Rect{Width: width, Height: 900})
		return c
	}
	c := paint(480)
	if _, ok := hitByID(c, hitNoteTagsExpand); !ok {
		t.Fatal("missing expand control")
	}
	if _, ok := hitByID(c, hitNoteTagRemove.At(s.noteTagToken("last"))); ok {
		t.Fatal("hidden tag still clickable")
	}
	collapsedBody := s.notes.sourceBounds.Y
	body := s.notes.body.String()
	s.Click(hitNoteTagsExpand)
	c = paint(480)
	if _, ok := hitByID(c, hitNoteTagRemove.At(s.noteTagToken("last"))); !ok || s.notes.sourceBounds.Y <= collapsedBody {
		t.Fatal("expand did not move body or expose tags")
	}
	if s.notes.body.String() != body {
		t.Fatal("expand changed draft")
	}
	s.Click(hitNoteTagsExpand)
	c = paint(480)
	if s.notes.sourceBounds.Y != collapsedBody {
		t.Fatal("collapse did not restore body")
	}
	// A single-line layout hides the control without changing the preference.
	s.Click(hitNoteTagsExpand)
	c = paint(10000)
	if _, ok := hitByID(c, hitNoteTagsExpand); ok || !s.notes.view.tagsExpanded {
		t.Fatal("wide layout changed preference")
	}
	c = paint(480)
	if _, ok := hitByID(c, hitNoteTagRemove.At(s.noteTagToken("last"))); !ok {
		t.Fatal("resize lost expanded state")
	}
	// The side uses the original layout, even when the library is collapsed.
	s.notes.view.tagsExpanded = false
	var side gfx.Canvas
	s.paintNoteTags(&side, gfx.Rect{Width: 280, Height: 28})
	if _, ok := hitByID(&side, hitNoteTagRemove.At(s.noteTagToken("last"))); !ok {
		t.Fatal("side labels were collapsed")
	}
	s.notes.view.tagsExpanded = true
	s.Click(hitNoteAdd)
	if s.notes.view.tagsExpanded {
		t.Fatal("new note kept previous expansion")
	}
}

func TestNotesDetailTagSpacingAndButtonReservation(t *testing.T) {
	var c gfx.Canvas
	labels := []string{"first", strings.Repeat("long ", 20), "last"}
	items, height, overflow := layoutNoteDetailTags(&c, labels, 200, 12, 32, 20, true)
	if !overflow || len(items) != 3 || height <= 28 {
		t.Fatal("missing expanded layout")
	}
	for _, item := range items {
		if item.bounds.X+item.bounds.Width > 166 {
			t.Fatal("tag overlaps expand button")
		}
	}
	if height != items[len(items)-1].bounds.Y+28 {
		t.Fatal("expanded layout has inconsistent bottom padding")
	}
	if items[1].bounds.Y-items[0].bounds.Y != 26 {
		t.Fatal("visual row gap is not six pixels")
	}
	collapsed, h, more := layoutNoteDetailTags(&c, labels, 200, 12, 32, 20, false)
	if !more || len(collapsed) != 1 || h != 28 {
		t.Fatal("collapsed layout", collapsed, h)
	}
	items, h, more = layoutNoteDetailTags(&c, []string{"one"}, 200, 12, 32, 20, false)
	if more || h != 28 || len(items) != 1 {
		t.Fatal("single label should not need a button")
	}
}
