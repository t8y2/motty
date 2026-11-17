package workbench

import (
	"bytes"
	"encoding/binary"
	"motty/internal/configstore"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	"motty/internal/sshconn"
)

// requirePrivateFile checks that a file is readable by its owner alone. Windows
// has no permission bits to check: a file there is private through the ACL of
// the user's profile directory, so the check is left out.
func requirePrivateFile(t testing.TB, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("%s mode = %v, want 0600", filepath.Base(path), info.Mode().Perm())
	}
}

// freshOptions starts the way a first run does: no state file at all.
func freshOptions(t testing.TB) Options {
	t.Helper()
	dir := t.TempDir()
	return Options{
		KnownHostsPath: filepath.Join(dir, "known_hosts"),
		Dial:           func(sshconn.Config) Connection { return &fakeConn{} },
		// Like newTestState, a test never opens the machine's keychain, including
		// the options a test hands to New itself.
		Keystore: &memKeys{},
		// The tests read English; the machine running them may not speak it.
		PreferredLanguages:    func() []string { return nil },
		SystemAppearance:      func() app.Appearance { return app.AppearanceUnknown },
		WatchSystemAppearance: func(func()) func() { return func() {} },
		SetNativeAppearance:   func(app.Appearance) {},
	}
}

func TestTwoWindowsSavingSideBySideBothPersist(t *testing.T) {
	_, options := newWorld(t)
	first := newTestState(t, options)
	second := newTestState(t, options)
	first.terminalFontSize = 15
	first.save()
	// The second window read the store before the first one wrote it.
	second.terminalFontSize = 16
	second.save()
	if strings.Contains(second.status, "Could not save") {
		t.Fatalf("the second window could not save: %q", second.status)
	}
	restarted := newTestState(t, options)
	if int(restarted.terminalFontSize) != 16 {
		t.Fatalf("terminalFontSize after restart = %v, want 16", restarted.terminalFontSize)
	}
}

func TestAStaleWindowDoesNotOverwriteTheOtherWindowsSharedEdit(t *testing.T) {
	_, options := newWorld(t)
	first := newTestState(t, options)
	second := newTestState(t, options)
	first.hosts[0].Label = "Edited elsewhere"
	first.save()
	// The second window still holds the old label; its own save must not put it
	// back, and its device-local choice must still be stored.
	second.terminalFontSize = 17
	second.save()
	restarted := newTestState(t, options)
	if restarted.hosts[0].Label != "Edited elsewhere" {
		t.Fatalf("a stale window overwrote the shared host: %q", restarted.hosts[0].Label)
	}
	if int(restarted.terminalFontSize) != 17 {
		t.Fatalf("the stale window's own change was lost: %v", restarted.terminalFontSize)
	}
}

func TestAStaleWindowKeepsTheTimesBothWindowsRecorded(t *testing.T) {
	_, options := newWorld(t)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	options.WallNow = func() time.Time { return now }
	first := newTestState(t, options)
	second := newTestState(t, options)
	first.rememberRecentConnection(&Session{host: first.hosts[0], recordRecent: true})
	now = now.Add(time.Hour)
	// The second window read the store before the first one recorded its success.
	second.rememberRecentConnection(&Session{host: second.hosts[1], recordRecent: true})
	restarted := newTestState(t, options)
	for _, id := range []string{first.hosts[0].CatalogID, second.hosts[1].CatalogID} {
		if _, ok := restarted.recentConnections[id]; !ok {
			t.Fatalf("recent time for %s missing: %v", id, restarted.recentConnections)
		}
	}
}

func TestFirstRunHasNoInventedHosts(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	if len(state.hosts) != 0 || state.selectedHostID != 0 {
		t.Fatalf("first run has hosts=%v selected=%d", state.hosts, state.selectedHostID)
	}
	canvas, texts := paintedTexts(state)
	if !contains(texts, state.messages.HostsEmpty) || !contains(texts, state.messages.HostsEmptySub) {
		t.Fatalf("the empty state is not shown: %v", texts)
	}
	var invite, connect bool
	for _, hit := range canvas.Hits() {
		invite = invite || (hit.ID == hitAddHostEmpty && !hit.Disabled)
		if hit.ID == hitConnect {
			connect = true
			if !hit.Disabled {
				t.Fatal("Connect is enabled with no host to connect to")
			}
		}
	}
	if !invite || !connect {
		t.Fatalf("invite=%v connect=%v", invite, connect)
	}

	state.Click(hitConnect)
	if len(state.sessions) != 0 {
		t.Fatal("Connect dialed with no host selected")
	}
	state.Click(hitAddHostEmpty)
	if state.modal != modalNewHost {
		t.Fatal("the empty-state button does not open the New Host dialog")
	}
}

func TestAddedHostsAndChoicesSurviveARestart(t *testing.T) {
	options := freshOptions(t)
	first := newTestState(t, options)
	first.Click(hitAddHost)
	typeInto(first, hitDialogLabel, "Edge")
	typeInto(first, hitDialogAddress, " edge.example.com ")
	typeInto(first, hitDialogUser, "ops")
	first.Click(hitDialogSubmit)
	if len(first.hosts) != 1 {
		t.Fatalf("host was not added: %q", first.dialogError)
	}
	useChinese(t, first)
	first.Click(hitTheme)

	second := newTestState(t, options)
	if len(second.hosts) != 1 {
		t.Fatalf("hosts after restart = %v", second.hosts)
	}
	host := second.hosts[0]
	if host.Label != "Edge" || host.Address != "edge.example.com" || host.User != "ops" || host.Port != 22 || host.target != "edge.example.com" {
		t.Fatalf("restored host = %+v", host)
	}
	if second.selectedHostID != 0 {
		t.Fatal("a host is selected before the user picked one")
	}
	if second.language != i18n.Chinese || second.appearance != appearanceLight {
		t.Fatalf("language=%v appearance=%v, want the saved choices", second.language, second.appearance)
	}
}

func TestDetectedHostSystemSurvivesARestart(t *testing.T) {
	world, options := newWorld(t)
	first := newTestState(t, options)
	fake := connect(t, first, world)
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseConnected
		status.System = sshconn.SystemUbuntu
	})
	first.Wake()
	if first.hosts[0].System != sshconn.SystemUbuntu || first.sessions[0].host.System != sshconn.SystemUbuntu {
		t.Fatalf("detected system was not applied: host=%v session=%v", first.hosts[0].System, first.sessions[0].host.System)
	}

	second := newTestState(t, options)
	if second.hosts[0].System != sshconn.SystemUbuntu || second.sessions[0].host.System != sshconn.SystemUbuntu {
		t.Fatalf("restored system: host=%v session=%v", second.hosts[0].System, second.sessions[0].host.System)
	}
}

func TestStateFileIsPrivateAndWrittenWithoutLeftovers(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.Click(hitTheme)

	requirePrivateFile(t, state.options.StatePath)
	entries, err := os.ReadDir(filepath.Dir(state.options.StatePath))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".tmp" {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestUnreadableStateIsKeptAndTheAppStartsEmpty(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	if err := os.WriteFile(options.StatePath, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if len(state.hosts) != 0 || state.modal != modalStateNotice || state.stateNotice.tooNew || state.saveBlocked {
		t.Fatalf("hosts=%v modal=%d notice=%+v blocked=%t", state.hosts, state.modal, state.stateNotice, state.saveBlocked)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if !strings.Contains(state.stateNotice.body, "state.json.corrupt") {
		t.Fatalf("the dialog does not say where the copy is: %q", state.stateNotice.body)
	}
	kept, err := os.ReadFile(options.StatePath + ".corrupt")
	if err != nil || string(kept) != "{ this is not json" {
		t.Fatalf("the unreadable file was not kept: %q %v", kept, err)
	}
	// Starting empty must not have clobbered anything: the next save is a new file.
	state.Click(hitStateNoticeOK)
	state.Click(hitTheme)
	if _, err := os.Stat(options.StatePath + ".corrupt"); err != nil {
		t.Fatal("saving removed the kept copy")
	}
}

func TestDamagedLocalStateIsSetAsideInsteadOfPanicking(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	saveReleaseState(t, options.StatePath, savedState{
		Version: stateFileVersion,
		Hosts:   []savedHost{{Label: "Kept", Address: "kept.example.com"}},
	})
	setLocalConfigValue(t, options.StatePath, localKey, []byte(`{"hosts":[`))

	state := newTestState(t, options)
	if len(state.hosts) != 0 || state.modal != modalStateNotice || state.saveBlocked {
		t.Fatalf("hosts=%v modal=%d blocked=%t", state.hosts, state.modal, state.saveBlocked)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if !strings.Contains(state.stateNotice.body, filepath.Base(options.StatePath)+".corrupt") {
		t.Fatalf("the dialog does not say where the copy is: %q", state.stateNotice.body)
	}
	if _, err := os.Stat(options.StatePath + ".corrupt"); err != nil {
		t.Fatalf("damaged configuration was not kept: %v", err)
	}
}

func TestASecondUnreadableFileDoesNotReplaceTheFirst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	for _, content := range []string{"first damaged file", "second damaged file", "third damaged file"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, report, err := loadState(path); err != nil || report.outcome != loadUnreadable || report.kept == "" {
			t.Fatalf("load %q: %+v, %v", content, report, err)
		}
	}
	for suffix, want := range map[string]string{".corrupt": "first damaged file", ".corrupt.2": "second damaged file", ".corrupt.3": "third damaged file"} {
		if got, err := os.ReadFile(path + suffix); err != nil || string(got) != want {
			t.Errorf("%s holds %q (%v), want %q", suffix, got, err, want)
		}
	}
}

// writeNewerStore makes a store the way a newer build might have left it: with
// payloads in a format this build does not know, or a schema it does not know.
func writeNewerStore(t *testing.T, path string, schema bool) []byte {
	t.Helper()
	store, err := configstore.Open(path, configFormat+1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(configstore.Change{Put: []configstore.Entry{{Kind: "host", ID: "h1", Payload: []byte(`{"d":{"label":"Keep Me"}}`)}}}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if schema {
		binary.BigEndian.PutUint32(content[60:64], 99) // user_version: a schema this build lacks
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return content
}

func TestStateFromANewerBuildIsLeftAloneAndNeverSavedOver(t *testing.T) {
	for name, schema := range map[string]bool{"payload format": false, "database schema": true} {
		t.Run(name, func(t *testing.T) {
			options := freshOptions(t)
			options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
			content := writeNewerStore(t, options.StatePath, schema)
			state := newTestState(t, options)
			if !state.saveBlocked || state.modal != modalStateNotice || !state.stateNotice.tooNew {
				t.Fatalf("blocked=%t modal=%d notice=%+v", state.saveBlocked, state.modal, state.stateNotice)
			}
			if state.stateNotice.found == "" || state.stateNotice.supported == "" {
				t.Fatalf("the dialog does not say which versions: %+v", state.stateNotice)
			}
			// The user can dismiss the dialog and carry on; nothing they do changes the file.
			state.Click(hitStateNoticeOK)
			state.Click(hitTheme)
			if got, err := os.ReadFile(options.StatePath); err != nil || !bytes.Equal(got, content) {
				t.Fatalf("the newer build's file was changed (%v)", err)
			}
			entries, _ := os.ReadDir(filepath.Dir(options.StatePath))
			for _, entry := range entries {
				if strings.Contains(entry.Name(), "corrupt") || strings.HasSuffix(entry.Name(), ".bak") || strings.HasSuffix(entry.Name(), ".tmp") {
					t.Errorf("unexpected file %s", entry.Name())
				}
			}
			// Changing something brings the explanation back, so the user is not
			// left thinking their changes are being kept.
			if state.modal != modalStateNotice {
				t.Fatal("a change that could not be saved did not say so")
			}
		})
	}
}

// TestHoveringTheStateNoticeBackdropLeavesItsCloseButtonAlone keeps the two
// targets of the dialog apart. The backdrop is the window-wide dismiss region
// and the corner button a control, so the pointer anywhere over the page must
// not paint the button as hovered; once both were one hit ID, it did.
func TestHoveringTheStateNoticeBackdropLeavesItsCloseButtonAlone(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	if err := os.WriteFile(options.StatePath, []byte("not a store"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if state.modal != modalStateNotice {
		t.Fatalf("modal = %d, want the state notice", state.modal)
	}
	size := gfx.Size{Width: 1200, Height: 800}
	var quiet gfx.Canvas
	state.Paint(&quiet, size)
	dismiss := gfx.ID{}
	for _, hit := range quiet.Hits() {
		if hit.Backdrop {
			dismiss = hit.ID
			break
		}
	}
	if dismiss.IsZero() {
		t.Fatal("the dialog has no backdrop to hover")
	}
	paintedWith := func(hovered gfx.ID) []gfx.Rectangle {
		var canvas gfx.Canvas
		canvas.SetInteraction(hovered, gfx.ID{}, gfx.ID{}, false)
		state.Paint(&canvas, size)
		return dialogLayerRectangles(&canvas)
	}
	if !slices.Equal(paintedWith(gfx.ID{}), paintedWith(dismiss)) {
		t.Fatal("hovering the backdrop paints the dialog's controls as hovered")
	}
	if slices.Equal(paintedWith(gfx.ID{}), paintedWith(hitStateNoticeClose)) {
		t.Fatal("the close button paints no hover look of its own")
	}
}

// dialogLayerRectangles is what the dialog painted: the page behind it is not
// part of the comparison.
func dialogLayerRectangles(canvas *gfx.Canvas) []gfx.Rectangle {
	rectangles := make([]gfx.Rectangle, 0, len(canvas.Rectangles()))
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Layer == dialogLayer {
			rectangles = append(rectangles, rectangle)
		}
	}
	return rectangles
}

func TestTheLastGoodStateIsKeptBesideTheFile(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	if err := saveState(options.StatePath, savedState{Hosts: []savedHost{{Label: "A", Address: "a.example.com"}}}); err != nil {
		t.Fatal(err)
	}
	good, _ := os.ReadFile(options.StatePath)
	state := newTestState(t, options)
	backup, err := os.ReadFile(options.StatePath + ".bak")
	if err != nil || !bytes.Equal(backup, good) {
		t.Fatalf("backup = %q (%v), want the loaded file", backup, err)
	}
	requirePrivateFile(t, options.StatePath+".bak")

	// The user deletes the host and the file now has none; the next start must
	// not replace the backup that still has it.
	state.hosts = nil
	state.save()
	newTestState(t, options)
	if backup, _ := os.ReadFile(options.StatePath + ".bak"); !bytes.Equal(backup, good) {
		t.Fatalf("an empty catalog replaced the backup: %q", backup)
	}
}

func TestAFirstRunMakesNoBackup(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	newTestState(t, options)
	if _, err := os.Stat(options.StatePath + ".bak"); err == nil {
		t.Fatal("a backup of nothing was made")
	}
}

func TestSavedEntriesWithoutALabelOrAddressAreSkipped(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	err := saveState(options.StatePath, savedState{Hosts: []savedHost{
		{Label: "", Address: "a.example.com"},
		{Label: "NoAddress"},
		{Label: "Ok", Address: "ok.example.com", Port: 2222},
	}})
	if err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if len(state.hosts) != 1 || state.hosts[0].Label != "Ok" || state.hosts[0].Port != 2222 || state.hosts[0].target != "ok.example.com:2222" {
		t.Fatalf("hosts = %+v", state.hosts)
	}
}

func TestAHostWithoutAUserSignsInAsTheSystemAccount(t *testing.T) {
	world, options := newWorld(t)
	if err := saveState(options.StatePath, savedState{Hosts: []savedHost{{Label: "Bare", Address: "bare.example.com"}}}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	state.systemUser = "me"
	openHost(state)
	if len(world.configs) != 1 || world.configs[0].User != "me" || world.configs[0].Port != 22 {
		t.Fatalf("dial config = %+v", world.configs)
	}
	if state.sessions[0].Address != "me@bare.example.com:22" {
		t.Fatalf("session address = %q", state.sessions[0].Address)
	}
}

func TestSavingOneSettingUsesTheIncrementalPath(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.connectionTimeout = 11 * time.Second
	state.save()
	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ConnectionTimeoutSeconds != 11 {
		t.Fatalf("timeout = %d, want 11", saved.ConnectionTimeoutSeconds)
	}

	// A mixed change must still go through the full encoder and keep both.
	state.connectionTimeout = 12 * time.Second
	state.hosts = append(state.hosts, Host{ID: state.nextHostID, CatalogID: "host_extra", Label: "extra", Address: "extra.example.com", User: "me", Port: 22})
	state.nextHostID++
	state.save()
	saved, _, err = loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ConnectionTimeoutSeconds != 12 || len(saved.Hosts) != 1 || saved.Hosts[0].Label != "extra" {
		t.Fatalf("mixed save = timeout %d hosts %+v", saved.ConnectionTimeoutSeconds, saved.Hosts)
	}
}

func TestSavingOneDeviceValueUsesTheLocalShortcut(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.language, state.languageChosen = i18n.Chinese, true
	state.save()
	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Language != "zh-CN" {
		t.Fatalf("language = %q, want zh-CN", saved.Language)
	}
	// The shortcut must not disturb the shared records written before it.
	state.connectionTimeout = 11 * time.Second
	state.save()
	saved, _, err = loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Language != "zh-CN" || saved.ConnectionTimeoutSeconds != 11 {
		t.Fatalf("after a shared save: language %q timeout %d", saved.Language, saved.ConnectionTimeoutSeconds)
	}
}

func TestSavingSeveralFieldsAtOnceKeepsAll(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.language, state.languageChosen = i18n.Chinese, true
	state.connectionTimeout = 11 * time.Second
	state.sftpParallel = 3
	state.hosts = append(state.hosts, Host{
		ID: state.nextHostID, CatalogID: "host_extra", Label: "extra", Address: "extra.example.com", User: "me", Port: 22,
	})
	state.nextHostID++
	state.save()
	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Language != "zh-CN" || saved.ConnectionTimeoutSeconds != 11 || saved.SFTPParallel != 3 {
		t.Fatalf("settings = language %q timeout %d parallel %d", saved.Language, saved.ConnectionTimeoutSeconds, saved.SFTPParallel)
	}
	if len(saved.Hosts) != 1 || saved.Hosts[0].Label != "extra" {
		t.Fatalf("hosts = %+v", saved.Hosts)
	}
}

func TestSavingHostGroupsUsesTheIncrementalPath(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.hostGroups = []hostGroup{{Name: "prod", Expanded: false}, {Name: "dev", Expanded: true}}
	state.save()
	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.HostGroups) != 2 || !saved.HostGroups[0].Collapsed || saved.HostGroups[1].Collapsed {
		t.Fatalf("groups after first save = %+v", saved.HostGroups)
	}

	state.hostGroups[0].Expanded = true // collapse state is device-local
	state.save()
	saved, _, err = loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.HostGroups) != 2 || saved.HostGroups[0].Collapsed || saved.HostGroups[1].Collapsed {
		t.Fatalf("groups after expanding = %+v", saved.HostGroups)
	}

	state.hostGroups = append(state.hostGroups, hostGroup{Name: "ops", Expanded: false})
	state.save()
	saved, _, err = loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.HostGroups) != 3 || saved.HostGroups[2].Name != "ops" || !saved.HostGroups[2].Collapsed {
		t.Fatalf("groups after adding = %+v", saved.HostGroups)
	}
}
