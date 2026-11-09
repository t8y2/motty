package workbench

import (
	"encoding/json"
	layoutcore "motty/internal/workspace"
	"reflect"
	"slices"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestWorkspaceDraftCreatesTemporaryAndFixedWorkspaces(t *testing.T) {
	t.Run("temporary", func(t *testing.T) {
		world, options := newWorld(t)
		state := newTestState(t, options)
		state.activeSection = sectionWorkspaces

		canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
		panelHit(t, canvas, hitWorkspaceNew)
		state.Click(hitWorkspaceNew)
		if state.modal != modalWorkspaceDraft || state.workspaceDraft.mode != workspaceDraftTemporary {
			t.Fatalf("new workspace opened modal=%d mode=%d", state.modal, state.workspaceDraft.mode)
		}

		state.Click(workspaceDraftHostID(1))
		state.Click(workspaceDraftHostID(0))
		state.Click(hitWorkspaceDraftSubmit)

		if len(world.configs) != 2 || world.configs[0].Host != "prod.example.com" || world.configs[1].Host != "staging.example.com" {
			t.Fatalf("dials = %+v, want host-list order", world.configs)
		}
		if len(state.templates) != 0 {
			t.Fatalf("temporary launch saved templates: %+v", state.templates)
		}
		if state.broadcast != 0 {
			t.Fatalf("temporary launch enabled broadcast: %d", state.broadcast)
		}
		if state.modal != modalNone || state.activeSection != sectionTerminal {
			t.Fatalf("modal=%d section=%d", state.modal, state.activeSection)
		}
	})

	t.Run("fixed", func(t *testing.T) {
		_, options := newWorld(t)
		state := newTestState(t, options)
		state.activeSection = sectionWorkspaces
		state.Click(hitWorkspaceNew)
		state.Click(hitWorkspaceDraftFixed)
		state.workspaceDraft.name.Insert("Production pair")
		state.Click(workspaceDraftHostID(1))
		state.Click(workspaceDraftHostID(0))
		state.Click(hitWorkspaceDraftSubmit)

		if len(state.templates) != 1 {
			t.Fatalf("templates = %+v", state.templates)
		}
		template := state.templates[0]
		wantHosts := []string{state.hosts[0].CatalogID, state.hosts[1].CatalogID}
		if template.Name != "Production pair" || !slices.Equal(template.HostIDs, wantHosts) || !isGrid(template.Layout, 2) {
			t.Fatalf("template = %+v, want hosts %v", template, wantHosts)
		}
		if state.modal != modalNone || state.activeSection != sectionWorkspaces ||
			state.selectedWorkspaceTemplateID != template.ID {
			t.Fatalf("modal=%d section=%d selected=%q", state.modal, state.activeSection, state.selectedWorkspaceTemplateID)
		}
		canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
		panelHit(t, canvas, hitWorkspaceTemplateRowList.At(0))
	})
}

func TestWorkspaceDraftSavesAndReappliesACompositionOnlyTemplate(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceNew)
	state.Click(hitWorkspaceDraftFixed)
	state.workspaceDraft.name.Insert("Production pair")
	state.Click(workspaceDraftHostID(1))
	state.Click(workspaceDraftHostID(0))
	state.Click(hitWorkspaceDraftPresets.At(2))
	state.Click(hitWorkspaceDraftSubmit)

	if len(world.configs) != 0 {
		t.Fatalf("saving the template dialed %d connections", len(world.configs))
	}
	if len(state.templates) != 1 {
		t.Fatalf("templates = %+v", state.templates)
	}
	template := state.templates[0]
	wantHosts := []string{state.hosts[0].CatalogID, state.hosts[1].CatalogID}
	if template.Name != "Production pair" || !slices.Equal(template.HostIDs, wantHosts) || !isGrid(template.Layout, 3) {
		t.Fatalf("template = %+v, want hosts %v", template, wantHosts)
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
			t.Fatalf("the saved template carries unexpected field %q: %s", field, raw)
		}
	}

	loaded, report, err := loadState(options.StatePath)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load: %+v, %v", report, err)
	}
	if len(loaded.WorkspaceTemplates) != 1 || !reflect.DeepEqual(loaded.WorkspaceTemplates[0], template) {
		t.Fatalf("loaded templates = %+v, want %+v", loaded.WorkspaceTemplates, template)
	}

	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceTemplateOpen.At(0))
	if len(world.configs) != 2 || world.configs[0].Host != "prod.example.com" || world.configs[1].Host != "staging.example.com" {
		t.Fatalf("dials = %+v, want saved host-list order", world.configs)
	}
	if state.activeSection != sectionTerminal || state.broadcast != 0 {
		t.Fatalf("section=%d broadcast=%d", state.activeSection, state.broadcast)
	}
}

func TestWorkspaceDraftSwitchingModesKeepsTheSharedSelection(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceNew)
	state.Click(workspaceDraftHostID(1))
	state.Click(workspaceDraftHostID(0))
	state.Click(hitWorkspaceDraftPresets.At(2))
	want := hostCatalogIDs(state.hosts[:2])

	state.Click(hitWorkspaceDraftFixed)
	if got := state.workspaceDraftSelectedCatalogIDs(); !slices.Equal(got, want) {
		t.Fatalf("fixed mode selection = %v, want %v", got, want)
	}
	if !isGrid(state.workspaceDraft.layout, 3) {
		t.Fatalf("fixed mode columns = %+v", state.workspaceDraft.layout)
	}

	state.Click(hitWorkspaceDraftTemporary)
	if got := state.workspaceDraftSelectedCatalogIDs(); !slices.Equal(got, want) {
		t.Fatalf("temporary mode selection = %v, want %v", got, want)
	}
	if !isGrid(state.workspaceDraft.layout, 3) {
		t.Fatalf("temporary mode columns = %+v", state.workspaceDraft.layout)
	}
}

func TestWorkspaceDraftSearchAndClicksStillUseHostListOrder(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceNew)
	state.EditorFor(hitWorkspaceDraftSearch).Insert("example.com")
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceDraftHostList.At(1))
	state.Click(hitWorkspaceDraftHostList.At(0))
	state.Click(hitWorkspaceDraftSubmit)

	if len(world.configs) != 2 || world.configs[0].Host != "prod.example.com" || world.configs[1].Host != "staging.example.com" {
		t.Fatalf("dials after filtered clicks = %+v", world.configs)
	}
}

func TestWorkspaceDraftOffersThisComputerAndOnlyEligibleSSHHosts(t *testing.T) {
	state := newTestState(t, testOptions(t))
	local := Host{ID: localHostID, CatalogID: "host_local", Label: "This computer", Address: "local"}
	desktop := Host{
		ID: 90, CatalogID: "host_desktop", Label: "Desktop", Address: "desktop.example.com",
		Port: 5900, Protocol: protocolVNC,
	}
	telnet := Host{
		ID: 91, CatalogID: "host_telnet", Label: "Switch", Address: "switch.example.com",
		Port: 23, Protocol: protocolTelnet,
	}
	missingID := Host{
		ID: 92, Label: "No stable id", Address: "unstable.example.com", Port: 22,
	}
	local.finish()
	desktop.finish()
	telnet.finish()
	missingID.finish()
	missingID.CatalogID = ""
	state.hosts = append(state.hosts, local, desktop, telnet, missingID)

	hosts := state.workspaceDraftHosts()
	if len(hosts) != len(sampleHosts)+1 || hosts[0].ID != localHostID {
		t.Fatalf("draft hosts = %+v", hosts)
	}
	want := hostCatalogIDs(state.hosts[:len(sampleHosts)])
	for _, host := range hosts[1:] {
		if host.ID == localHostID || !host.isSSH() || !validCatalogID(host.CatalogID) || host.CatalogID == "" {
			t.Fatalf("draft offered ineligible host %+v", host)
		}
	}
	if got := hostCatalogIDs(hosts[1:]); !slices.Equal(got, want) {
		t.Fatalf("draft host order = %v, want %v", got, want)
	}

	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceNew)
	state.workspaceDraft.query.Insert("desktop")
	if got := state.workspaceDraftHosts(); len(got) != 0 {
		t.Fatalf("draft search offered a VNC host: %+v", got)
	}
	state.workspaceDraft.query = text.Editor{}
	state.workspaceDraft.query.Insert("switch")
	if got := state.workspaceDraftHosts(); len(got) != 0 {
		t.Fatalf("draft search offered a Telnet host: %+v", got)
	}
}

func TestWorkspaceDraftEnforcesTheTwelveHostLimitAndCatalogOrder(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	state.hosts = nil
	for index := 0; index <= maxWorkspaceTemplateHosts; index++ {
		host := Host{
			ID: uint32(index + 1), CatalogID: "host_" + string(rune('a'+index)),
			Label: "Host " + string(rune('A'+index)), Address: "host-" + string(rune('a'+index)) + ".example.com", Port: 22,
		}
		host.finish()
		state.hosts = append(state.hosts, host)
	}
	state.Click(hitWorkspaceNew)
	for index := 0; index < maxWorkspaceTemplateHosts; index++ {
		state.Click(workspaceDraftHostID(index))
	}
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.workspaceDraftScroll(gfx.ScrollEvent{X: state.workspaceDraft.bounds.X + 20, Y: state.workspaceDraft.bounds.Y + 20, DeltaY: 1000})
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if !panelHit(t, canvas, workspaceDraftHostID(maxWorkspaceTemplateHosts)).Disabled {
		t.Fatal("the first host past the limit is not disabled")
	}
	state.Click(workspaceDraftHostID(maxWorkspaceTemplateHosts))
	if !reflect.DeepEqual(state.workspaceDraftSelectedCatalogIDs(), hostCatalogIDs(state.hosts[:maxWorkspaceTemplateHosts])) {
		t.Fatalf("selection = %v", state.workspaceDraftSelectedCatalogIDs())
	}
}

func TestWorkspaceDraftRequiresAtLeastOneHost(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceNew)
	state.Click(hitWorkspaceDraftSubmit)
	if state.modal != modalWorkspaceDraft || state.workspaceDraft.error != state.messages.WorkspaceDraftHostsRequired {
		t.Fatalf("modal=%d error=%q", state.modal, state.workspaceDraft.error)
	}
	if len(world.configs) != 0 || len(state.sessions) != 0 {
		t.Fatalf("empty draft dialed=%d sessions=%d", len(world.configs), len(state.sessions))
	}
}

func TestWorkspaceDraftEditCopiesTemplateAndPreservesMissingReferences(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", []string{
		state.hosts[0].CatalogID, "host_deleted", state.hosts[1].CatalogID,
	}, 2)
	state.templates = []WorkspaceTemplate{template}
	state.openWorkspaceDraftEdit(template.ID)

	if state.modal != modalWorkspaceDraft || state.workspaceDraft.mode != workspaceDraftEdit {
		t.Fatalf("edit opened modal=%d mode=%d", state.modal, state.workspaceDraft.mode)
	}
	if state.workspaceDraft.editID != template.ID || state.workspaceDraft.name.String() != template.Name ||
		!state.workspaceDraft.layout.Equal(template.Layout) || !slices.Equal(state.workspaceDraft.order, template.HostIDs) {
		t.Fatalf("edit draft = %+v, want template %+v", state.workspaceDraft, template)
	}

	state.workspaceDraft.name = *text.NewEditor("Critical")
	state.workspaceDraftRemoveItem(1)
	if !slices.Equal(state.workspaceDraft.order, []string{state.hosts[0].CatalogID, state.hosts[1].CatalogID}) {
		t.Fatalf("explicit removal left order %v", state.workspaceDraft.order)
	}
	state.workspaceDraft.name = *text.NewEditor(template.Name)
	if !slices.Equal(state.workspaceDraft.order, []string{state.hosts[0].CatalogID, state.hosts[1].CatalogID}) {
		t.Fatalf("removed missing reference came back: %v", state.workspaceDraft.order)
	}
}

func TestWorkspaceDraftEditRejectsInvalidBoundariesWithoutChangingTheTemplate(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", []string{
		state.hosts[0].CatalogID, "host_deleted",
	}, 2)
	state.templates = []WorkspaceTemplate{template}
	zero, four := uint8(0), uint8(4)

	tests := []struct {
		name   string
		setup  func()
		column *uint8
	}{
		{name: "empty members", setup: func() { state.workspaceDraftRemoveItem(1); state.workspaceDraftRemoveItem(0) }},
		{name: "duplicate members", setup: func() { state.workspaceDraft.order = append(state.workspaceDraft.order, state.workspaceDraft.order[0]) }},
		{name: "too many members", setup: func() {
			state.workspaceDraft.order = []string{
				"host_1", "host_2", "host_3", "host_4", "host_5", "host_6", "host_7", "host_8", "host_9", "host_10", "host_11", "host_12", "host_13",
			}
		}},
		{name: "zero columns", setup: func() {}, column: &zero},
		{name: "four columns", setup: func() {}, column: &four},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state.templates = []WorkspaceTemplate{template}
			state.openWorkspaceDraftEdit(template.ID)
			test.setup()
			if test.column != nil {
				state.workspaceDraft.layout = columnsSpec(*test.column)
			}
			state.submitWorkspaceDraft()
			if state.modal != modalWorkspaceDraft || state.workspaceDraft.error == "" {
				t.Fatalf("invalid edit closed with error %q", state.workspaceDraft.error)
			}
			if !reflect.DeepEqual(state.templates, []WorkspaceTemplate{template}) {
				t.Fatalf("invalid edit changed templates: %+v", state.templates)
			}
		})
	}
}

func TestWorkspaceDraftEditUpdatesTheSameTemplateAndPersists(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", []string{
		state.hosts[0].CatalogID, "host_deleted", state.hosts[1].CatalogID,
	}, 2)
	state.templates = []WorkspaceTemplate{template}
	state.openWorkspaceDraftEdit(template.ID)
	state.workspaceDraft.name = *text.NewEditor("Critical")
	state.workspaceDraftRemoveItem(1)
	if next, ok := components.SegmentedControlKeyDown(hitWorkspaceDraftPresets.At(1),
		[]components.SegmentOption{
			{ID: hitWorkspaceDraftPresets.At(0)}, {ID: hitWorkspaceDraftPresets.At(1)}, {ID: hitWorkspaceDraftPresets.At(2)},
		}, components.DirectionLTR, gfx.KeyRight); !ok || next != hitWorkspaceDraftPresets.At(2) {
		t.Fatalf("columns test setup = %v, %v", next, ok)
	}
	state.Click(hitWorkspaceDraftPresets.At(2))
	state.submitWorkspaceDraft()

	if state.modal != modalNone || len(state.templates) != 1 {
		t.Fatalf("edit closed modal=%d templates=%+v", state.modal, state.templates)
	}
	got := state.templates[0]
	if got.ID != template.ID || got.Name != "Critical" || !isGrid(got.Layout, 3) ||
		!slices.Equal(got.HostIDs, []string{state.hosts[0].CatalogID, state.hosts[1].CatalogID}) {
		t.Fatalf("edited template = %+v", got)
	}
	loaded, report, err := loadState(options.StatePath)
	if err != nil || report.outcome != loadOK || !reflect.DeepEqual(loaded.WorkspaceTemplates, state.templates) {
		t.Fatalf("loaded edit: %+v outcome=%v err=%v", loaded.WorkspaceTemplates, report.outcome, err)
	}
}

func TestWorkspaceDraftFailedEditRestoresTemplateContents(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	previousConfig := state.config
	t.Cleanup(func() { state.config = previousConfig })
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", []string{
		state.hosts[0].CatalogID, "host_deleted", state.hosts[1].CatalogID,
	}, 2)
	state.templates = []WorkspaceTemplate{template}
	state.openWorkspaceDraftEdit(template.ID)
	state.workspaceDraft.name = *text.NewEditor("Critical")
	state.workspaceDraft.order = []string{state.hosts[1].CatalogID}
	state.workspaceDraft.layout = layoutcore.GridSpec(3)
	before := cloneWorkspaceTemplates(state.templates)
	beforeSelected := state.selectedWorkspaceTemplateID
	beforeExpanded := state.workspaceLibraryExpanded
	state.config = nil

	state.submitWorkspaceDraft()

	if state.modal != modalWorkspaceDraft || state.workspaceDraft.error != state.messages.WorkspaceSaveFailed {
		t.Fatalf("failed edit: modal=%d error=%q", state.modal, state.workspaceDraft.error)
	}
	if !reflect.DeepEqual(state.templates, before) || state.selectedWorkspaceTemplateID != beforeSelected ||
		state.workspaceLibraryExpanded != beforeExpanded {
		t.Fatalf("failed edit changed library: templates=%+v selected=%q expanded=%t",
			state.templates, state.selectedWorkspaceTemplateID, state.workspaceLibraryExpanded)
	}
	if state.templates[0].HostIDs[1] != "host_deleted" {
		t.Fatalf("rollback left a mutated host slice: %v", state.templates[0].HostIDs)
	}
	if state.workspaceDraft.name.String() != "Critical" ||
		!slices.Equal(state.workspaceDraft.order, []string{state.hosts[1].CatalogID}) {
		t.Fatal("failed edit discarded the user's input")
	}
}

func TestWorkspaceDraftEditMissingTargetDoesNotInsertTheDraft(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", []string{state.hosts[0].CatalogID}, 1)
	other := mustWorkspaceTemplate(t, "Database", []string{state.hosts[1].CatalogID}, 1)
	state.templates = []WorkspaceTemplate{template, other}
	state.openWorkspaceDraftEdit(template.ID)
	state.workspaceDraft.name = *text.NewEditor("Renamed")
	state.templates = []WorkspaceTemplate{other}

	state.submitWorkspaceDraft()

	if state.modal != modalWorkspaceDraft || state.workspaceDraft.error != state.messages.WorkspaceTemplateEditMissing {
		t.Fatalf("stale target: modal=%d error=%q", state.modal, state.workspaceDraft.error)
	}
	if !reflect.DeepEqual(state.templates, []WorkspaceTemplate{other}) {
		t.Fatalf("stale target inserted or changed a template: %+v", state.templates)
	}
}

func TestWorkspaceDraftEditCancelDoesNotChangeAnOpenWorkspace(t *testing.T) {
	for _, test := range []struct {
		name   string
		cancel func(*State)
	}{
		{"cancel", func(state *State) { state.Click(hitWorkspaceDraftCancel) }},
		{"dismiss", func(state *State) { state.Click(hitWorkspaceDraftDismiss) }},
		{"escape", func(state *State) { state.Key(gfx.KeyEvent{Key: gfx.KeyEscape}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			world, options := newWorld(t)
			state := newTestState(t, options)
			state.activeSection = sectionWorkspaces
			template := mustWorkspaceTemplate(t, "Production", workspaceCatalogIDs(state, 0, 1), 2)
			state.templates = []WorkspaceTemplate{template}
			state.selectedWorkspaceTemplateID = template.ID
			state.applySelectedWorkspaceTemplate()
			workspace := state.deck.ActiveWorkspace()
			active := state.activeSessionID
			panes := state.deck.Panes(workspace, nil)
			context, _ := state.deck.Context(workspace)
			state.activeSection = sectionWorkspaces
			state.openWorkspaceDraftEdit(template.ID)
			state.workspaceDraft.name = *text.NewEditor("Renamed")
			state.workspaceDraftRemoveItem(0)
			test.cancel(state)

			if state.modal != modalNone || !reflect.DeepEqual(state.templates, []WorkspaceTemplate{template}) {
				t.Fatal("cancel changed the saved template")
			}
			gotContext, _ := state.deck.Context(workspace)
			if state.activeSessionID != active || !reflect.DeepEqual(state.deck.Panes(workspace, nil), panes) ||
				gotContext != context || len(world.configs) != 2 {
				t.Fatal("cancel changed the open workspace")
			}
		})
	}
}

func TestWorkspaceDraftEditTogglesCurrentHostsAtTheEndOfTheSavedOrder(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", []string{
		state.hosts[1].CatalogID, "host_deleted",
	}, 2)
	state.templates = []WorkspaceTemplate{template}
	state.openWorkspaceDraftEdit(template.ID)
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(workspaceDraftHostID(0))
	if want := []string{state.hosts[1].CatalogID, "host_deleted", state.hosts[0].CatalogID}; !slices.Equal(state.workspaceDraft.order, want) {
		t.Fatalf("adding a host = %v, want %v", state.workspaceDraft.order, want)
	}
	state.Click(workspaceDraftHostID(0))
	if want := []string{state.hosts[1].CatalogID, "host_deleted"}; !slices.Equal(state.workspaceDraft.order, want) {
		t.Fatalf("removing a host = %v, want %v", state.workspaceDraft.order, want)
	}
}

func TestWorkspaceDraftEditPreviewAndReorderKeepMissingReferences(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", []string{
		state.hosts[0].CatalogID, "host_deleted", state.hosts[1].CatalogID,
	}, 2)
	state.templates = []WorkspaceTemplate{template}
	state.openWorkspaceDraftEdit(template.ID)
	paintAt(state, gfx.Size{Width: 1400, Height: 900})

	items := state.workspaceItemsForRegion(workspaceCardRegion{view: workspaceCardsDraft})
	if len(items) != 3 || items[1].host != "host_deleted" {
		t.Fatalf("preview items = %+v", items)
	}
	region := workspaceCardRegion{view: workspaceCardsDraft, index: 0}
	state.moveWorkspaceCard(region, 2)
	want := []string{"host_deleted", state.hosts[1].CatalogID, state.hosts[0].CatalogID}
	if !slices.Equal(state.workspaceDraft.order, want) {
		t.Fatalf("reordered draft = %v, want %v", state.workspaceDraft.order, want)
	}
}

func TestWorkspaceDraftRejectsAnEmptyNameWithoutClosing(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceNew)
	state.Click(workspaceDraftHostID(0))
	state.Click(hitWorkspaceDraftFixed)
	state.Click(hitWorkspaceDraftSubmit)
	if state.modal != modalWorkspaceDraft || state.workspaceDraft.error == "" || len(state.templates) != 0 {
		t.Fatalf("modal=%d error=%q templates=%d", state.modal, state.workspaceDraft.error, len(state.templates))
	}
}

func TestWorkspaceDraftDefaultColumnsFollowSelectionUntilChosen(t *testing.T) {
	state := manyHostsState(t, 5)
	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceNew)
	for index := 0; index < 4; index++ {
		state.Click(workspaceDraftHostID(index))
		want := DefaultWorkspaceLayout(index + 1)
		if got := state.workspaceDraft.layout; !got.Equal(want) {
			t.Fatalf("%d hosts selected gave %+v, want %+v", index+1, got, want)
		}
	}
	state.Click(hitWorkspaceDraftPresets.At(2))
	state.Click(workspaceDraftHostID(4))
	if !isGrid(state.workspaceDraft.layout, 3) {
		t.Fatalf("explicit columns changed to %+v", state.workspaceDraft.layout)
	}
	state.Click(workspaceDraftHostID(4))
	if !isGrid(state.workspaceDraft.layout, 3) {
		t.Fatalf("explicit columns changed after deselecting to %+v", state.workspaceDraft.layout)
	}
}

func TestWorkspaceLibraryOffersBothCreationModes(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	panelHit(t, canvas, hitWorkspaceNew)
	state.Click(hitWorkspaceNew)
	canvas, _ = paintAt(state, gfx.Size{Width: 1400, Height: 900})
	panelHit(t, canvas, hitWorkspaceDraftTemporary)
	panelHit(t, canvas, hitWorkspaceDraftFixed)
	if state.modal != modalWorkspaceDraft {
		t.Fatalf("modal = %d", state.modal)
	}
	state.Click(hitWorkspaceDraftCancel)
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceNew)
	state.Click(hitWorkspaceDraftFixed)
	if state.modal != modalWorkspaceDraft || state.workspaceDraft.mode != workspaceDraftFixed {
		t.Fatalf("fixed entry opened modal=%d mode=%d", state.modal, state.workspaceDraft.mode)
	}
}

func workspaceDraftHostID(index int) gfx.ID {
	// Unfiltered choices put this computer before the saved SSH hosts.
	return hitWorkspaceDraftHostList.At(index + 1)
}

func hostCatalogIDs(hosts []Host) []string {
	ids := make([]string, len(hosts))
	for index := range hosts {
		ids[index] = hosts[index].CatalogID
	}
	return ids
}
