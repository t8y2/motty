package workbench

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	layoutcore "motty/internal/workspace"
)

func launchTemporaryWorkspace(t testing.TB, state *State, hostIndexes ...int) layoutcore.WorkspaceID {
	t.Helper()
	return launchTemporaryWorkspaceWithColumns(t, state, 0, hostIndexes...)
}

func launchTemporaryWorkspaceWithColumns(t testing.TB, state *State, columns uint8, hostIndexes ...int) layoutcore.WorkspaceID {
	t.Helper()
	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceNew)
	for _, index := range hostIndexes {
		state.Click(workspaceDraftHostID(index))
	}
	if columns != 0 {
		state.Click(workspaceDraftColumnID(columns))
	}
	state.Click(hitWorkspaceDraftSubmit)
	if state.modal != modalNone || state.activeSection != sectionTerminal {
		t.Fatalf("temporary launch left modal=%d section=%d", state.modal, state.activeSection)
	}
	workspaceID := state.deck.ActiveWorkspace()
	if workspaceID == 0 {
		t.Fatal("temporary launch created no workspace")
	}
	return workspaceID
}

func TestTemporaryWorkspaceCanBeSavedFromItsTabMenu(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspaceID := launchTemporaryWorkspace(t, state, 1, 0)
	if _, ok := state.temporaryWorkspaceLayouts[workspaceID]; !ok {
		t.Fatal("temporary launch did not record its source")
	}

	rightClickTab(state, hitTabList.At(int(workspaceID)))
	if !menuHas(state, hitTabMenuSaveWorkspace) {
		t.Fatal("a temporary workspace tab does not offer Save as Workspace Template")
	}
	state.Click(hitTabMenuSaveWorkspace)
	if state.modal != modalWorkspaceSave || state.workspaceSave.workspace != workspaceID {
		t.Fatalf("save dialog = modal %d workspace %d", state.modal, state.workspaceSave.workspace)
	}
	if got, want := state.workspaceSave.name.String(), state.tabLabel(state.workspaceTabs[0]); got != want {
		t.Fatalf("save name starts as %q, want %q", got, want)
	}
	if !isGrid(state.workspaceSave.layout, 2) {
		t.Fatalf("save columns = %+v, want 2", state.workspaceSave.layout)
	}
	if state.workspaceSave.error != "" {
		t.Fatalf("save dialog opened with error %q", state.workspaceSave.error)
	}
	if len(world.configs) != 2 {
		t.Fatalf("opening save changed dials to %d", len(world.configs))
	}
}

func TestOnlyTemporaryWorkspaceTabsOfferSave(t *testing.T) {
	t.Run("ordinary session", func(t *testing.T) {
		world, options := newWorld(t)
		state := newTestState(t, options)
		connect(t, state, world)
		workspaceID := state.deck.ActiveWorkspace()
		rightClickTab(state, hitTabList.At(int(workspaceID)))
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		if _, ok := hitByID(&canvas, hitTabMenuSaveWorkspace); ok {
			t.Fatal("an ordinary session tab offers Save as Workspace Template")
		}
	})

	t.Run("fixed template", func(t *testing.T) {
		world, options := newWorld(t)
		state := newTestState(t, options)
		template := mustWorkspaceTemplate(t, "Saved", workspaceCatalogIDs(state, 0, 1), 2)
		state.templates = []WorkspaceTemplate{template}
		state.activeSection = sectionWorkspaces
		paintAt(state, gfx.Size{Width: 1400, Height: 900})
		state.Click(hitWorkspaceTemplateOpen.At(0))
		workspaceID := state.deck.ActiveWorkspace()
		if len(world.configs) != 2 {
			t.Fatalf("applied template dialed %d hosts", len(world.configs))
		}
		rightClickTab(state, hitTabList.At(int(workspaceID)))
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		if _, ok := hitByID(&canvas, hitTabMenuSaveWorkspace); ok {
			t.Fatal("a fixed-template tab offers Save as Workspace Template")
		}
	})
}

func TestSavingTemporaryWorkspaceStoresOnlyCompositionInVisualOrder(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	workspaceID := launchTemporaryWorkspaceWithColumns(t, state, 3, 0, 1, 2)
	// Move the middle pane to the first visual position through a grid of the
	// same panes in a deliberately different order.
	panes := state.deck.Panes(workspaceID, nil)
	if len(panes) != 3 {
		t.Fatalf("panes = %d", len(panes))
	}
	ordered := []layoutcore.PaneID{panes[2].ID, panes[0].ID, panes[1].ID}
	if err := state.deck.SetLayout(workspaceID, ordered, layoutcore.GridSpec(3)); err != nil {
		t.Fatal(err)
	}
	state.refreshWorkspaceViews()

	state.openWorkspaceSave(workspaceID)
	state.workspaceSave.name.Insert("Ops cluster")
	state.submitWorkspaceSave()

	if state.modal != modalNone || state.activeSection != sectionWorkspaces {
		t.Fatalf("save left modal=%d section=%d", state.modal, state.activeSection)
	}
	if len(state.templates) != 1 {
		t.Fatalf("templates = %+v", state.templates)
	}
	template := state.templates[0]
	if template.Name != "Ops cluster" || !isGrid(template.Layout, 3) || !slices.Equal(template.HostIDs,
		[]string{state.hosts[2].CatalogID, state.hosts[0].CatalogID, state.hosts[1].CatalogID}) {
		t.Fatalf("template = %+v", template)
	}
	if state.selectedWorkspaceTemplateID != template.ID {
		t.Fatalf("selected = %q, want %q", state.selectedWorkspaceTemplateID, template.ID)
	}
	if _, ok := state.temporaryWorkspaceLayouts[workspaceID]; ok {
		t.Fatal("saved temporary workspace retained its save marker")
	}
	if len(state.fileNotices.items) != 1 || state.fileNotices.items[0].detail != strings.ReplaceAll(
		state.messages.WorkspaceDraftSavedFmt, "%s", template.Name) {
		t.Fatalf("notice = %+v", state.fileNotices.items)
	}

	raw, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"id": true, "name": true, "host_ids": true, "layout": true}
	for field := range fields {
		if !allowed[field] {
			t.Fatalf("template carries %q: %s", field, raw)
		}
	}

	loaded, report, err := loadState(options.StatePath)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load: %+v, %v", report, err)
	}
	if len(loaded.WorkspaceTemplates) != 1 || !reflect.DeepEqual(loaded.WorkspaceTemplates[0], template) {
		t.Fatalf("loaded = %+v, want %+v", loaded.WorkspaceTemplates, template)
	}
}

func TestSavingTemporaryWorkspaceUsesTheCurrentHeaderColumns(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	workspaceID := launchTemporaryWorkspaceWithColumns(t, state, 2, 0, 1, 2)
	paintAt(state, gfx.Size{Width: 1400, Height: 900})

	state.Click(hitWorkspaceHeaderLayout)
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceHeaderPresets.At(2))
	if !isGrid(state.temporaryWorkspaceLayouts[workspaceID], 3) {
		t.Fatalf("temporary source columns = %+v, want 3", state.temporaryWorkspaceLayouts[workspaceID])
	}

	state.openWorkspaceSave(workspaceID)
	state.workspaceSave.name.Insert("Current layout")
	state.submitWorkspaceSave()
	if len(state.templates) != 1 || !isGrid(state.templates[0].Layout, 3) {
		t.Fatalf("saved template = %+v", state.templates)
	}
}

func TestSavingTemporaryWorkspaceRequiresAName(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	workspaceID := launchTemporaryWorkspace(t, state, 0)
	state.openWorkspaceSave(workspaceID)
	state.workspaceSave.name = *text.NewEditor("   ")
	before := len(state.templates)
	state.Click(hitWorkspaceSaveSubmit)
	if state.modal != modalWorkspaceSave || state.workspaceSave.error != state.messages.WorkspaceDraftNameRequired {
		t.Fatalf("empty name: modal=%d error=%q", state.modal, state.workspaceSave.error)
	}
	if len(state.templates) != before {
		t.Fatalf("empty name added templates: %+v", state.templates)
	}
	if _, ok := state.temporaryWorkspaceLayouts[workspaceID]; !ok {
		t.Fatal("failed save removed the temporary source marker")
	}
	state.Click(hitWorkspaceSaveCancel)
	if state.modal != modalNone || state.workspaceSave.workspace != 0 || state.workspaceSave.name.String() != "" {
		t.Fatalf("cancel did not clear the save dialog: modal=%d save=%+v", state.modal, state.workspaceSave)
	}
}

func TestSavingTemporaryWorkspaceReportsAnUnwritableStoreWithoutChangingState(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	previousConfig := state.config
	t.Cleanup(func() { state.config = previousConfig })
	workspaceID := launchTemporaryWorkspace(t, state, 0, 1)
	state.openWorkspaceSave(workspaceID)
	state.workspaceSave.name = *text.NewEditor("Unwritable")
	beforeTemplates := slices.Clone(state.templates)
	beforeSelected := state.selectedWorkspaceTemplateID
	beforeExpanded := state.workspaceLibraryExpanded
	beforeBroadcast := state.broadcast
	beforeStatus := state.status
	beforeSection := state.activeSection
	beforeNoticeCount := len(state.fileNotices.items)

	state.config = nil
	state.submitWorkspaceSave()

	if state.modal != modalWorkspaceSave || state.workspaceSave.error != state.messages.WorkspaceSaveFailed {
		t.Fatalf("failed save: modal=%d error=%q", state.modal, state.workspaceSave.error)
	}
	if state.workspaceSave.name.String() != "Unwritable" {
		t.Fatalf("failed save changed input to %q", state.workspaceSave.name.String())
	}
	if !reflect.DeepEqual(state.templates, beforeTemplates) || state.selectedWorkspaceTemplateID != beforeSelected ||
		state.workspaceLibraryExpanded != beforeExpanded {
		t.Fatalf("failed save changed library state: templates=%+v selected=%q expanded=%t",
			state.templates, state.selectedWorkspaceTemplateID, state.workspaceLibraryExpanded)
	}
	if columns, ok := state.temporaryWorkspaceLayouts[workspaceID]; !ok || !isGrid(columns, 2) {
		t.Fatalf("failed save changed temporary marker: columns=%+v present=%t", columns, ok)
	}
	if state.broadcast != beforeBroadcast || state.status != beforeStatus || state.activeSection != beforeSection {
		t.Fatalf("failed save changed broadcast=%d status=%q section=%d, want %d %q %d",
			state.broadcast, state.status, state.activeSection, beforeBroadcast, beforeStatus, beforeSection)
	}
	if len(state.fileNotices.items) != beforeNoticeCount {
		t.Fatalf("failed save showed a success notice: %+v", state.fileNotices.items)
	}
}

func TestFailedWorkspaceSaveKeepsItsOpenSFTPViewAndConnection(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	previousConfig := state.config
	t.Cleanup(func() { state.config = previousConfig })
	workspaceID := state.deck.ActiveWorkspace()
	state.temporaryWorkspaceLayouts[workspaceID] = layoutcore.GridSpec(1)
	state.openWorkspaceSave(workspaceID)
	state.workspaceSave.name = *text.NewEditor("Unwritable")
	view := session.files
	worker := view.worker
	history := session.fileHistory
	session.keptAfterShell = true
	fake.mu.Lock()
	canceled := fake.canceled
	fake.mu.Unlock()
	state.config = nil

	state.submitWorkspaceSave()

	if state.modal != modalWorkspaceSave || state.workspaceSave.error != state.messages.WorkspaceSaveFailed {
		t.Fatalf("failed save: modal=%d error=%q", state.modal, state.workspaceSave.error)
	}
	if session.files != view || view.worker != worker || !reflect.DeepEqual(session.fileHistory, history) {
		t.Fatalf("failed save released the file view: files=%p want %p, worker=%p want %p",
			session.files, view, view.worker, worker)
	}
	fake.mu.Lock()
	gotCanceled := fake.canceled
	fake.mu.Unlock()
	if gotCanceled != canceled {
		t.Fatalf("failed save canceled the connection %d times, want %d", gotCanceled, canceled)
	}
}

func TestWorkspaceSaveRejectsPanesThatCannotBecomeATemplate(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, state *State, workspaceID layoutcore.WorkspaceID)
		messageKey string
	}{
		{
			name: "empty pane",
			setup: func(t *testing.T, state *State, workspaceID layoutcore.WorkspaceID) {
				t.Helper()
				if _, err := state.deck.Insert(workspaceID, state.deck.Panes(workspaceID, nil)[0].ID,
					layoutcore.Horizontal, 0.5, layoutcore.After); err != nil {
					t.Fatal(err)
				}
			},
			messageKey: "empty-pane",
		},
		{
			name: "missing session",
			setup: func(t *testing.T, state *State, workspaceID layoutcore.WorkspaceID) {
				t.Helper()
				// Keep the pane bound so the failure is a stale reference, not
				// the distinct empty-pane case.
				state.sessions = state.sessions[:0]
			},
			messageKey: "missing-session",
		},
		{
			name: "missing host reference",
			setup: func(t *testing.T, state *State, workspaceID layoutcore.WorkspaceID) {
				t.Helper()
				pane := state.deck.Panes(workspaceID, nil)[0]
				session, ok := state.sessionByIDPointer(uint32(pane.SessionID))
				if !ok {
					t.Fatal("session missing")
				}
				session.host.CatalogID = ""
				session.host.ID = 7
			},
			messageKey: "missing-host",
		},
		{
			name: "duplicate host",
			setup: func(t *testing.T, state *State, workspaceID layoutcore.WorkspaceID) {
				t.Helper()
				host := state.hosts[0]
				pane := state.deck.Panes(workspaceID, nil)[1]
				if _, err := state.deck.Unbind(workspaceID, pane.ID); err != nil {
					t.Fatal(err)
				}
				for index := range state.sessions {
					if state.sessions[index].ID == uint32(pane.SessionID) {
						state.sessions = append(state.sessions[:index], state.sessions[index+1:]...)
						break
					}
				}
				if state.bindWorkspaceSession(workspaceID, pane.ID, host, state.sessionAddress(host)) == nil {
					t.Fatal("bind duplicate host")
				}
			},
			messageKey: "duplicate-host",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, options := newWorld(t)
			state := newTestState(t, options)
			workspaceID := launchTemporaryWorkspace(t, state, 0, 1)
			test.setup(t, state, workspaceID)
			state.openWorkspaceSave(workspaceID)
			state.workspaceSave.name.Insert("Invalid")
			state.Click(hitWorkspaceSaveSubmit)
			var want string
			switch test.messageKey {
			case "empty-pane":
				want = state.messages.WorkspaceDraftEmptyPane
			case "missing-session":
				want = state.messages.WorkspaceDraftMissingSession
			case "missing-host":
				want = state.messages.WorkspaceDraftMissingHost
			case "duplicate-host":
				want = state.messages.WorkspaceDraftDuplicateHost
			}
			if state.modal != modalWorkspaceSave || state.workspaceSave.error != want {
				t.Fatalf("invalid composition: modal=%d error=%q, want %q",
					state.modal, state.workspaceSave.error, want)
			}
			if len(state.templates) != 0 {
				t.Fatalf("invalid composition added %+v", state.templates)
			}
			if _, ok := state.temporaryWorkspaceLayouts[workspaceID]; !ok {
				t.Fatal("invalid composition removed temporary marker")
			}
		})
	}
}

func TestWorkspaceConnectionStateCountsEmptyPanesSeparately(t *testing.T) {
	state, workspaceID, _ := launchWorkspaceWithConnections(t, []Host{
		testWorkspaceHost("web", "web.example.com"),
		testWorkspaceHost("db", "db.example.com"),
	})
	if _, err := state.deck.Insert(workspaceID, state.deck.Panes(workspaceID, nil)[0].ID,
		layoutcore.Horizontal, 0.5, layoutcore.Before); err != nil {
		t.Fatal(err)
	}
	status := state.workspaceConnectionState(workspaceID)
	if status.Empty != 1 || status.Pending != 0 || !workspaceCanBroadcast(status) {
		t.Fatalf("empty pane state = %+v", status)
	}
}

func TestSavedTemporaryWorkspaceCanBeAppliedAndReorderedAgain(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspaceID := launchTemporaryWorkspace(t, state, 1, 0)
	state.openWorkspaceSave(workspaceID)
	state.workspaceSave.name.Insert("Pair")
	state.submitWorkspaceSave()
	if len(state.templates) != 1 {
		t.Fatalf("templates = %+v", state.templates)
	}
	first := state.templates[0]
	world.configs = nil
	state.activeSection = sectionWorkspaces
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceTemplateOpen.At(0))
	if len(world.configs) != 2 || world.configs[0].Host != "prod.example.com" || world.configs[1].Host != "staging.example.com" {
		t.Fatalf("applied dials = %+v", world.configs)
	}
	if !slices.Equal(state.templates[0].HostIDs, first.HostIDs) || state.templates[0].Columns != first.Columns {
		t.Fatalf("applying changed template: %+v -> %+v", first, state.templates[0])
	}
}

func TestTemporaryWorkspaceSaveMarkerIsRemovedWhenTheWorkspaceEnds(t *testing.T) {
	t.Run("close tab", func(t *testing.T) {
		_, options := newWorld(t)
		state := newTestState(t, options)
		workspaceID := launchTemporaryWorkspace(t, state, 0)
		state.closeWorkspaceNow(workspaceID)
		if _, ok := state.temporaryWorkspaceLayouts[workspaceID]; ok {
			t.Fatal("closing the tab left its save marker")
		}
	})

	t.Run("close last pane", func(t *testing.T) {
		_, options := newWorld(t)
		state := newTestState(t, options)
		workspaceID := launchTemporaryWorkspace(t, state, 0)
		state.closeSession(uint32(state.deck.Panes(workspaceID, nil)[0].SessionID))
		if _, ok := state.temporaryWorkspaceLayouts[workspaceID]; ok {
			t.Fatal("closing the last pane left its save marker")
		}
	})

	t.Run("detach", func(t *testing.T) {
		_, options := newWorld(t)
		state := newTestState(t, options)
		workspaceID := launchTemporaryWorkspace(t, state, 0, 1)
		if _, ok := state.detachWorkspace(workspaceID); !ok {
			t.Fatal("detach failed")
		}
		if _, ok := state.temporaryWorkspaceLayouts[workspaceID]; ok {
			t.Fatal("detaching the tab left its save marker")
		}
	})
}

func workspaceCatalogIDs(state *State, indexes ...int) []string {
	ids := make([]string, len(indexes))
	for index, hostIndex := range indexes {
		ids[index] = state.hosts[hostIndex].CatalogID
	}
	return ids
}

func workspaceDraftColumnID(columns uint8) gfx.ID {
	switch columns {
	case 2:
		return hitWorkspaceDraftPresets.At(1)
	case 3:
		return hitWorkspaceDraftPresets.At(2)
	default:
		return hitWorkspaceDraftPresets.At(0)
	}
}
