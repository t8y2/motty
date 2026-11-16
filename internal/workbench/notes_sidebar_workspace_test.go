package workbench

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

func noteTabPair(t *testing.T) (*State, layoutcore.WorkspaceID, layoutcore.WorkspaceID) {
	t.Helper()
	s := newTestState(t, testOptions(t))
	openHost(s)
	first := s.deck.ActiveWorkspace()
	s.Click(hitHosts)
	s.Click(hitHostList.At(2))
	openHost(s)
	second := s.deck.ActiveWorkspace()
	if first == second {
		t.Fatal("expected two workspaces")
	}
	s.cellWidth, s.cellHeight = 8, 16
	s.Resize(gfx.Size{Width: 1200, Height: 800})
	return s, first, second
}

func TestNoteSidebarWorkspaceGeometryAndIsolation(t *testing.T) {
	s, first, second := noteTabPair(t)
	s.notes.list = []savedNote{{ID: "one", Body: "shared"}}
	a := s.noteSidebarFor(first)
	a.open, a.width, a.fraction = true, 350, .7
	a.content.selected = "one"
	a.content.query.Insert("query A")
	a.content.sideAll = true
	a.content.view.expanded["folder"] = true
	a.content.view.treeScroll, a.content.previewScroll = 17, 29
	a.widthDragging = true
	s.activateWorkspace(first)
	a.widthDragging = true
	oldInput := s.NotesInput(app.TextInputContext{ID: hitNoteSearch})
	if oldInput == nil {
		t.Fatal("sidebar search has no native input")
	}
	stale := oldInput.OnEvent
	s.activateWorkspace(second)
	b := s.noteSidebarState()
	if b.open || b.width != 400 || b.fraction != .5 || b.content.query.String() != "" || b.content.sideAll || b.content.selected != "" || len(b.content.view.expanded) != 0 {
		t.Fatal("new tab inherited sidebar state")
	}
	if a.widthDragging {
		t.Fatal("switch retained drag")
	}
	if got, _ := stale(app.TextEvent{}); !reflect.DeepEqual(got, app.TextState{}) {
		t.Fatal("stale search event remained active")
	}
	b.open, b.width = true, 460
	s.fitTerminals()
	sizes := map[uint32][2]int{}
	for i := range s.sessions {
		c, r := s.sessions[i].term.screen.Size()
		sizes[s.sessions[i].ID] = [2]int{c, r}
	}
	for _, id := range []layoutcore.WorkspaceID{first, second, first} {
		s.activateWorkspace(id)
		s.fitTerminals()
		for i := range s.sessions {
			session := &s.sessions[i]
			c, r := session.term.screen.Size()
			if sizes[session.ID] != [2]int{c, r} {
				t.Fatal("switch resized a terminal")
			}
		}
	}
	if a.content.query.String() != "query A" || a.content.view.treeScroll != 17 || a.content.previewScroll != 29 {
		t.Fatal("switch lost browsing state")
	}
	s.Click(hitNotesPanel)
	s.Click(hitNotesPanel)
	if s.noteSidebarState() != a || a.width != 350 || a.content.query.String() != "query A" {
		t.Fatal("reopen lost state")
	}
	s.Resize(gfx.Size{Width: 400, Height: 500})
	s.Resize(gfx.Size{Width: 1200, Height: 800})
	if a.width != 350 || b.width != 460 {
		t.Fatal("layout overwrote preferred widths")
	}
	s.notes.list[0].Body = "updated"
	s.invalidateNoteViews()
	if a.content.body.String() != "updated" {
		t.Fatal("shared content did not refresh")
	}
	s.notes.list = nil
	s.invalidateNoteViews()
	if a.content.selected != "" {
		t.Fatal("deleted selection survived")
	}
	s.closeWorkspaceNow(first)
	if s.noteSidebars[first] != nil {
		t.Fatal("closed workspace retained sidebar")
	}
}

func TestNoteSidebarWorkspacePersistenceAndMigration(t *testing.T) {
	s, first, second := noteTabPair(t)
	s.notes.list = []savedNote{{ID: "one", Body: "shared"}}
	s.notes.folders = []savedNoteFolder{{ID: "folder"}}
	a := s.noteSidebarFor(first)
	a.open, a.width, a.fraction = true, 440, .6
	a.content.query.Insert("saved search")
	a.content.selected = "one"
	a.content.sideAll = true
	a.content.view.expanded["folder"] = true
	a.content.view.treeScroll, a.content.previewScroll = 23, 41
	saved := s.snapshot()
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded savedState
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	restored := newTestState(t, testOptions(t))
	restored.restore(decoded)
	got := restored.noteSidebarFor(first)
	if !got.open || got.width != 440 || got.fraction != .6 || got.content.query.String() != "saved search" || !got.content.sideAll || !got.content.view.expanded["folder"] || got.content.selected != "one" {
		t.Fatal("sidebar state not restored")
	}
	if got.content.previewScroll != 0 || got.content.view.treeScroll != 0 {
		t.Fatal("scroll position persisted")
	}
	if restored.notesPanelOpenFor(second) {
		t.Fatal("closed tab opened on restore")
	}
	// A new empty object must take precedence over the legacy global switch.
	restored.restoreNoteSidebars(&savedNoteSidebars{}, true)
	if restored.notesPanelOpen() {
		t.Fatal("legacy switch overrode new format")
	}
	restored.restoreNoteSidebars(nil, true)
	if !restored.notesPanelOpenFor(second) || restored.notesPanelOpenFor(first) {
		t.Fatal("legacy switch did not migrate only to active workspace")
	}
	empty := newTestState(t, testOptions(t))
	empty.restoreNoteSidebars(nil, true)
	openHost(empty)
	if empty.notesPanelOpen() {
		t.Fatal("legacy switch leaked into new workspace")
	}
	// Sidebars are window-local and cannot be imported by a second window.
	isolated := applyWindowState(saved, nil, 2)
	if isolated.NoteSidebars != nil || isolated.Deck != nil {
		t.Fatal("new window inherited sidebar state")
	}
	if !windowStateFields["note_sidebars"] || !isWorkspaceField("note_sidebars") {
		t.Fatal("sidebar state is not routed with workspaces")
	}
}

func TestNoteSidebarImageResultStaysWithWorkspace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer server.Close()
	s, first, second := noteTabPair(t)
	s.notes.list = []savedNote{{ID: "image", Body: "![image](" + server.URL + "/image.png)"}}
	s.activateWorkspace(first)
	a := s.noteSidebarState()
	a.open = true
	a.content.selected = "image"
	s.syncNoteSidebar()
	ready := make(chan struct{}, 1)
	s.options.Invalidate = func() {
		select {
		case ready <- struct{}{}:
		default:
		}
	}
	s.loadNoteImagesContent(&a.content, true)
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("image callback not queued")
	}
	s.activateWorkspace(second)
	b := s.noteSidebarState()
	b.open = true
	s.drainAI()
	if a.content.error == "" || b.content.error != "" {
		t.Fatal("image result delivered to wrong workspace")
	}
	s.activateWorkspace(first)
	s.loadNoteImagesContent(&a.content, true)
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("image callback not queued")
	}
	s.closeWorkspaceNow(first)
	s.drainAI()
	if s.noteSidebars[first] != nil || b.content.error != "" {
		t.Fatal("late callback recreated closed sidebar")
	}
}

// Exercise the original path with a real PTY: an idle shell must receive no
// resize on tab switches, and therefore produce no background prompt redraw.
func TestNoteSidebarIdleLocalShellDoesNotAlertOnTabSwitch(t *testing.T) {
	s, _ := openLocal(t)
	s.Resize(gfx.Size{Width: 1200, Height: 800})
	first := s.deck.ActiveWorkspace()
	s.Click(hitLocalTerminal)
	second := s.deck.ActiveWorkspace()
	settle := func() {
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			s.Wake()
			time.Sleep(5 * time.Millisecond)
		}
	}
	settle()
	s.Click(hitNotesPanel)
	settle()
	s.backgroundOutputAlerts = true
	s.clearOutputAlerts()
	for _, id := range []layoutcore.WorkspaceID{first, second} {
		s.activateWorkspace(id)
		settle()
		for i := range s.sessions {
			if s.sessions[i].outputAlert.kind != outputAlertNone {
				t.Fatal("idle tab switch triggered an output alert")
			}
		}
	}
	// Closing the panel is acknowledged while visible. Subsequent idle tab
	// switches must remain quiet, too.
	s.Click(hitNotesPanel)
	settle()
	s.clearOutputAlerts()
	s.activateWorkspace(first)
	settle()
	for i := range s.sessions {
		if s.sessions[i].outputAlert.kind != outputAlertNone {
			t.Fatal("closed sidebar tab switch alerted")
		}
	}
}

func TestNoteSidebarSplitSharesWorkspaceState(t *testing.T) {
	s := threeVisiblePaneState(t)
	s.Click(hitNotesPanel)
	workspace := s.deck.ActiveWorkspace()
	v := s.noteSidebarState()
	v.width = 350
	s.fitTerminals()
	sizes := map[uint32][2]int{}
	for i := range s.sessions {
		c, r := s.sessions[i].term.screen.Size()
		sizes[s.sessions[i].ID] = [2]int{c, r}
	}
	for _, pane := range s.deck.Panes(workspace, nil) {
		s.activatePane(workspace, pane.ID)
		if s.noteSidebarState() != v {
			t.Fatal("split created another sidebar")
		}
		for i := range s.sessions {
			session := &s.sessions[i]
			c, r := session.term.screen.Size()
			if sizes[session.ID] != [2]int{c, r} {
				t.Fatal("split focus resized terminal")
			}
		}
	}
}
