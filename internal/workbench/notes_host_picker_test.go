package workbench

import (
	"fmt"
	"slices"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestNotesPopupSizing(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.activeSection = sectionNotes
	for _, test := range []struct{ doc, window, want float32 }{{1200, 1400, 600}, {400, 800, 340}, {400, 300, 276}} {
		s.notes.view.documentBounds.Width = test.doc
		w, h := notePopupExtent(test.doc, test.window, 800, false)
		if w != test.want || h < 479 || h > 481 {
			t.Fatalf("size %v %v", w, h)
		}
	}
	s.notes.view.popup = "hosts"
	s.notes.view.documentBounds.Width = 1200
	for _, test := range []struct{ window, want float32 }{{1400, 1200}, {1000, 976}} {
		w, _ := notePopupExtent(1200, test.window, 800, true)
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
	if !ok || hit.Role != gfx.RoleCheckbox || hit.Label != "Host 00" || hit.Checked {
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
	group, ok := hitByID(&c, hitNoteHostGroups.At(s.noteTagToken("Group")))
	if !ok {
		t.Fatal("the group row is missing")
	}
	s.Click(id)
	if !noteHasHost(s.notes.draft.Tags, "host-00") || s.notes.view.popup != "hosts" || len(s.sessions) != 0 {
		t.Fatal("card did not select or opened connection")
	}
	config := s.WindowConfig(app.Config{OnScroll: s.Scroll})
	for range 20 {
		config.OnScroll(gfx.ScrollEvent{X: group.Bounds.X + 10, Y: group.Bounds.Y + 10, DeltaY: 300})
	}
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 1400, Height: 800})
	if _, ok := hitByID(&c, hitNoteHostGroups.At(s.noteTagToken("Group"))); ok {
		t.Fatal("the wheel did not scroll the picker")
	}
	s.notes.view.picker.Insert("Host 00")
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 1400, Height: 800})
	groupAfter, ok := hitByID(&c, hitNoteHostGroups.At(s.noteTagToken("Group")))
	if !ok || groupAfter.Bounds.Y > group.Bounds.Y+5 {
		t.Fatal("query did not reset the picker scroll")
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
