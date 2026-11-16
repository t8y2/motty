package workbench

import (
	"context"
	"errors"
	"fmt"
	"github.com/vivaui/viva/text"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/monitoring"
	"motty/internal/remotestats"
	"motty/internal/sshconn"
)

func dashboardTestState(t *testing.T) (*State, *fakeConn, *int) {
	t.Helper()
	dialed := 0
	f := &fakeConn{status: sshconn.Status{Version: 1, Phase: sshconn.PhaseConnected, System: sshconn.SystemUbuntu}}
	f.streamCommand = streamReadings(statsReadings...)
	f.runCommand = func(context.Context, string, int) ([]byte, error) {
		return []byte("info\t4\t0.1\t0.2\t0.3\t100\t6.8\ndisk\t104857600\t52428800\t52428800\t50%\t0\t/\n"), nil
	}
	opts := testOptions(t)
	opts.Dial = func(sshconn.Config) Connection { dialed++; return f }
	s := newTestState(t, opts)
	s.hosts = []Host{{ID: 1, CatalogID: "host", Label: "web", Address: "192.168.1.10", Port: 22, User: "deploy", System: sshconn.SystemUbuntu}}
	s.activeSection = sectionDashboard
	return s, f, &dialed
}

func TestDashboardHostSudoRequiresOwnConfirmationAndStopsOnDisconnect(t *testing.T) {
	state, fake, _ := dashboardTestState(t)
	createDashboard(t, state, "Production")
	state.Click(hitDashboardToggle)
	waitDashboard(t, state, state.dashboard.active)
	source := state.dashboard.sources["host"]
	if source == nil || source.session.sudoCommands {
		t.Fatal("dashboard was privileged before confirmation")
	}
	connection := &sudoRunnerTestConn{fakeConn: fake}
	connection.stream = fake.StreamCommand
	connection.run = fake.RunCommand
	source.session.conn = connection
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 700, Height: 650}} {
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		sudo, found := hitByID(&canvas, hitDashboardSudoList.At(1))
		terminal, terminalFound := hitByID(&canvas, hitDashboardTerminalList.At(1))
		if !found || !terminalFound || sudo.Bounds.Width <= 0 || sudo.Bounds.X+sudo.Bounds.Width > terminal.Bounds.X {
			t.Fatalf("dashboard sudo control overlaps the terminal at %+v", size)
		}
	}
	state.Click(hitDashboardSudoList.At(1))
	if state.modal != modalSudoSFTP || !state.sudoSFTP.dashboard || source.session.sudoCommands {
		t.Fatal("dashboard did not request independent consent")
	}
	state.Click(hitSudoSFTPCancel)
	if source.session.sudoCommands {
		t.Fatal("canceled dashboard authorization took effect")
	}
	state.Click(hitDashboardSudoList.At(1))
	state.sudoSFTP.password.Insert("dashboard-secret")
	state.Click(hitSudoSFTPEnable)
	if !source.session.sudoCommands || source.session.sudoPassword != "dashboard-secret" {
		t.Fatal("dashboard authorization was not applied to its source")
	}
	state.Wake()
	waitFor(t, state, func() bool {
		connection.mu.Lock()
		defer connection.mu.Unlock()
		return len(connection.commands) != 0
	})
	connection.mu.Lock()
	password := connection.password
	connection.mu.Unlock()
	if password != "dashboard-secret" {
		t.Fatal("dashboard collector did not use its own authorization")
	}
	source.blocked = true
	state.Click(hitDashboardSudoList.At(1))
	if source.blocked || source.session.sudoCommands || source.session.sudoPassword != "" {
		t.Fatal("disabling sudo did not restore ordinary monitoring after an authorization failure")
	}
	state.Click(hitDashboardSudoList.At(1))
	state.sudoSFTP.password.Insert("dashboard-secret")
	state.Click(hitSudoSFTPEnable)
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseClosed })
	state.Wake()
	if source.session.sudoCommands || source.session.sudoPassword != "" {
		t.Fatal("dashboard disconnect retained authorization")
	}
	state.closeDashboard()
}
func createDashboard(t *testing.T, s *State, name string) string {
	t.Helper()
	s.Click(hitDashboardNew)
	s.dashboard.editor.name.Insert(name)
	s.dashboard.editor.selected = []string{"host"}
	s.Click(hitDashboardDialogSave)
	if s.modal != modalNone {
		t.Fatalf("creation failed: %s", s.dashboard.editor.error)
	}
	return s.dashboard.active
}
func waitDashboard(t *testing.T, s *State, panel string) monitoring.Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.Wake()
		snap, _ := s.dashboard.engine.Read(panel, "host", 100, 0)
		if snap.HasSample {
			return snap
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no sample collected")
	return monitoring.Snapshot{}
}
func TestDashboardCreateValidatePersistAndRestoreWithoutConnecting(t *testing.T) {
	s, _, dialed := dashboardTestState(t)
	s.Click(hitDashboardNew)
	s.Click(hitDashboardDialogSave)
	if s.modal != modalDashboard || s.dashboard.editor.error == "" {
		t.Fatal("accepted an empty panel")
	}
	s.dashboard.editor.name.Insert("Production")
	s.Click(hitDashboardDialogSave)
	if s.modal != modalDashboard {
		t.Fatal("accepted no hosts")
	}
	s.dashboard.editor.selected = []string{"host"}
	s.Click(hitDashboardDialogSave)
	id := s.dashboard.active
	if *dialed != 0 || len(s.sessions) != 0 || s.dashboard.running[id] {
		t.Fatal("creating panel opened a connection or terminal")
	}
	s.Click(hitDashboardNew)
	s.dashboard.editor.name.Insert("production")
	s.dashboard.editor.selected = []string{"host"}
	s.Click(hitDashboardDialogSave)
	if s.modal != modalDashboard || s.dashboard.editor.error != s.messages.Dashboard.DuplicateName {
		t.Fatal("accepted duplicate name")
	}
	s.Click(hitDashboardDialogCancel)
	saved := s.snapshot()
	restored := newTestState(t, testOptions(t))
	restored.restoreLocal(saved)
	if len(restored.dashboard.panels) != 1 || restored.dashboard.active != id || len(restored.dashboard.engine.Wanted()) != 0 {
		t.Fatal("panel definition did not restore stopped")
	}
	_, local, _, err := splitState(saved, &configBase{}, s.configDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(local), "monitor_panels") {
		t.Fatal("panels were not device-local")
	}
}
func TestDashboardCollectsWithoutTerminalSharesSourcesAndStopsIndependently(t *testing.T) {
	s, f, dialed := dashboardTestState(t)
	a := createDashboard(t, s, "A")
	s.Click(hitDashboardToggle)
	snap := waitDashboard(t, s, a)
	if snap.Sample.CPUPercent != 37.5 || *dialed != 1 || len(s.sessions) != 0 || len(f.shells) != 0 {
		t.Fatal("monitoring lost real readings or opened a hidden terminal")
	}
	b := createDashboard(t, s, "B")
	s.Click(hitDashboardToggle)
	if *dialed != 1 || len(s.dashboard.sources) != 1 {
		t.Fatal("second panel duplicated connection")
	}
	s.dashboard.active = a
	s.Click(hitDashboardToggle)
	if len(s.dashboard.sources) != 1 {
		t.Fatal("stopping one panel stopped another")
	}
	s.activeSection = sectionHosts
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1280, Height: 900})
	s.Wake()
	if len(s.dashboard.sources) != 1 {
		t.Fatal("leaving page stopped sampling")
	}
	s.activeSection = sectionDashboard
	s.dashboard.active = b
	s.Click(hitDashboardToggle)
	if len(s.dashboard.sources) != 0 || f.canceled != 1 {
		t.Fatalf("last subscription leaked connection: %d", f.canceled)
	}
	after, _ := s.dashboard.engine.Read(a, "host", 100, 0)
	if !after.HasSample {
		t.Fatal("stopping erased history")
	}
}
func TestDashboardAuthenticationUsesExistingPromptWithoutTerminal(t *testing.T) {
	s, f, _ := dashboardTestState(t)
	f.status.Phase = sshconn.PhaseHostKeyPrompt
	f.status.HostKey = sshconn.HostKeyInfo{Address: "192.168.1.10:22"}
	createDashboard(t, s, "A")
	s.Click(hitDashboardToggle)
	s.Wake()
	if s.modal != modalHostKey || s.promptSession() == nil || s.promptSession().term != nil {
		t.Fatal("detached monitor did not enter normal host trust flow")
	}
	s.closeModal()
	s.closeDashboard()
	if len(s.dashboard.sources) != 0 {
		t.Fatal("authentication connection leaked")
	}
}

func TestDashboardUsesTheHostsSavedPasswordAtASharedEndpoint(t *testing.T) {
	s, f, _ := dashboardTestState(t)
	host := s.hosts[0]
	other := host
	other.ID, other.CatalogID = 2, "other"
	s.hosts = append(s.hosts, other)
	for id, secret := range map[string]string{host.CatalogID: "monitor secret", other.CatalogID: "other secret"} {
		if err := s.secrets.vault.Set(hostSecretName(id, hostPasswordPart), secret); err != nil {
			t.Fatal(err)
		}
	}
	f.status.Phase = sshconn.PhasePasswordPrompt
	f.status.Attempt = 1
	f.status.Endpoint = sshconn.Endpoint{Host: host.Address, Port: host.Port, User: host.User}
	f.status.Questions = []sshconn.Question{{Secret: true}}
	createDashboard(t, s, "A")
	s.Click(hitDashboardToggle)
	s.Wake()
	if s.modal != modalNone || len(s.sessions) != 0 || len(f.passwords) != 1 || f.passwords[0] != "monitor secret" {
		t.Fatalf("monitor did not use its host's password: modal=%d terminals=%d answers=%d", s.modal, len(s.sessions), len(f.passwords))
	}
}

func TestDashboardNetworkDisconnectFollowsReconnectSetting(t *testing.T) {
	s, f, _ := dashboardTestState(t)
	id := createDashboard(t, s, "A")
	s.connectionReconnect = reconnectThree
	s.Click(hitDashboardToggle)
	waitDashboard(t, s, id)
	f.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseClosed; status.Err = sshconn.ErrDisconnected })
	s.Wake()
	source := s.dashboard.sources["host"]
	if source.retry != 1 || source.retryAt <= s.now() {
		t.Fatal("network failure did not schedule configured reconnect")
	}
	s.closeDashboard()
	if source.timer != nil {
		t.Fatal("close retained reconnect timer")
	}
}
func TestDashboardUnsupportedCommandStopsUntilExplicitRetry(t *testing.T) {
	s, f, _ := dashboardTestState(t)
	f.streamCommand = func(context.Context, string, int, func([]byte) bool) error { return errors.New("exec request denied") }
	id := createDashboard(t, s, "A")
	s.Click(hitDashboardToggle)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.Wake()
		r, _ := s.dashboard.engine.Read(id, "host", 100, 0)
		if r.Status == monitoring.Failed {
			if !s.dashboard.sources["host"].blocked {
				t.Fatal("command refusal retried")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("command refusal not shown")
}

func TestDashboardEditAndDeleteLeaveOtherSubscriptionsRunning(t *testing.T) {
	s, f, dialed := dashboardTestState(t)
	a := createDashboard(t, s, "A")
	s.Click(hitDashboardToggle)
	waitDashboard(t, s, a)
	b := createDashboard(t, s, "B")
	s.Click(hitDashboardToggle)
	s.openDashboardEditor(true)
	s.dashboard.editor.name = text.Editor{}
	s.dashboard.editor.name.Insert("B renamed")
	s.dashboard.editor.note.Insert("Notes")
	s.submitDashboard()
	if s.dashboardPanel().Name != "B renamed" || s.dashboardPanel().Note != "Notes" || *dialed != 1 {
		t.Fatal("editing restarted source or lost fields")
	}
	s.deleteDashboard()
	s.Click(hitFormSubmit)
	if len(s.dashboard.panels) != 1 || s.dashboard.active != a || len(s.dashboard.sources) != 1 || f.canceled != 0 {
		t.Fatal("deleting one panel stopped shared source")
	}
	if _, ok := s.dashboard.engine.Read(b, "host", 100, 0); ok {
		t.Fatal("deleted panel retained history")
	}
	s.deleteDashboard()
	s.Click(hitFormSubmit)
	if len(s.dashboard.panels) != 0 || len(s.dashboard.sources) != 0 || f.canceled != 1 {
		t.Fatal("last deletion did not release source")
	}
}

func TestDashboardMissingHostsCanBeRemovedFromEditor(t *testing.T) {
	s, _, _ := dashboardTestState(t)
	createDashboard(t, s, "A")
	s.hosts = nil
	s.openDashboardEditor(true)
	s.Click(hitDashboardMissingList.At(0))
	if len(s.dashboard.editor.selected) != 0 {
		t.Fatal("could not remove deleted host")
	}
	s.submitDashboard()
	if s.modal != modalDashboard || s.dashboard.editor.error != s.messages.Dashboard.HostsRequired {
		t.Fatal("saved a panel with no hosts")
	}
}

func TestDashboardProtocolChangeReleasesConnectionAndCanBeRemoved(t *testing.T) {
	s, f, _ := dashboardTestState(t)
	id := createDashboard(t, s, "A")
	s.Click(hitDashboardToggle)
	waitDashboard(t, s, id)
	s.hosts[0].Protocol = protocolRDP
	s.hostDataChanged()
	if len(s.dashboard.sources) != 0 || f.canceled != 1 {
		t.Fatal("a non-SSH destination retained its old monitoring connection")
	}
	snap, _ := s.dashboard.engine.Read(id, "host", 100, 0)
	if snap.Status != monitoring.Unsupported {
		t.Fatal("protocol change did not mark the host unsupported")
	}
	s.openDashboardEditor(true)
	groups, _, shown := s.dashboardPickerGroups()
	if shown != 1 || len(groups) != 1 || len(groups[0].missing) != 1 {
		t.Fatal("unsupported selected host is hidden from the picker")
	}
	s.dashboard.editor.query.Insert("web")
	if _, _, shown := s.dashboardPickerGroups(); shown != 1 {
		t.Fatal("unsupported selected host cannot be searched by name")
	}
	s.Click(hitDashboardMissingList.At(0))
	if len(s.dashboard.editor.selected) != 0 {
		t.Fatal("unsupported host cannot be deselected")
	}
}

func TestDashboardSnapshotOwnsPanelDefinitions(t *testing.T) {
	s, _, _ := dashboardTestState(t)
	createDashboard(t, s, "A")
	saved := s.snapshot()
	s.dashboard.panels[0].Name = "Changed"
	s.dashboard.panels[0].Hosts[0] = "Changed host"
	if saved.MonitorPanels[0].Name != "A" || saved.MonitorPanels[0].Hosts[0] != "host" {
		t.Fatal("editing live panels mutated a saved snapshot")
	}
}

func TestDashboardReconnectUsesEditedEndpointAndRestartsCollector(t *testing.T) {
	s, f, dialed := dashboardTestState(t)
	id := createDashboard(t, s, "A")
	s.Click(hitDashboardToggle)
	waitDashboard(t, s, id)
	s.hosts[0].Address = "192.168.1.20"
	s.hostDataChanged()
	source := s.dashboard.sources["host"]
	if *dialed != 2 || f.canceled != 1 || source.session.endpoint.Host != "192.168.1.20" || source.collecting != 5 {
		t.Fatal("edited endpoint kept the old transport or left its collector stopped")
	}
}

func TestDashboardInvalidReconnectDoesNotRetainCancelledTransport(t *testing.T) {
	s, _, dialed := dashboardTestState(t)
	id := createDashboard(t, s, "A")
	s.Click(hitDashboardToggle)
	waitDashboard(t, s, id)
	s.proxies = []proxyRecord{{TargetID: 1, JumpID: 999}}
	source := s.dashboard.sources["host"]
	source.retryAt = time.Nanosecond
	s.connectDashboardSource(source)
	s.pumpDashboard()
	if source.session.conn != nil || source.collecting != 0 || source.retryAt != 0 || !source.blocked || *dialed != 1 {
		t.Fatal("invalid route kept a cancelled transport, active collector or retry timer")
	}
}

func TestDashboardRestoringStoppedDefinitionsRemovesOrphanHistory(t *testing.T) {
	s, _, _ := dashboardTestState(t)
	a := createDashboard(t, s, "A")
	b := createDashboard(t, s, "B")
	s.restoreDashboard([]monitoring.Panel{s.dashboard.panels[1]}, b)
	if _, ok := s.dashboard.engine.Read(a, "host", 100, 0); ok {
		t.Fatal("restoring removed definitions retained orphan history")
	}
	if _, ok := s.dashboard.engine.Read(b, "host", 100, 0); !ok {
		t.Fatal("restoring removed a retained panel")
	}
}

func TestDashboardLocalRefreshKeepsRunningUnavailablePanel(t *testing.T) {
	s, _, _ := dashboardTestState(t)
	a := createDashboard(t, s, "A")
	s.hosts = nil
	s.Click(hitDashboardToggle)
	s.restoreDashboard(nil, "")
	if len(s.dashboard.panels) != 1 || !s.dashboard.running[a] {
		t.Fatal("local refresh stopped a running panel with missing hosts")
	}
}

func TestDashboardWindowsCollectorReusesPowerShellAndEmbeddedDetails(t *testing.T) {
	s, f, _ := dashboardTestState(t)
	s.hosts[0].System = sshconn.SystemWindows
	f.status.System = sshconn.SystemWindows
	f.streamCommand = func(ctx context.Context, cmd string, limit int, line func([]byte) bool) error {
		if !strings.HasPrefix(cmd, "powershell.exe ") {
			t.Error("Windows host received Linux command")
		}
		line([]byte(`gop-stats-win-v1 {"cpu":25,"total":8589934592,"free":4294967296,"uptime":100,"networkAvailable":false,"cores":[{"id":"0","percent":25}],"meta":{"count":1,"kernel":"Windows","disks":[{"path":"C:\\","total":1000,"free":400}]}}`))
		<-ctx.Done()
		return ctx.Err()
	}
	f.runCommand = func(context.Context, string, int) ([]byte, error) {
		t.Error("Windows started Linux metadata polling")
		return nil, errors.New("unexpected")
	}
	id := createDashboard(t, s, "Windows")
	s.Click(hitDashboardToggle)
	r := waitDashboard(t, s, id)
	if r.Sample.CPUPercent != 25 || len(r.Details.Disks) != 1 || !r.Sample.NetworkUnavailable {
		t.Fatal("lost Windows metrics or embedded metadata")
	}
}

func TestDashboardExistingPasswordAndKeyDialogsFindDetachedConnections(t *testing.T) {
	for _, phase := range []sshconn.Phase{sshconn.PhasePasswordPrompt, sshconn.PhaseKeyPassphrasePrompt} {
		t.Run(fmt.Sprint(phase), func(t *testing.T) {
			s, f, _ := dashboardTestState(t)
			f.status.Phase = phase
			f.status.IdentityPath = "test_key"
			f.status.Questions = []sshconn.Question{{Text: "Password:", Secret: true}}
			createDashboard(t, s, "A")
			s.Click(hitDashboardToggle)
			s.Wake()
			v := s.promptSession()
			if v == nil || v.term != nil || v.conn != f || s.modal == modalNone {
				t.Fatal("existing authentication UI lost detached connection")
			}
		})
	}
}

func TestDashboardHostViewsSurvivePanelSwitchingAndUseKeyboardTabs(t *testing.T) {
	s, _, _ := dashboardTestState(t)
	a := createDashboard(t, s, "A")
	s.Click(hitDashboardSimple)
	result := s.Key(gfx.KeyEvent{Target: hitDashboardSimpleViewList.At(3), Key: gfx.KeyDown})
	if !result.Handled || result.Focus != hitDashboardSimpleViewList.At(4) {
		t.Fatal("icon tab did not support arrow keys")
	}
	s.Click(result.Focus)
	createDashboard(t, s, "B")
	s.Click(hitDashboardPanelList.At(0))
	if s.dashboard.active != a || s.dashboardRow(s.dashboardPanel(), "host", 1000).simplePage != 1 {
		t.Fatal("panel switching reset the host's own view")
	}
}

func TestDashboardStartupUsesAnObservableTimeRangeWithoutInventingPoints(t *testing.T) {
	s, _, _ := dashboardTestState(t)
	id := createDashboard(t, s, "A")
	p := s.dashboardPanel()
	s.dashboard.engine.Set(*p, true)
	at := time.Now()
	s.dashboard.engine.Publish("host", at, remotestats.Sample{CPUPercent: 20})
	s.dashboard.engine.Publish("host", at.Add(5*time.Second), remotestats.Sample{CPUPercent: 40})
	r := s.dashboardRow(p, "host", 1200)
	if len(r.x) != 2 || r.x[1]-r.x[0] < .05 || r.x[1] != 1 {
		t.Fatal("startup samples were fabricated or hidden in the empty retention window")
	}
	if r.snapshot.Version == 0 || p.ID != id {
		t.Fatal("lost current panel")
	}
}

func TestDashboardPageKeysScrollThePageFromAnyControl(t *testing.T) {
	s, _, _ := dashboardTestState(t)
	for i := 2; i <= 6; i++ {
		catalog := fmt.Sprintf("host%d", i)
		s.hosts = append(s.hosts, Host{ID: uint32(i), CatalogID: catalog, Label: catalog, Address: "10.0.0.1", Port: 22, User: "deploy", System: sshconn.SystemUbuntu})
	}
	createDashboard(t, s, "A")
	p := s.dashboardPanel()
	p.Hosts = append(p.Hosts, "host2", "host3", "host4", "host5", "host6")
	s.dashboard.engine.Set(*p, true)
	paintAt(s, gfx.Size{Width: 900, Height: 520})
	if s.dashboard.content <= s.dashboard.body.Height {
		t.Fatalf("content %v fits the body %v; the scroll check would prove nothing", s.dashboard.content, s.dashboard.body.Height)
	}
	if s.dashboard.scroll != 0 {
		t.Fatal("the page did not start at the top")
	}
	// The page switch button holds the focus; page keys must still scroll the
	// panel instead of requiring a Tab stop on the scroll viewport first.
	result := s.Key(gfx.KeyEvent{Key: gfx.KeyPageDown, Target: hitDashboardSwitch})
	if !result.Handled || s.dashboard.scroll <= 0 {
		t.Fatalf("PageDown on a control did not scroll the page: handled=%v scroll=%v", result.Handled, s.dashboard.scroll)
	}
}
