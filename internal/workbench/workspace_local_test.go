package workbench

import (
	layoutcore "motty/internal/workspace"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/localshell"
)

func TestWorkspaceLocalReferenceCannotCollideWithASavedHost(t *testing.T) {
	if validCatalogID(workspaceLocalTerminalRef) {
		t.Fatal("local terminal reference overlaps the catalog ID namespace")
	}
	if validCatalogID(workspaceLocalShellReference("/bin/bash")) {
		t.Fatal("local shell reference overlaps the catalog ID namespace")
	}
	if got := workspaceHostReference(localHost("Renamed terminal")); got != workspaceLocalTerminalRef {
		t.Fatalf("ordinary or restored local terminal reference = %q", got)
	}
	shelled := localHost("Renamed terminal")
	shelled.localShell = "/bin/bash"
	if got := workspaceHostReference(shelled); got != workspaceLocalShellReference("/bin/bash") {
		t.Fatalf("shell terminal reference = %q, want %q", got, workspaceLocalShellReference("/bin/bash"))
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

// workspaceShellProgram names a second shell for the computer the test binary
// runs on, so the template it builds is valid on every platform.
func workspaceShellPrograms() (string, string) {
	if runtime.GOOS == "windows" {
		return `C:\Program Files\PowerShell\7\pwsh.exe`, `C:\Windows\System32\cmd.exe`
	}
	return "/usr/bin/zsh", "/bin/bash"
}

func TestWorkspaceDraftListsEachLocalShellAndKeepsItInTheTemplate(t *testing.T) {
	program, second := workspaceShellPrograms()
	_, options := newWorld(t)
	state := newTestState(t, options)
	withShellChoices(t, state,
		[]localshell.Choice{{Name: "Alpha", Path: program, Default: true}, {Name: "Beta", Path: second}},
		localshell.WSLChoices([]string{"Ubuntu"}))
	state.localShell = program
	state.openWorkspaceDraft(workspaceDraftFixed)
	// The draft adopts the distributions as it is painted, like the picker does.
	waitUntil(t, state, "the WSL shell choices", func() bool {
		paintedTexts(state)
		return state.localShellMenu.listing == nil
	})

	hosts := state.workspaceDraftHosts()
	if len(hosts) != 3+len(state.hosts) || hosts[0].CatalogID != workspaceLocalTerminalRef {
		t.Fatalf("draft hosts = %v, want this computer, Beta, WSL: Ubuntu and the saved hosts", hostCatalogIDs(hosts))
	}
	beta, wsl := hosts[1], hosts[2]
	if beta.Label != "Beta" || beta.localShell != second || beta.CatalogID != workspaceLocalShellReference(second) {
		t.Fatalf("shell row = %+v, want Beta at %q", beta, second)
	}
	if wsl.Label != "WSL: Ubuntu" || wsl.localShell != localshell.WSLShell("Ubuntu") {
		t.Fatalf("WSL row = %+v", wsl)
	}
	state.workspaceDraft.query.Insert("wsl")
	if matches := state.workspaceDraftHosts(); len(matches) != 1 || matches[0].CatalogID != wsl.CatalogID {
		t.Fatalf("searching wsl = %v", hostCatalogIDs(matches))
	}

	// Selecting the shells alongside an SSH host saves references that carry
	// the shells and survive persistence.
	state.openWorkspaceDraft(workspaceDraftFixed)
	state.workspaceDraft.name.Insert("Shells")
	state.Click(hitWorkspaceDraftHostList.At(1))
	state.Click(hitWorkspaceDraftHostList.At(2))
	state.Click(hitWorkspaceDraftHostList.At(3))
	state.Click(hitWorkspaceDraftSubmit)
	if len(state.templates) != 1 {
		t.Fatalf("templates = %+v, draft error = %q", state.templates, state.workspaceDraft.error)
	}
	template := state.templates[0]
	want := []string{beta.CatalogID, wsl.CatalogID, state.hosts[0].CatalogID}
	if !slices.Equal(template.HostIDs, want) {
		t.Fatalf("template references = %v, want %v", template.HostIDs, want)
	}
	path := testConfigPath(t)
	if err := saveState(path, savedState{WorkspaceTemplates: state.templates}); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := loadState(path)
	if err != nil || len(loaded.WorkspaceTemplates) != 1 || !slices.Equal(loaded.WorkspaceTemplates[0].HostIDs, want) {
		t.Fatalf("loaded templates = %+v, error = %v", loaded.WorkspaceTemplates, err)
	}

	// The saved references resolve to hosts that run their own shell, and the
	// bare local reference is still the default shell.
	state.openWorkspaceDraftEdit(template.ID)
	if !state.workspaceDraft.selected[beta.CatalogID] || !state.workspaceDraft.selected[wsl.CatalogID] {
		t.Fatalf("edit selection lost the shells: %+v", state.workspaceDraft.selected)
	}
	plan, err := PlanWorkspaceLaunch(state.workspaceHosts(), template.HostIDs, template.Layout)
	if err != nil || len(plan.Hosts) != 3 || len(plan.Missing) != 0 {
		t.Fatalf("plan = %+v, error = %v", plan, err)
	}
	if plan.Hosts[0].localShell != second || plan.Hosts[1].localShell != localshell.WSLShell("Ubuntu") || plan.Hosts[2].ID == localHostID {
		t.Fatalf("planned hosts run %q %q local=%v", plan.Hosts[0].localShell, plan.Hosts[1].localShell, plan.Hosts[2].ID == localHostID)
	}
	plan, err = PlanWorkspaceLaunch(state.workspaceHosts(), []string{workspaceLocalTerminalRef}, layoutcore.GridSpec(1))
	if err != nil || len(plan.Hosts) != 1 || plan.Hosts[0].localShell != "" {
		t.Fatalf("bare local reference planned %+v, error = %v", plan.Hosts, err)
	}
}

func TestWorkspaceLaunchesTheChosenShellAndSavesItsReference(t *testing.T) {
	skipWithoutUnixShell(t)
	world, options := newWorld(t)
	state := newTestState(t, options)
	closeLocalShells(t, state)
	withShellChoices(t, state,
		[]localshell.Choice{{Name: "Alpha", Path: "/bin/sh", Default: true}, {Name: "Beta", Path: "/bin/bash"}}, nil)
	state.localShell = "/bin/sh"
	state.openWorkspaceDraft(workspaceDraftTemporary)
	state.Click(hitWorkspaceDraftHostList.At(1)) // Beta, not the default Alpha
	state.Click(hitWorkspaceDraftHostList.At(2)) // the first saved host
	state.Click(hitWorkspaceDraftSubmit)
	if len(state.sessions) != 2 || len(world.conns) != 1 {
		t.Fatalf("sessions = %d, SSH dials = %d", len(state.sessions), len(world.conns))
	}
	local, remote := &state.sessions[0], &state.sessions[1]
	if !local.isLocal() || remote.isLocal() || local.Title != "Beta" {
		t.Fatalf("workspace panes = %T %T %q", local.conn, remote.conn, local.Title)
	}
	if got := localShellOf(t, local); got != "/bin/bash" {
		t.Fatalf("the workspace ran %q, want the chosen /bin/bash", got)
	}
	finishWorkspaceConnection(t, state, world.conns[0])
	got, err := state.workspaceSaveHostCatalogIDs(state.deck.ActiveWorkspace())
	if err != nil || !slices.Equal(got, []string{workspaceLocalShellReference("/bin/bash"), remote.host.CatalogID}) {
		t.Fatalf("save references = %v, %v", got, err)
	}
}

func TestWorkspaceLaunchCatalogResolvesFilteredShells(t *testing.T) {
	program, second := workspaceShellPrograms()
	state := newTestState(t, testOptions(t))
	withShellChoices(t, state,
		[]localshell.Choice{{Name: "Alpha", Path: program, Default: true}, {Name: "Beta", Path: second}}, nil)
	state.localShell = program
	state.refreshLocalShells()
	ref := workspaceLocalShellReference(second)

	// Hidden: the draft drops the row, the launch catalog still resolves the
	// saved shell.
	state.hideShell(second, true)
	plan, err := PlanWorkspaceLaunch(state.workspaceLaunchCatalog([]string{ref}), []string{ref}, layoutcore.GridSpec(1))
	if err != nil || len(plan.Missing) != 0 || len(plan.Hosts) != 1 || plan.Hosts[0].localShell != second {
		t.Fatalf("hidden shell plan = %+v, error = %v", plan, err)
	}

	// Made the default: the sentinel covers the row, the pinned shell still
	// resolves.
	state.localShell = second
	plan, err = PlanWorkspaceLaunch(state.workspaceLaunchCatalog([]string{ref}), []string{ref}, layoutcore.GridSpec(1))
	if err != nil || len(plan.Missing) != 0 || len(plan.Hosts) != 1 || plan.Hosts[0].localShell != second {
		t.Fatalf("defaulted shell plan = %+v, error = %v", plan, err)
	}

	// A reference nothing resolves stays missing rather than gaining a pane:
	// nothing to launch, and the id lands in Missing for the warning.
	plan, err = PlanWorkspaceLaunch(state.workspaceLaunchCatalog([]string{"host_nope"}), []string{"host_nope"}, layoutcore.GridSpec(1))
	if len(plan.Missing) != 1 || len(plan.Hosts) != 0 || plan.Missing[0] != "host_nope" {
		t.Fatalf("unknown id plan = %+v", plan)
	}
}

func TestWorkspaceTemplateLaunchesShellsHiddenOrDefaultedAfterSaving(t *testing.T) {
	skipWithoutUnixShell(t)
	state := newTestState(t, testOptions(t))
	closeLocalShells(t, state)
	withShellChoices(t, state,
		[]localshell.Choice{{Name: "Alpha", Path: "/bin/sh", Default: true}, {Name: "Beta", Path: "/bin/bash"}}, nil)
	state.localShell = "/bin/sh"

	// Save a template that pins Beta, then hide it: the library still
	// launches the saved shell.
	template := mustWorkspaceTemplate(t, "Shell", []string{workspaceLocalShellReference("/bin/bash")}, 1)
	state.templates = []WorkspaceTemplate{template}
	state.selectedWorkspaceTemplateID = template.ID
	state.hideShell("/bin/bash", true)

	state.applySelectedWorkspaceTemplate()
	if len(state.sessions) != 1 {
		t.Fatalf("sessions = %d, want the hidden shell's pane", len(state.sessions))
	}
	session := &state.sessions[0]
	if got := localShellOf(t, session); got != "/bin/bash" || session.Title != "Beta" {
		t.Fatalf("ran %q titled %q, want hidden Beta /bin/bash", got, session.Title)
	}

	// Making Beta the default moves the sentinel onto it: the same template
	// still launches the pinned shell.
	state.localShell = "/bin/bash"
	state.applySelectedWorkspaceTemplate()
	if len(state.sessions) != 2 {
		t.Fatalf("sessions after second apply = %d", len(state.sessions))
	}
	if got := localShellOf(t, &state.sessions[1]); got != "/bin/bash" {
		t.Fatalf("reopened %q, want the pinned /bin/bash", got)
	}
}
