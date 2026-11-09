package workbench

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestNotesHostTagCodecAndNormalization(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	h := noteHost{"host_a", "主机,@:{}%+", "user,@:{}%+", "生产环境 / A,@:{}%+"}
	tag := encodeNoteHost(h)
	if strings.Contains(tag, ",") {
		t.Fatal("encoded host can split into ordinary tags")
	}
	if got, ok := parseNoteHost(tag); !ok || got != h {
		t.Fatalf("round trip: %+v %v", got, ok)
	}
	for _, bad := range []string{"plain", "{@Host:@Address:@User:@Label:@}", "{@Host:a@Address:%XY@User:@Label:@}", "{@Host:a@Address:x,y@User:@Label:@}"} {
		if _, ok := parseNoteHost(bad); ok {
			t.Fatalf("accepted invalid encoding %q", bad)
		}
	}
	s.hosts = []Host{{CatalogID: h.CatalogID, Address: h.Address, User: h.User, Label: h.Label}}
	n := s.normalizeNoteHosts(savedNote{Tags: []string{"ordinary", tag, tag, "{@Host:bad"}})
	if len(n.Tags) != 3 || !noteHasHost(n.Tags, h.CatalogID) {
		t.Fatalf("normalization: %+v", n)
	}
	if again := s.normalizeNoteHosts(n); !reflect.DeepEqual(n, again) {
		t.Fatal("normalization not idempotent")
	}
}

func TestNotesHostTagsShareEditingAndStableSelection(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.hosts = []Host{{CatalogID: "one", Label: "One"}, {CatalogID: "two", Label: "Two"}}
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	token := s.noteTagToken("two")
	slices.Reverse(s.hosts)
	s.Click(hitNoteHostRows.At(token))
	if !noteHasHost(s.notes.draft.Tags, "two") || noteHasHost(s.notes.draft.Tags, "one") {
		t.Fatal("host selection followed an index")
	}
	s.setNoteTags([]string{"regular"})
	if !noteHasHost(s.notes.draft.Tags, "two") || !slices.Equal(s.noteTagCandidates(), []string{"regular"}) {
		t.Fatal("ordinary editing lost or exposed host association")
	}
	s.activeSection = sectionNotes
	s.notes.panel = true
	if noteHostCount(s.notes.draft.Tags) != 1 {
		t.Fatal("associations changed")
	}
	tag := s.notes.draft.Tags[1]
	s.Click(hitNoteTagRemove.At(s.noteTagToken(tag)))
	if noteHostCount(s.notes.draft.Tags) != 0 || !slices.Equal(s.notes.draft.Tags, []string{"regular"}) {
		t.Fatal("chip removal affected other tags")
	}
	// A click queued before a host was deleted cannot add it back.
	s.hosts = slices.DeleteFunc(s.hosts, func(h Host) bool { return h.CatalogID == "two" })
	s.Click(hitNoteHostRows.At(token))
	if noteHasHost(s.notes.draft.Tags, "two") {
		t.Fatal("stale picker click restored deleted host")
	}
}

func TestNotesHostRefreshMergesOnlyCurrentAssociations(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	old := noteHost{CatalogID: "host", Label: "Old"}
	current := noteHost{CatalogID: "host", Label: "New", Address: "new.example", User: "new-user"}
	s.hosts = []Host{{CatalogID: current.CatalogID, Label: current.Label, Address: current.Address, User: current.User}}
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.replaceNoteTagEditor([]string{"ordinary", encodeNoteHost(old)})
	s.save()
	updates := scanNoteHosts(s.notes.list, map[string]noteHost{"host": current})
	if len(updates) != 1 {
		t.Fatal("scan did not find changed snapshot")
	}
	s.notes.body.Insert("unsaved body")
	s.notes.title.Insert("unsaved title")
	s.notes.hostUpdates = updates
	s.applyNoteHostUpdates()
	if s.notes.body.String() != "unsaved body" || s.notes.title.String() != "unsaved title" || !slices.Equal(s.notes.draft.Tags, []string{"ordinary", encodeNoteHost(current)}) {
		t.Fatal("refresh overwrote draft content")
	}
	revision := s.notes.draft.Revision
	s.notes.hostUpdates = updates
	s.applyNoteHostUpdates()
	if revision != s.notes.draft.Revision {
		t.Fatal("metadata refresh changed body revision")
	}
	// A queued result cannot resurrect an association removed after the scan.
	s.notes.hostUpdates = updates
	s.replaceNoteTagEditor([]string{"ordinary"})
	s.save()
	s.applyNoteHostUpdates()
	if noteHostCount(s.notes.draft.Tags) != 0 {
		t.Fatal("refresh restored removed association")
	}
	// Missing hosts retain their cached tag.
	missing := encodeNoteHost(noteHost{CatalogID: "missing", Label: "Cached"})
	if got := scanNoteHosts([]savedNote{{Tags: []string{missing}}}, nil); len(got) != 0 {
		t.Fatal("missing host was rewritten")
	}
}

func TestNotesHostRefreshDefersCompositionAndRejectsStaleHosts(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	old := noteHost{CatalogID: "host", Label: "Old"}
	next := noteHost{CatalogID: "host", Label: "Next"}
	s.hosts = []Host{{CatalogID: "host", Label: "Next"}}
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.replaceNoteTagEditor([]string{encodeNoteHost(old)})
	s.save()
	s.notes.hostUpdates = scanNoteHosts(s.notes.list, map[string]noteHost{"host": next})
	s.notes.body.SetMarkedText("输入", text.Range{End: 2})
	s.applyNoteHostUpdates()
	if len(s.notes.hostUpdates) != 1 || !slices.Contains(s.notes.draft.Tags, encodeNoteHost(old)) {
		t.Fatal("refresh modified composing draft")
	}
	s.notes.body.Insert("输入")
	s.applyNoteHostUpdates()
	if !slices.Contains(s.notes.draft.Tags, encodeNoteHost(next)) || s.notes.body.String() != "输入" {
		t.Fatal("deferred result not applied")
	}
	s.notes.hostUpdates = []noteHostUpdate{{s.notes.selected, encodeNoteHost(next), encodeNoteHost(old)}}
	s.applyNoteHostUpdates()
	if !slices.Contains(s.notes.draft.Tags, encodeNoteHost(next)) {
		t.Fatal("stale host snapshot applied")
	}
	// Repeated entry requests are coalesced and eventually drained.
	s.refreshNoteHosts()
	s.refreshNoteHosts()
	deadline := time.Now().Add(3 * time.Second)
	for s.notes.hostScanRunning && time.Now().Before(deadline) {
		s.drainAI()
		time.Sleep(time.Millisecond)
	}
	if s.notes.hostScanRunning || s.notes.hostScanAgain {
		t.Fatal("coalesced scans did not finish")
	}
}

func TestNotesHostRefreshSurvivesSwitchDeletionAndSaveFailure(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	old := noteHost{CatalogID: "host", Label: "Old"}
	next := noteHost{CatalogID: "host", Label: "New"}
	s.hosts = []Host{{CatalogID: "host", Label: "New"}}
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	first := s.notes.selected
	s.replaceNoteTagEditor([]string{encodeNoteHost(old)})
	s.save()
	updates := scanNoteHosts(s.notes.list, map[string]noteHost{"host": next})
	s.Click(hitNoteAdd)
	second := s.notes.selected
	s.notes.body.Insert("second draft")
	s.notes.hostUpdates = updates
	s.applyNoteHostUpdates()
	if s.notes.selected != second || s.notes.body.String() != "second draft" || !noteHasHost(s.notes.list[s.noteIndex(first)].Tags, "host") {
		t.Fatal("switching notes lost draft or association")
	}
	s.notes.list = slices.DeleteFunc(s.notes.list, func(n savedNote) bool { return n.ID == first })
	s.notes.hostUpdates = updates
	s.applyNoteHostUpdates()
	if s.noteIndex(first) >= 0 {
		t.Fatal("scan restored deleted note")
	}
	s.replaceNoteTagEditor([]string{encodeNoteHost(old)})
	s.save()
	s.notes.hostUpdates = scanNoteHosts(s.notes.list, map[string]noteHost{"host": next})
	s.notes.body.Insert(" pending body")
	s.saveBlocked = true
	s.applyNoteHostUpdates()
	if s.notes.error == "" || !s.hasUnsavedNotes() || !slices.Contains(s.notes.draft.Tags, encodeNoteHost(next)) || !strings.Contains(s.notes.body.String(), " pending body") {
		t.Fatalf("failed save: error=%q dirty=%v tags=%v bodylen=%d", s.notes.error, s.hasUnsavedNotes(), s.notes.draft.Tags, len(s.notes.body.String()))
	}
}

func TestNotesHostTagsPaintAfterOrdinaryTags(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	missing := encodeNoteHost(noteHost{CatalogID: "missing", Label: "Cached"})
	s.replaceNoteTagEditor([]string{missing, "ordinary"})
	s.save()
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1000, Height: 700})
	hostHit, ok := hitByID(&c, hitNoteTagHints.At(s.noteTagToken(missing)))
	if !ok || !strings.Contains(hostHit.Label, "主机不存在") && !strings.Contains(hostHit.Label, "Host missing") {
		t.Fatal("missing state is inaccessible")
	}
	ordinaryHit, ok := hitByID(&c, hitNoteTagHints.At(s.noteTagToken("ordinary")))
	if !ok || ordinaryHit.Bounds.X >= hostHit.Bounds.X {
		t.Fatal("hosts did not follow ordinary tags")
	}
}

func TestNotesHostTagsArriveThroughSyncAndBackup(t *testing.T) {
	a := newTestState(t, freshOptions(t))
	a.notes.list = []savedNote{{ID: "synced", Body: "body", Tags: []string{"ordinary", encodeNoteHost(noteHost{CatalogID: "foreign-host"})}}}
	a.save()
	document, err := a.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	b := newTestState(t, freshOptions(t))
	if _, err := b.mergeRemote(document, false); err != nil {
		t.Fatal(err)
	}
	if len(b.notes.list) != 1 || !noteHasHost(b.notes.list[0].Tags, "foreign-host") {
		t.Fatal("sync lost host association")
	}
	b.save()
	document, err = b.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	restored := newTestState(t, freshOptions(t))
	if _, err := restored.applyBackup(document); err != nil {
		t.Fatal(err)
	}
	if len(restored.notes.list) != 1 || !slices.Equal(restored.notes.list[0].Tags, b.notes.list[0].Tags) {
		t.Fatal("backup lost special tags")
	}
}

func noteHostCount(tags []string) int {
	count := 0
	for _, tag := range tags {
		if _, ok := parseNoteHost(tag); ok {
			count++
		}
	}
	return count
}
