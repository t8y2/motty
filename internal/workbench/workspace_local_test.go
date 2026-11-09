package workbench

import (
	layoutcore "motty/internal/workspace"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

func TestWorkspaceLocalReferenceCannotCollideWithASavedHost(t *testing.T) {
	if validCatalogID(workspaceLocalTerminalRef) {
		t.Fatal("local terminal reference overlaps the catalog ID namespace")
	}
	if got := workspaceHostReference(localHost("Renamed terminal")); got != workspaceLocalTerminalRef {
		t.Fatalf("ordinary or restored local terminal reference = %q", got)
	}
	if _, err := NewWorkspaceTemplate("Duplicate", []string{workspaceLocalTerminalRef, workspaceLocalTerminalRef}, layoutcore.GridSpec(1)); err != ErrWorkspaceTemplateDupID {
		t.Fatalf("duplicate local reference error = %v", err)
	}
}

func TestWorkspaceLocalTerminalCanBeSelectedSavedAndResolved(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.openWorkspaceDraft(workspaceDraftFixed)
	hosts := state.workspaceDraftHosts()
	if len(hosts) == 0 || hosts[0].ID != localHostID {
		t.Fatalf("the first workspace choice is not this computer: %+v", hosts)
	}
	localRef := hosts[0].CatalogID
	state.workspaceDraft.name.Insert("Local and production")
	state.Click(hitWorkspaceDraftHostList.At(0))
	state.Click(hitWorkspaceDraftHostList.At(1))
	state.Click(hitWorkspaceDraftSubmit)
	if len(state.templates) != 1 {
		t.Fatalf("templates = %+v, draft error = %q", state.templates, state.workspaceDraft.error)
	}
	template := state.templates[0]
	if !slices.Equal(template.HostIDs, []string{localRef, state.hosts[0].CatalogID}) {
		t.Fatalf("template references = %v", template.HostIDs)
	}
	path := testConfigPath(t)
	if err := saveState(path, savedState{WorkspaceTemplates: state.templates}); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := loadState(path)
	if err != nil || len(loaded.WorkspaceTemplates) != 1 || !slices.Equal(loaded.WorkspaceTemplates[0].HostIDs, template.HostIDs) {
		t.Fatalf("loaded templates = %+v, error = %v", loaded.WorkspaceTemplates, err)
	}
	// The local reference resolves on this computer even without any SSH hosts.
	state.hosts = nil
	_, name, _, _, available := state.workspaceCardText(workspaceArrangementItem{host: localRef})
	if !available || name != state.messages.LocalTerminalTitle {
		t.Fatalf("local template card = %q, available = %t", name, available)
	}
	state.openWorkspaceDraft(workspaceDraftTemporary)
	state.workspaceDraft.query.Insert(state.messages.LocalTerminalTitle)
	if matches := state.workspaceDraftHosts(); len(matches) != 1 || matches[0].ID != localHostID {
		t.Fatalf("local search = %+v", matches)
	}
}

func TestWorkspaceLocalTerminalLaunchesWithSSHAndBroadcasts(t *testing.T) {
	skipWithoutUnixShell(t)
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.localShell = "/bin/sh"
	state.openWorkspaceDraft(workspaceDraftTemporary)
	hosts := state.workspaceDraftHosts()
	if len(hosts) == 0 || hosts[0].ID != localHostID {
		t.Fatal("local terminal is missing from workspace choices")
	}
	localRef := hosts[0].CatalogID
	state.Click(hitWorkspaceDraftHostList.At(0))
	state.Click(hitWorkspaceDraftHostList.At(1))
	state.Click(hitWorkspaceDraftSubmit)
	if len(state.sessions) != 2 || len(world.conns) != 1 {
		t.Fatalf("sessions = %d, SSH dials = %d", len(state.sessions), len(world.conns))
	}
	workspace := state.deck.ActiveWorkspace()
	local, remote := &state.sessions[0], &state.sessions[1]
	if !local.isLocal() || remote.isLocal() {
		t.Fatal("workspace pane order or transport is wrong")
	}
	if conn, ok := local.conn.(*localConn); !ok || conn.shell != "/bin/sh" {
		t.Fatalf("local terminal does not use the configured shell: %T", local.conn)
	}
	finishWorkspaceConnection(t, state, world.conns[0])
	state.Wake()
	command := "printf 'workspace-%s\\n' 'local-ready'\r"
	state.typed(local, []byte(command))
	waitWorkspaceLocalOutput(t, state, local.ID, "workspace-local-ready")
	if got := world.conns[0].shell(t).typed(); got != "" {
		t.Fatalf("broadcast starts enabled: remote typed %q", got)
	}
	state.toggleBroadcast()
	if state.broadcast != workspace {
		t.Fatalf("mixed workspace cannot broadcast: %+v", state.workspaceConnectionState(workspace))
	}
	command = "printf 'workspace-%s\\n' 'broadcast-ready'\r"
	state.typed(remote, []byte(command))
	waitWorkspaceLocalOutput(t, state, local.ID, "workspace-broadcast-ready")
	state.typed(local, []byte("printf 'local-to-ssh\\n'\r"))
	if got := world.conns[0].shell(t).typed(); !strings.Contains(got, "local-to-ssh") {
		t.Fatalf("local input did not reach SSH: %q", got)
	}
	state.openWorkspaceArrangement()
	moveWorkspaceItem(state.workspaceLayoutDraft.items, 0, 1)
	state.applyWorkspaceArrangement()
	if got, err := state.workspaceSaveHostCatalogIDs(workspace); err != nil || !slices.Equal(got, []string{remote.host.CatalogID, localRef}) {
		t.Fatalf("save references after reordering = %v, %v", got, err)
	}
	if len(world.conns) != 1 || state.sessions[0].ID != local.ID {
		t.Fatal("reordering reconnected the workspace")
	}
	state.openWorkspaceSave(workspace)
	state.submitWorkspaceSave()
	if len(state.templates) != 1 || !slices.Equal(state.templates[0].HostIDs, []string{remote.host.CatalogID, localRef}) {
		t.Fatalf("saving the temporary workspace lost its local terminal: %+v", state.templates)
	}
	paintAt(state, gfx.Size{Width: 1200, Height: 800})
}

func TestWorkspaceLocalTemplateReopensFreshWithoutACatalog(t *testing.T) {
	skipWithoutUnixShell(t)
	options := testOptions(t)
	template := mustWorkspaceTemplate(t, "Local", []string{workspaceLocalTerminalRef}, 1)
	if err := saveState(options.StatePath, savedState{WorkspaceTemplates: []WorkspaceTemplate{template}}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	state.localShell = "/bin/sh"
	state.hosts = nil
	state.selectedWorkspaceTemplateID = template.ID
	state.applySelectedWorkspaceTemplate()
	state.applySelectedWorkspaceTemplate()
	if len(state.sessions) != 2 || !state.sessions[0].isLocal() || !state.sessions[1].isLocal() {
		t.Fatalf("reopened sessions = %+v", state.sessions)
	}
	first, second := &state.sessions[0], &state.sessions[1]
	if first.conn == second.conn || first.term.shell == second.term.shell {
		t.Fatal("reopening reused the existing local shell")
	}
	state.typed(second, []byte("printf 'fresh-%s\\n' 'local-shell'\r"))
	waitWorkspaceLocalOutput(t, state, second.ID, "fresh-local-shell")
	state.typed(second, []byte("exit\r"))
	deadline := time.Now().Add(5 * time.Second)
	for !second.term.ended && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(5 * time.Millisecond)
	}
	if got := workspaceSessionState(second); got != workspacePaneSettled {
		t.Fatalf("exited local terminal state = %d", got)
	}
}

func TestWorkspaceLocalShellFailureSettlesAndRetriesIndependently(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.localShell = filepath.Join(t.TempDir(), "missing-shell")
	state.templates = []WorkspaceTemplate{mustWorkspaceTemplate(t, "Mixed", []string{workspaceLocalTerminalRef, state.hosts[0].CatalogID}, 2)}
	state.selectedWorkspaceTemplateID = state.templates[0].ID
	state.applySelectedWorkspaceTemplate()
	if len(state.sessions) != 2 || len(world.conns) != 1 {
		t.Fatalf("sessions = %d, SSH dials = %d", len(state.sessions), len(world.conns))
	}
	finishWorkspaceConnection(t, state, world.conns[0])
	local, remote := &state.sessions[0], &state.sessions[1]
	if !local.shellFailed || workspaceSessionState(local) != workspacePaneFailed || workspaceSessionState(remote) != workspacePaneConnected {
		t.Fatalf("mixed workspace after local startup failed = %+v", state.workspaceConnectionState(state.deck.ActiveWorkspace()))
	}
	if !state.canReconnect(local) {
		t.Fatal("failed local shell has no retry path")
	}
	state.ActivatePane(local.ID)
	paintAt(state, gfx.Size{Width: 1200, Height: 800})
	state.Click(state.paneIdentity(local, hitConnRetry))
	if len(world.conns) != 1 || workspaceSessionState(remote) != workspacePaneConnected {
		t.Fatal("retrying the local terminal disturbed its SSH peer")
	}
}

func waitWorkspaceLocalOutput(t *testing.T, state *State, sessionID uint32, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		session, ok := state.sessionByIDPointer(sessionID)
		if ok && session.term != nil && strings.Contains(session.term.screen.Text(), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("local terminal did not produce %q", want)
}
