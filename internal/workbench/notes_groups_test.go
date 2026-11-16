package workbench

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestNotesGroupTagsCodecSelectionAndFiltering(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	path := "生产环境/数据库,@%"
	tag := encodeNoteHostGroup(path)
	if strings.Contains(tag, ",") {
		t.Fatal("unescaped delimiter")
	}
	if got, ok := parseNoteHostGroup(tag); !ok || got != path {
		t.Fatal("round trip", got, ok)
	}
	for _, bad := range []string{"{@HostGroup:@}", "{@HostGroup:%XX@}", "{@HostGroup:a@b@}", "{@HostGroup:%FF@}"} {
		if _, ok := parseNoteHostGroup(bad); ok {
			t.Fatal("accepted", bad)
		}
	}
	s.hostGroups = []hostGroup{{Name: path, Explicit: true}, {Name: "Empty", Explicit: true}}
	s.hosts = []Host{{CatalogID: "db", Group: path + "/Child"}, {CatalogID: "other", Group: "Elsewhere"}}
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.Click(hitNoteGroupSelect.At(s.noteTagToken(path)))
	s.setNoteTags([]string{"ordinary", "{@HostGroup:broken"})
	if !noteHasGroup(s.notes.draft.Tags, path) || !slices.Equal(s.noteTagCandidates(), []string{"ordinary", "{@HostGroup:broken"}) {
		t.Fatal("ordinary editing exposed or lost group")
	}
	n := s.normalizeNoteHosts(savedNote{Tags: []string{tag, tag}})
	if len(n.Tags) != 1 {
		t.Fatal("group dedup")
	}
	if !s.noteMatchesHost(n.Tags, "db") || s.noteMatchesHost(n.Tags, "other") {
		t.Fatal("group filter")
	}
	s.toggleNoteHost("db")
	if !noteHasGroup(s.notes.draft.Tags, path) || !noteHasHost(s.notes.draft.Tags, "db") {
		t.Fatal("group selection affected host")
	}
	s.Click(hitNoteTagRemove.At(s.noteTagToken(tag)))
	if noteHasGroup(s.notes.draft.Tags, path) || !noteHasHost(s.notes.draft.Tags, "db") {
		t.Fatal("group removal affected host")
	}
	s.toggleNoteGroup("")
	items, _ := s.layoutNoteHostPicker(400, "Empty")
	if len(items) != 1 || items[0].key != "Empty" {
		t.Fatal("empty group search", items)
	}
}
func TestNotesGroupChangesAreOrderedAndDoNotRestoreAssociations(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.hostGroups = []hostGroup{{Name: "A", Explicit: true}, {Name: "A/DB", Explicit: true}, {Name: "A2", Explicit: true}}
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.replaceNoteTagEditor([]string{encodeNoteHostGroup("A/DB"), encodeNoteHostGroup("A2"), "ordinary"})
	s.save()
	if !s.remapHostGroup("A", "B") || !s.remapHostGroup("B", "C") {
		t.Fatal("remap")
	}
	s.notes.body.Insert("new body")
	s.save()
	if !noteHasGroup(s.notes.draft.Tags, "C/DB") || !noteHasGroup(s.notes.draft.Tags, "A2") || s.notes.body.String() != "new body" {
		t.Fatal("ordered remap or draft", s.notes.draft.Tags)
	}
	// A removal followed by a new association at the same path is a new identity.
	s.notes.body.SetMarkedText("输入", text.Range{End: 2})
	s.queueNoteGroupChange("C", "D", false)
	s.removeNoteGroup("C/DB")
	s.toggleNoteGroup("C/DB")
	s.notes.body.Unmark()
	s.save()
	if !noteHasGroup(s.notes.draft.Tags, "C/DB") || noteHasGroup(s.notes.draft.Tags, "D/DB") {
		t.Fatal("old event modified new association")
	}
	// Capture deletion before recreation; the old delete must not remove the new tag.
	s.notes.body.SetMarkedText("输入", text.Range{End: 2})
	if !s.deleteHostGroup("C", false) {
		t.Fatal("delete group")
	}
	s.removeNoteGroup("C/DB")
	s.hostGroups = append(s.hostGroups, hostGroup{Name: "C/DB", Explicit: true})
	s.toggleNoteGroup("C/DB")
	s.notes.body.Unmark()
	s.save()
	if !noteHasGroup(s.notes.draft.Tags, "C/DB") {
		t.Fatal("old deletion removed recreated group association")
	}
}
func TestNotesGroupChangesCompositionAndDeletion(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.hostGroups = []hostGroup{{Name: "A", Explicit: true}, {Name: "A/DB", Explicit: true}}
	s.hosts = []Host{{CatalogID: "db", Group: "A/DB"}}
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.replaceNoteTagEditor([]string{encodeNoteHostGroup("A/DB"), encodeNoteHost(noteHost{CatalogID: "db"})})
	s.save()
	s.notes.body.SetMarkedText("输入", text.Range{End: 2})
	s.queueNoteGroupChange("A", "B", false)
	if !noteHasGroup(noteWords(s.notes.tags.String()), "A/DB") {
		t.Fatal("changed composing draft")
	}
	s.notes.body.Insert("输入")
	s.save()
	if !noteHasGroup(s.notes.draft.Tags, "B/DB") {
		t.Fatal("deferred remap")
	}
	s.queueNoteGroupChange("B", "", true)
	s.save()
	if noteHasGroup(s.notes.draft.Tags, "B/DB") || !noteHasHost(s.notes.draft.Tags, "db") {
		t.Fatal("delete did not preserve individual host")
	}
	// Verify the group checkbox has a separate hit from expand/collapse.
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1000, Height: 700})
	s.Click(hitNoteHostsOpen)
	c.Reset()
	s.Paint(&c, gfx.Size{Width: 1000, Height: 700})
	found := false
	for _, hit := range c.Hits() {
		if hit.ID == hitNoteGroupSelect.At(s.noteTagToken("A")) {
			found = true
		}
	}
	if !found {
		t.Fatal("missing group selection checkbox")
	}
}

func TestNotesGroupQueuedCollisionAndSaveFailure(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	first := s.notes.selected
	s.replaceNoteTagEditor([]string{encodeNoteHostGroup("A"), encodeNoteHostGroup("B")})
	s.save()
	s.notes.body.SetMarkedText("输入", text.Range{End: 2})
	s.queueNoteGroupChange("A", "B", false)
	s.queueNoteGroupChange("B", "C", false)
	// Removing the pre-existing B must not cancel the independent A association.
	s.removeNoteGroup("B")
	s.saveBlocked = true
	s.notes.body.Unmark()
	s.Click(hitNoteAdd)
	s.notes.body.Insert("other draft")
	s.save()
	tags := s.notes.list[s.noteIndex(first)].Tags
	if !noteHasGroup(tags, "C") || noteHasGroup(tags, "A") {
		t.Fatal("collision cancellation", tags)
	}
	if !s.hasUnsavedNotes() || s.notes.body.String() != "other draft" {
		t.Fatal("overwrote another draft")
	}
	s.saveBlocked = false
	s.save()
	reopened := newTestState(t, s.options)
	if !noteHasGroup(reopened.notes.list[reopened.noteIndex(first)].Tags, "C") {
		t.Fatal("group tags not persisted")
	}
	// A deleted note must not be recreated by an outstanding result.
	s.selectNoteWithoutSave(first)
	s.notes.body.SetMarkedText("输入", text.Range{End: 2})
	s.queueNoteGroupChange("C", "D", false)
	s.notes.body.Unmark()
	s.notes.list = slices.DeleteFunc(s.notes.list, func(n savedNote) bool { return n.ID == first })
	s.save()
	if s.noteIndex(first) >= 0 {
		t.Fatal("restored deleted note")
	}
}

func TestNotesGroupRenameSurvivesImmediateClose(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.hostGroups = []hostGroup{{Name: "A", Explicit: true}}
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.replaceNoteTagEditor([]string{encodeNoteHostGroup("A")})
	s.save()
	id := s.notes.selected
	if !s.remapHostGroup("A", "B") {
		t.Fatal("rename failed")
	}
	if s.ConfirmDocumentClose(func() {}) {
		t.Fatal("saved rename unexpectedly blocked closing")
	}
	s.Close()
	reopened := newTestState(t, s.options)
	if reopened.hostGroupIndex("B") < 0 || !noteHasGroup(reopened.notes.list[reopened.noteIndex(id)].Tags, "B") {
		t.Fatal("immediate close lost renamed group association")
	}
}

func TestNotesPendingGroupRenameProtectsClose(t *testing.T) {
	for _, action := range []string{"retry", "discard"} {
		t.Run(action, func(t *testing.T) {
			s := newTestState(t, freshOptions(t))
			s.hostGroups = []hostGroup{{Name: "A", Explicit: true}}
			s.Click(hitNotes)
			s.Click(hitNoteAdd)
			s.replaceNoteTagEditor([]string{encodeNoteHostGroup("A")})
			s.save()
			// A composition may equal the saved text; the pending association
			// alone must still protect closing.
			s.notes.body.SetMarkedText("", text.Range{})
			if !s.remapHostGroup("A", "B") || !s.hasUnsavedNotes() {
				t.Fatal("pending rename was not retained")
			}
			closed := false
			if !s.ConfirmDocumentClose(func() { closed = true }) || closed {
				t.Fatal("pending rename did not protect closing")
			}
			if action == "discard" {
				s.clickNoteExit(hitNoteExitDiscard)
			} else {
				s.notes.body.Unmark()
				s.clickNoteExit(hitNoteExitRetry)
			}
			if !closed || len(s.notes.groupJobs) != 0 || s.hasUnsavedNotes() {
				t.Fatal("close left pending changes")
			}
			s.save()
			reopened := newTestState(t, s.options)
			want := "B"
			if action == "discard" {
				want = "A"
			}
			if !noteHasGroup(reopened.notes.list[0].Tags, want) {
				t.Fatal("close did not persist the chosen association")
			}
		})
	}
}
