package workbench

import (
	"fmt"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestNotesPopupSizing(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.activeSection = sectionNotes
	for _, test := range []struct{ doc, window, want float32 }{{1200, 1400, 600}, {400, 800, 340}, {400, 300, 276}} {
		s.notes.view.documentBounds.Width = test.doc
		w, h := s.notePopupSize(gfx.Rect{Width: test.window, Height: 800})
		if w != test.want || h < 479 || h > 481 {
			t.Fatalf("size %v %v", w, h)
		}
	}
	s.notes.view.popup = "hosts"
	s.notes.view.documentBounds.Width = 1200
	for _, test := range []struct{ window, want float32 }{{1400, 1200}, {1000, 976}} {
		w, _ := s.notePopupSize(gfx.Rect{Width: test.window, Height: 800})
		if w != test.want {
			t.Fatalf("host popup width: %v, want %v", w, test.want)
		}
	}
}

func TestNotesHostPickerUsesIndependentOrderedGroups(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.hosts = []Host{{CatalogID: "b", Label: "Beta", Group: "Prod/DB", User: "operator"}, {CatalogID: "a", Label: "Alpha", Group: "Prod/DB"}, {CatalogID: "root", Label: "Root"}, {CatalogID: "dev", Label: "Development", Group: "Dev", Address: "dev.example"}}
	s.hostGroups = []hostGroup{{Name: "Prod", Pinned: true, Expanded: false}, {Name: "Prod/DB", Expanded: false}, {Name: "Dev", Expanded: false}, {Name: "Empty", Explicit: true}}
	s.hostFilterOn = true
	s.hostFilter = "Dev"
	items, _ := s.layoutNoteHostPicker(400, "")
	keys := []string{}
	for _, item := range items {
		keys = append(keys, item.key)
	}
	if !slices.Equal(keys, []string{"", "root", "Prod", "Prod/DB", "b", "a", "Dev", "dev", "Empty"}) {
		t.Fatalf("order: %v", keys)
	}
	var beta, alpha gfx.Rect
	for _, item := range items {
		if item.key == "b" {
			beta = item.bounds
		}
		if item.key == "a" {
			alpha = item.bounds
		}
	}
	if beta.Y != alpha.Y || beta.X >= alpha.X || beta.Width < 180 {
		t.Fatal("cards did not form columns")
	}
	s.notes.view.hostPickerCollapsed = map[string]bool{"Prod": true}
	collapsed, _ := s.layoutNoteHostPicker(400, "")
	for _, item := range collapsed {
		if item.key == "b" || item.key == "Prod/DB" {
			t.Fatal("collapsed subtree visible")
		}
	}
	for _, query := range []string{"OPERATOR", "Prod/DB", "dev.example", "Alpha"} {
		found, _ := s.layoutNoteHostPicker(400, query)
		if len(found) == 0 {
			t.Fatalf("query %q missed matches", query)
		}
	}
	searched, _ := s.layoutNoteHostPicker(400, "operator")
	keys = nil
	for _, item := range searched {
		keys = append(keys, item.key)
	}
	if !slices.Equal(keys, []string{"Prod", "Prod/DB", "b"}) || !s.notes.view.hostPickerCollapsed["Prod"] {
		t.Fatal("search modified collapse state or omitted ancestors")
	}
	if s.hostGroups[0].Expanded || !s.hostFilterOn || s.hostFilter != "Dev" {
		t.Fatal("picker modified hosts page")
	}
}

func TestNotesHostPickerCheckboxScrollAndFocus(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	for i := 0; i < 60; i++ {
		s.hosts = append(s.hosts, Host{CatalogID: fmt.Sprintf("host-%02d", i), Label: fmt.Sprintf("Host %02d", i), Group: "Group"})
	}
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1400, Height: 800})
	s.Click(hitNoteHostsOpen)
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 1400, Height: 800})
	token := s.noteTagToken("host-00")
	id := hitNoteHostRows.At(token)
	hit, ok := hitByID(&c, id)
	if !ok || hit.Role != gfx.RoleCheckbox || hit.Bounds.Height != 40 || hit.Label != "Host 00" {
		t.Fatalf("card semantics: %+v", hit)
	}
	count := 0
	for _, h := range c.Hits() {
		if h.ID == id {
			count++
		}
	}
	if count != 1 {
		t.Fatal("card has duplicate checkbox targets")
	}
	geo := s.notes.view.tagPopupArea.Geometry()
	if geo.MaxOffsetY <= 0 || geo.Bounds.Height >= 480 {
		t.Fatal("results do not scroll below height cap")
	}
	s.Click(id)
	if !noteHasHost(s.notes.draft.Tags, "host-00") || s.notes.view.popup != "hosts" || len(s.sessions) != 0 {
		t.Fatal("card did not select or opened connection")
	}
	s.notesScroll(gfx.ScrollEvent{DeltaY: 10000})
	s.notes.view.picker.Insert("Host 00")
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 1400, Height: 800})
	if s.notes.view.tagPopupScroll != 0 || s.notes.view.tagPopupArea.Geometry().Bounds.Height >= geo.Bounds.Height {
		t.Fatal("query did not shrink popup/reset scroll")
	}
	s.Click(id)
	if noteHasHost(s.notes.draft.Tags, "host-00") {
		t.Fatal("second click did not deselect")
	}
	s.notes.view.picker.SetSelection(text.Range{End: s.notes.view.picker.RuneCount()})
	s.notes.view.picker.Insert("")
	s.Click(hitNoteHostGroups.At(s.noteTagToken("Group")))
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 1400, Height: 800})
	if _, ok := hitByID(&c, id); ok {
		t.Fatal("derived group did not retain collapse")
	}
	r := s.notesKey(gfx.KeyEvent{Key: gfx.KeyEscape})
	if s.notes.view.popup != "" || r.Focus != hitNoteHostsOpen {
		t.Fatal("dismiss did not restore focus")
	}
}
