package workbench

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func TestWorkspaceDraftCreatesTemporaryAndFixedWorkspaces(t *testing.T) {
	t.Run("temporary", func(t *testing.T) {
		world, options := newWorld(t)
		state := newTestState(t, options)
		state.activeSection = sectionWorkspaces

		canvas, _ := paintAt(state, ui.Size{Width: 1400, Height: 900})
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
		if template.Name != "Production pair" || !slices.Equal(template.HostIDs, wantHosts) || template.Columns != 2 {
			t.Fatalf("template = %+v, want hosts %v", template, wantHosts)
		}
		if state.modal != modalNone || state.activeSection != sectionWorkspaces ||
			state.selectedWorkspaceTemplateID != template.ID {
			t.Fatalf("modal=%d section=%d selected=%q", state.modal, state.activeSection, state.selectedWorkspaceTemplateID)
		}
		canvas, _ := paintAt(state, ui.Size{Width: 1400, Height: 900})
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
	state.Click(hitWorkspaceDraftColumns3)
	state.Click(hitWorkspaceDraftSubmit)

	if len(world.configs) != 0 {
		t.Fatalf("saving the template dialed %d connections", len(world.configs))
	}
	if len(state.templates) != 1 {
		t.Fatalf("templates = %+v", state.templates)
	}
	template := state.templates[0]
	wantHosts := []string{state.hosts[0].CatalogID, state.hosts[1].CatalogID}
	if template.Name != "Production pair" || !slices.Equal(template.HostIDs, wantHosts) || template.Columns != 3 {
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
	allowed := map[string]bool{"id": true, "name": true, "host_ids": true, "columns": true}
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

	paintAt(state, ui.Size{Width: 1400, Height: 900})
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
	state.Click(hitWorkspaceDraftColumns3)
	want := hostCatalogIDs(state.hosts[:2])

	state.Click(hitWorkspaceDraftFixed)
	if got := state.workspaceDraftSelectedCatalogIDs(); !slices.Equal(got, want) {
		t.Fatalf("fixed mode selection = %v, want %v", got, want)
	}
	if state.workspaceDraft.columns != 3 {
		t.Fatalf("fixed mode columns = %d", state.workspaceDraft.columns)
	}

	state.Click(hitWorkspaceDraftTemporary)
	if got := state.workspaceDraftSelectedCatalogIDs(); !slices.Equal(got, want) {
		t.Fatalf("temporary mode selection = %v, want %v", got, want)
	}
	if state.workspaceDraft.columns != 3 {
		t.Fatalf("temporary mode columns = %d", state.workspaceDraft.columns)
	}
}

func TestWorkspaceDraftSearchAndClicksStillUseHostListOrder(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceNew)
	state.EditorFor(hitWorkspaceDraftSearch).Insert("example.com")
	paintAt(state, ui.Size{Width: 1400, Height: 900})
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

func TestWorkspaceDraftEnforcesTheEightHostLimitAndCatalogOrder(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	state.hosts = nil
	for index := 0; index < 9; index++ {
		host := Host{
			ID: uint32(index + 1), CatalogID: "host_" + string(rune('a'+index)),
			Label: "Host " + string(rune('A'+index)), Address: "host-" + string(rune('a'+index)) + ".example.com", Port: 22,
		}
		host.finish()
		state.hosts = append(state.hosts, host)
	}
	state.Click(hitWorkspaceNew)
	for index := 0; index < 8; index++ {
		state.Click(workspaceDraftHostID(index))
	}
	paintAt(state, ui.Size{Width: 1400, Height: 900})
	state.workspaceDraftScroll(ui.ScrollEvent{X: state.workspaceDraft.bounds.X + 20, Y: state.workspaceDraft.bounds.Y + 20, DeltaY: 1000})
	canvas, _ := paintAt(state, ui.Size{Width: 1400, Height: 900})
	if !panelHit(t, canvas, workspaceDraftHostID(8)).Disabled {
		t.Fatal("the ninth unselected host is not disabled")
	}
	state.Click(workspaceDraftHostID(8))
	if !reflect.DeepEqual(state.workspaceDraftSelectedCatalogIDs(), hostCatalogIDs(state.hosts[:8])) {
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
		want := DefaultWorkspaceColumns(index + 1)
		if got := state.workspaceDraft.columns; got != want {
			t.Fatalf("%d hosts selected gave %d columns, want %d", index+1, got, want)
		}
	}
	state.Click(hitWorkspaceDraftColumns3)
	state.Click(workspaceDraftHostID(4))
	if state.workspaceDraft.columns != 3 {
		t.Fatalf("explicit columns changed to %d", state.workspaceDraft.columns)
	}
	state.Click(workspaceDraftHostID(4))
	if state.workspaceDraft.columns != 3 {
		t.Fatalf("explicit columns changed after deselecting to %d", state.workspaceDraft.columns)
	}
}

func TestWorkspaceLibraryOffersBothCreationModes(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	canvas, _ := paintAt(state, ui.Size{Width: 1400, Height: 900})
	panelHit(t, canvas, hitWorkspaceNew)
	state.Click(hitWorkspaceNew)
	canvas, _ = paintAt(state, ui.Size{Width: 1400, Height: 900})
	panelHit(t, canvas, hitWorkspaceDraftTemporary)
	panelHit(t, canvas, hitWorkspaceDraftFixed)
	if state.modal != modalWorkspaceDraft {
		t.Fatalf("modal = %d", state.modal)
	}
	state.Click(hitWorkspaceDraftCancel)
	paintAt(state, ui.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceNew)
	state.Click(hitWorkspaceDraftFixed)
	if state.modal != modalWorkspaceDraft || state.workspaceDraft.mode != workspaceDraftFixed {
		t.Fatalf("fixed entry opened modal=%d mode=%d", state.modal, state.workspaceDraft.mode)
	}
}

func workspaceDraftHostID(index int) ui.ID {
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
