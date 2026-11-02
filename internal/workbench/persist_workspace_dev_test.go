//go:build dev

package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/backup"
	"motty/internal/configstore"
	layoutcore "motty/internal/workspace"
)

func saveDevelopmentWorkspace(t *testing.T, path, workspace string) {
	t.Helper()
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Commit(configstore.Change{Local: map[string][]byte{
		developmentWorkspaceKey: []byte(workspace),
	}}); err != nil {
		t.Fatal(err)
	}
}

func releaseWorkspaceFixture() savedState {
	return savedState{
		HostCatalogVersion: hostCatalogSchemaVersion,
		ShortcutVersion:    shortcutSchemaVersion,
		Theme:              "dark",
		Hosts: []savedHost{{
			ID: "release-host", Label: "Release Host", Address: "release.example.com", User: "me", Port: 22,
		}},
		Workspace:     "settings",
		ActiveSession: 1,
		Sessions: []savedSession{{
			Label: "Release Session", Address: "release.example.com", User: "me", Port: 22, SavedHost: true,
		}},
		Deck: workspaceDeck("Release", 1),
	}
}

func workspaceDeck(title string, session int) *layoutcore.DeckDTO {
	sessionID := layoutcore.SessionID(session)
	return &layoutcore.DeckDTO{
		Version:         layoutcore.DeckDTOVersion,
		ActiveWorkspace: 1,
		Workspaces: []layoutcore.WorkspaceDTO{{
			ID: 1, Title: title, ActivePane: 1,
			Layout: layoutcore.DTO{
				Version: layoutcore.DTOVersion,
				Root:    1,
				Nodes:   []layoutcore.NodeDTO{{ID: 1, Kind: layoutcore.KindLeaf, Session: sessionID}},
			},
			Panes:   []layoutcore.PaneDTO{{ID: 1, SessionID: sessionID}},
			Context: layoutcore.ContextStateDTO{},
		}},
	}
}

func TestDevelopmentRestoreDoesNotFallBackToReleaseWorkspace(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	saveReleaseState(t, options.StatePath, releaseWorkspaceFixture())

	state := newTestState(t, options)
	if len(state.sessions) != 0 || state.activeSessionID != 0 || state.activeSection != sectionHosts || state.deck.ActiveWorkspace() != 0 {
		t.Fatalf("development restored release workspace: sessions=%d active=%d section=%d deck=%d",
			len(state.sessions), state.activeSessionID, state.activeSection, state.deck.ActiveWorkspace())
	}
	if len(state.hosts) != 1 || state.hosts[0].Label != "Release Host" {
		t.Fatalf("shared hosts after clear = %+v", state.hosts)
	}
}

func TestDevelopmentRestoreTreatsAnEmptyWorkspaceObjectAsEmpty(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	saveReleaseState(t, options.StatePath, releaseWorkspaceFixture())
	saveDevelopmentWorkspace(t, options.StatePath, `{}`)

	state := newTestState(t, options)
	if len(state.sessions) != 0 || state.activeSection != sectionHosts || state.deck.ActiveWorkspace() != 0 {
		t.Fatalf("empty development workspace restored state: sessions=%d section=%d deck=%d",
			len(state.sessions), state.activeSection, state.deck.ActiveWorkspace())
	}
	if len(state.hosts) != 1 {
		t.Fatalf("shared hosts after empty workspace = %+v", state.hosts)
	}
}

func TestDevelopmentRestoreAppliesItsOwnWorkspace(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	saveReleaseState(t, options.StatePath, releaseWorkspaceFixture())
	saveDevelopmentWorkspace(t, options.StatePath, `{
		"workspace":"terminal",
		"active_session":1,
		"sessions":[{"label":"Dev Session","address":"dev.example.com","user":"me","port":22}]
	}`)

	state := newTestState(t, options)
	if len(state.sessions) != 1 || state.sessions[0].Title != "Dev Session" || state.sessions[0].Address != "me@dev.example.com:22" || state.activeSection != sectionTerminal {
		t.Fatalf("development workspace = sessions %+v, section %d", state.sessions, state.activeSection)
	}
}

func TestDevelopmentFullSavePreservesReleaseWorkspace(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	release := releaseWorkspaceFixture()
	saveReleaseState(t, options.StatePath, release)

	state := newTestState(t, options)
	next := state.snapshot()
	next.Theme = "light"
	next.TerminalFontSize = 17
	next.Workspace = "terminal"
	next.ActiveSession = 1
	next.Sessions = []savedSession{{Label: "Dev Session", Address: "dev.example.com", User: "me", Port: 22}}
	next.Deck = workspaceDeck("Dev", 1)
	if _, err := writeConfig(state.config, state.configBase, state.configDefaults, next); err != nil {
		t.Fatal(err)
	}

	gotRelease := loadReleaseState(t, options.StatePath)
	if gotRelease.Workspace != release.Workspace || gotRelease.ActiveSession != release.ActiveSession ||
		!reflect.DeepEqual(gotRelease.Sessions, release.Sessions) || !reflect.DeepEqual(gotRelease.Deck, release.Deck) {
		t.Fatalf("release workspace changed:\n got %+v\nwant %+v", gotRelease, release)
	}
	if gotRelease.Theme != "light" || gotRelease.TerminalFontSize != 17 {
		t.Fatalf("shared settings did not reach release state: theme=%q font=%d", gotRelease.Theme, gotRelease.TerminalFontSize)
	}

	gotDevelopment, report, err := loadState(options.StatePath)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("development load: %+v, %v", report, err)
	}
	if gotDevelopment.Workspace != "terminal" || gotDevelopment.ActiveSession != 1 ||
		len(gotDevelopment.Sessions) != 1 || gotDevelopment.Sessions[0].Label != "Dev Session" ||
		!reflect.DeepEqual(gotDevelopment.Deck, next.Deck) {
		t.Fatalf("development workspace = %+v", gotDevelopment)
	}
}

func TestDevelopmentFullSavePreservesMissingAndEmptyReleaseFields(t *testing.T) {
	tests := []struct {
		name    string
		release string
		present bool
	}{
		{name: "missing", release: `{"theme":"dark"}`},
		{
			name:    "empty",
			release: `{"theme":"dark","workspace":"","sessions":[],"active_session":0,"deck":null}`,
			present: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := freshOptions(t)
			options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
			saveReleaseState(t, options.StatePath, savedState{
				HostCatalogVersion: hostCatalogSchemaVersion,
				ShortcutVersion:    shortcutSchemaVersion,
				Hosts:              releaseWorkspaceFixture().Hosts,
			})
			setLocalConfigValue(t, options.StatePath, localKey, []byte(test.release))

			state := newTestState(t, options)
			next := state.snapshot()
			next.Theme = "light"
			next.Workspace = "terminal"
			if _, err := writeConfig(state.config, state.configBase, state.configDefaults, next); err != nil {
				t.Fatal(err)
			}

			var fields map[string]json.RawMessage
			if err := json.Unmarshal(localConfigValue(t, options.StatePath, localKey), &fields); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"workspace", "sessions", "active_session", "deck"} {
				raw, present := fields[name]
				if present != test.present {
					t.Fatalf("%s present = %t, want %t: %s", name, present, test.present, raw)
				}
			}
			if test.present {
				if string(fields["workspace"]) != `""` || string(fields["sessions"]) != `[]` ||
					string(fields["active_session"]) != `0` || string(fields["deck"]) != `null` {
					t.Fatalf("empty release fields changed: %s", localConfigValue(t, options.StatePath, localKey))
				}
			}
		})
	}
}

func TestDevelopmentFullSavePreservesUnknownLocalFields(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	release := releaseWorkspaceFixture()
	saveReleaseState(t, options.StatePath, release)

	var local map[string]json.RawMessage
	if err := json.Unmarshal(localConfigValue(t, options.StatePath, localKey), &local); err != nil {
		t.Fatal(err)
	}
	local["language"], _ = json.Marshal("zh-CN")
	local["future_local_value"], _ = json.Marshal(map[string]bool{"kept": true})
	localBytes, err := json.Marshal(local)
	if err != nil {
		t.Fatal(err)
	}
	setLocalConfigValue(t, options.StatePath, localKey, localBytes)

	state := newTestState(t, options)
	next := state.snapshot()
	next.Language = ""
	next.Theme = "light"
	next.Workspace = "terminal"
	if _, err := writeConfig(state.config, state.configBase, state.configDefaults, next); err != nil {
		t.Fatal(err)
	}

	var after map[string]json.RawMessage
	if err := json.Unmarshal(localConfigValue(t, options.StatePath, localKey), &after); err != nil {
		t.Fatal(err)
	}
	if got := string(after["future_local_value"]); got != `{"kept":true}` {
		t.Fatalf("unknown local field = %s", got)
	}
	if _, present := after["language"]; present {
		t.Fatalf("cleared known local field survived: %s", after["language"])
	}
	if got := string(after["workspace"]); got != `"settings"` {
		t.Fatalf("release workspace = %s", got)
	}
}

func developmentWorkspaceInitialState(t *testing.T, path string) savedState {
	t.Helper()
	saveReleaseState(t, path, releaseWorkspaceFixture())
	saveDevelopmentWorkspace(t, path, `{}`)
	store, saved, _, report, err := openConfig(path)
	if err != nil || store == nil || report.outcome != loadOK {
		t.Fatalf("open development config: store=%v report=%+v err=%v", store, report, err)
	}
	store.Close()
	return saved
}

func developmentWorkspaceChangedState(t *testing.T, before savedState) savedState {
	t.Helper()
	after := before
	after.Theme = "light"
	after.TerminalFontSize = 17
	after.TerminalScrollback = 9000
	after.Workspace = "terminal"
	after.ActiveSession = 1
	after.Sessions = []savedSession{{Label: "Dev Session", Address: "dev.example.com", User: "me", Port: 22}}
	after.Deck = workspaceDeck("Dev", 1)
	return after
}

func persistedDevelopmentConfig(t *testing.T, path string) (map[string]string, map[string]string) {
	t.Helper()
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries, local, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	records := make(map[string]string, len(entries))
	for _, entry := range entries {
		records[entry.Kind+"/"+entry.ID] = string(entry.Payload)
	}
	values := make(map[string]string, len(local))
	for key, value := range local {
		values[key] = string(value)
	}
	return records, values
}

func TestDevelopmentIncrementalSaveMatchesFullEncoding(t *testing.T) {
	run := func(t *testing.T, incremental bool) (map[string]string, map[string]string) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.db")
		before := developmentWorkspaceInitialState(t, path)
		after := developmentWorkspaceChangedState(t, before)
		store, saved, base, _, err := openConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if store != nil {
				store.Close()
			}
		}()
		if jsonOf(t, saved) != jsonOf(t, before) {
			t.Fatalf("development baseline changed before save")
		}
		if incremental {
			shared, handled, err := writeChangedFields(store, base, nil, changedSavedFields(saved, after))
			if err != nil || !handled || !shared {
				t.Fatalf("incremental save: shared=%t handled=%t err=%v", shared, handled, err)
			}
		} else {
			shared, err := writeConfig(store, base, nil, after)
			if err != nil || !shared {
				t.Fatalf("full save: shared=%t err=%v", shared, err)
			}
		}
		store.Close()
		store = nil
		return persistedDevelopmentConfig(t, path)
	}

	fullRecords, fullLocal := run(t, false)
	incrementalRecords, incrementalLocal := run(t, true)
	if !reflect.DeepEqual(incrementalRecords, fullRecords) {
		t.Fatalf("incremental records differ from full:\n incremental=%v\n full=%v", incrementalRecords, fullRecords)
	}
	if !reflect.DeepEqual(incrementalLocal, fullLocal) {
		t.Fatalf("incremental local values differ from full:\n incremental=%v\n full=%v", incrementalLocal, fullLocal)
	}

	var releaseFields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(incrementalLocal[localKey]), &releaseFields); err != nil {
		t.Fatal(err)
	}
	if string(releaseFields["workspace"]) != `"settings"` || !bytesContain(releaseFields["sessions"], "Release Session") {
		t.Fatalf("release workspace in state = %s", incrementalLocal[localKey])
	}
	if !bytesContain([]byte(incrementalLocal[developmentWorkspaceKey]), "Dev Session") {
		t.Fatalf("development workspace = %s", incrementalLocal[developmentWorkspaceKey])
	}
}

func TestDevelopmentSyncReloadRetainsItsWorkspaceBaseline(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	release := releaseWorkspaceFixture()
	saveReleaseState(t, options.StatePath, release)
	workspace := `{
		"workspace":"terminal",
		"active_session":1,
		"sessions":[{"label":"Dev Session","address":"dev.example.com","user":"me","port":22}],
		"deck":` + string(workspaceJSON(t, workspaceDeck("Dev Session", 1))) + `
	}`
	saveDevelopmentWorkspace(t, options.StatePath, workspace)

	state := newTestState(t, options)
	release.Hosts = append(release.Hosts, savedHost{
		Label: "Synced Host", Address: "synced.example.com", User: "me", Port: 22,
	})
	saveReleaseState(t, options.StatePath, release)
	if err := state.reloadShared(); err != nil {
		t.Fatal(err)
	}
	if !bytesContain(state.configBase.development, "Dev Session") {
		t.Fatalf("sync reload dropped the development baseline: %s", state.configBase.development)
	}

	state.activeSection = sectionHosts
	state.save()
	var development map[string]json.RawMessage
	if err := json.Unmarshal(localConfigValue(t, options.StatePath, developmentWorkspaceKey), &development); err != nil {
		t.Fatal(err)
	}
	if !bytesContain(development["sessions"], "Dev Session") || !bytesContain(development["deck"], `"title":"Dev Session"`) {
		t.Fatalf("incremental save after sync replaced the development workspace: %v", development)
	}
}

func bytesContain(raw []byte, text string) bool {
	return strings.Contains(string(raw), text)
}

func TestDevelopmentIncrementalNoOpDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	developmentWorkspaceInitialState(t, path)
	store, _, base, _, err := openConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	shared, handled, err := writeChangedFields(store, base, nil, nil)
	if err != nil || !handled || shared {
		t.Fatalf("no-op save: shared=%t handled=%t err=%v", shared, handled, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("a no-op incremental save rewrote the configuration")
	}
}

func TestDevelopmentWorkspaceIsStoredOnlyInLocalValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	developmentWorkspaceInitialState(t, path)
	records, local := persistedDevelopmentConfig(t, path)

	if _, ok := local[developmentWorkspaceKey]; !ok {
		t.Fatalf("development workspace is not a local value: %v", local)
	}
	for key := range records {
		if strings.Contains(key, developmentWorkspaceKey) {
			t.Fatalf("development workspace became a shared record: %s", key)
		}
	}
	if configFormat != 14 || stateFileVersion != 4 {
		t.Fatalf("unexpected formats: config=%d state=%d", configFormat, stateFileVersion)
	}
}

func TestExistingReleaseWritePreservesDevelopmentAndUnknownLocalValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	release := releaseWorkspaceFixture()
	saveReleaseState(t, path, release)
	saveDevelopmentWorkspace(t, path, `{"workspace":"debug"}`)
	const unknownKey = "future_local_value"
	unknown := []byte(`{"kept":true}`)
	setLocalConfigValue(t, path, unknownKey, unknown)
	development := append([]byte(nil), localConfigValue(t, path, developmentWorkspaceKey)...)

	release.Theme = "light"
	saveReleaseState(t, path, release)

	if got := localConfigValue(t, path, developmentWorkspaceKey); !reflect.DeepEqual(got, development) {
		t.Fatalf("release write changed development workspace:\n got %s\nwant %s", got, development)
	}
	if got := localConfigValue(t, path, unknownKey); !reflect.DeepEqual(got, unknown) {
		t.Fatalf("release write changed unknown local value:\n got %s\nwant %s", got, unknown)
	}
	if got := loadReleaseState(t, path); got.Theme != "light" {
		t.Fatalf("release write did not update its own state: theme=%q", got.Theme)
	}
}

func TestReleaseWorkspaceSaveAfterDevelopmentLaunchKeepsTheDevelopmentWorkspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	saveReleaseState(t, path, releaseWorkspaceFixture())
	saveDevelopmentWorkspace(t, path, `{
		"workspace":"terminal",
		"active_session":1,
		"sessions":[{"label":"Development Workspace B","address":"dev-b.example.com","user":"me","port":22}]
	}`)

	release := releaseWorkspaceFixture()
	release.Workspace = "keys"
	release.ActiveSession = 0
	release.Sessions = nil
	release.Deck = nil
	release.Theme = "light"
	saveReleaseState(t, path, release)

	options := freshOptions(t)
	options.StatePath = path
	state := newTestState(t, options)
	if len(state.sessions) != 1 || state.sessions[0].Title != "Development Workspace B" ||
		state.sessions[0].Address != "me@dev-b.example.com:22" || state.activeSection != sectionTerminal {
		t.Fatalf("development workspace after release save = sessions %+v, section %d", state.sessions, state.activeSection)
	}
	gotRelease := loadReleaseState(t, path)
	if gotRelease.Theme != "light" || gotRelease.Workspace != "keys" || len(gotRelease.Sessions) != 0 || gotRelease.Deck != nil {
		t.Fatalf("release workspace after save = %+v", gotRelease)
	}
}

func TestSharedBackupDoesNotContainTheDevelopmentWorkspace(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	options.Keystore = &memKeys{}
	saveReleaseState(t, options.StatePath, releaseWorkspaceFixture())
	saveDevelopmentWorkspace(t, options.StatePath, `{
		"workspace":"terminal",
		"active_session":1,
		"sessions":[{"label":"DevBackupOnlyMarker","address":"backup-dev.example.com","user":"me","port":22}]
	}`)

	state := newTestState(t, options)
	document, err := state.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range document.Records {
		if record.Kind == developmentWorkspaceKey || record.ID == developmentWorkspaceKey {
			t.Fatalf("development workspace became a backup record: %+v", record)
		}
	}
	plain, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if bytesContain(plain, developmentWorkspaceKey) || bytesContain(plain, "DevBackupOnlyMarker") {
		t.Fatalf("shared backup contains the development workspace: %s", plain)
	}

	sealed, err := backup.Seal(document, testBackupPassword)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := backup.Open(sealed, testBackupPassword)
	if err != nil {
		t.Fatal(err)
	}
	openedPlain, err := json.Marshal(opened)
	if err != nil {
		t.Fatal(err)
	}
	if bytesContain(openedPlain, developmentWorkspaceKey) || bytesContain(openedPlain, "DevBackupOnlyMarker") {
		t.Fatalf("sealed shared backup contains the development workspace: %s", openedPlain)
	}
}

func TestDevelopmentWorkspaceOnlySaveKeepsSharedRecordsAndDoesNotScheduleSync(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	release := releaseWorkspaceFixture()
	release.Sync = &savedSync{URL: "https://dav.example.com/dav"}
	saveReleaseState(t, options.StatePath, release)
	saveDevelopmentWorkspace(t, options.StatePath, `{}`)

	state := newTestState(t, options)
	state.syncDelay = time.Hour
	state.lastSaved, state.hasSaved = state.snapshot(), true
	beforeRecords, err := state.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	_, beforeLocal, err := state.config.Load()
	if err != nil {
		t.Fatal(err)
	}
	releaseState := append([]byte(nil), beforeLocal[localKey]...)

	state.activeSection = sectionDebug
	changes := changedSavedFields(state.lastSaved, state.snapshot())
	if len(changes) != 1 || changes[0].name != "workspace" {
		t.Fatalf("workspace-only change = %+v", changes)
	}
	state.save()

	afterRecords, err := state.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterRecords, beforeRecords) {
		t.Fatalf("development workspace save changed shared records:\n before=%v\n after=%v", beforeRecords, afterRecords)
	}
	if state.sync.timer != nil {
		t.Fatal("development workspace save scheduled a sync")
	}
	_, afterLocal, err := state.config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !bytesContain(afterLocal[developmentWorkspaceKey], `"debug"`) {
		t.Fatalf("development workspace was not saved: %s", afterLocal[developmentWorkspaceKey])
	}
	if !reflect.DeepEqual(afterLocal[localKey], releaseState) {
		t.Fatalf("development workspace save changed release state:\n got %s\nwant %s", afterLocal[localKey], releaseState)
	}
}

func TestDevelopmentWorkspaceDoesNotBypassTooNewProtection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	store, err := configstore.Open(path, configFormat+1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(configstore.Change{
		Put: []configstore.Entry{{
			Kind: "host", ID: "future", Payload: []byte(`{"d":{"label":"Future"}}`),
		}},
		Local: map[string][]byte{
			localKey:                []byte(`{"workspace":"terminal"}`),
			developmentWorkspaceKey: []byte(`{"workspace":"debug"}`),
		},
	}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	store.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	options := freshOptions(t)
	options.StatePath = path
	state := newTestState(t, options)
	if !state.saveBlocked || state.stateNotice.found == "" || state.stateNotice.supported == "" {
		t.Fatalf("too-new state was not protected: blocked=%t notice=%+v", state.saveBlocked, state.stateNotice)
	}
	state.activeSection = sectionDebug
	state.save()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("development workspace save changed a too-new configuration")
	}
}

func TestDevelopmentIncrementalCommitFailureDoesNotAdvanceBase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	before := developmentWorkspaceInitialState(t, path)
	after := developmentWorkspaceChangedState(t, before)
	store, saved, base, _, err := openConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	changes := changedSavedFields(saved, after)
	payloads := make(map[configstore.Key][]byte, len(base.payloads))
	for key, payload := range base.payloads {
		payloads[key] = append([]byte(nil), payload...)
	}
	baseLocal := append([]byte(nil), base.local...)
	baseDevelopment := append([]byte(nil), base.development...)
	baseMaxOrd := base.maxOrd
	store.Close()

	if _, handled, err := writeChangedFields(store, base, nil, changes); err == nil || !handled {
		t.Fatalf("closed store save: handled=%t err=%v", handled, err)
	}
	if !reflect.DeepEqual(base.payloads, payloads) || !reflect.DeepEqual(base.local, baseLocal) ||
		!reflect.DeepEqual(base.development, baseDevelopment) || base.maxOrd != baseMaxOrd {
		t.Fatal("a failed commit advanced the saved baseline")
	}

	store, err = configstore.OpenShared(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, handled, err := writeChangedFields(store, base, nil, changes); err != nil || !handled {
		t.Fatalf("retry: handled=%t err=%v", handled, err)
	}
	_, local := persistedDevelopmentConfig(t, path)
	if !bytesContain([]byte(local[developmentWorkspaceKey]), "Dev Session") {
		t.Fatalf("retry did not save the development workspace: %s", local[developmentWorkspaceKey])
	}
	if !bytesContain([]byte(local[localKey]), `"terminal_font_size":17`) {
		t.Fatalf("retry did not save common settings: %s", local[localKey])
	}
}

func TestDevelopmentGalleryWorkspaceRestores(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	saveReleaseState(t, options.StatePath, releaseWorkspaceFixture())
	saveDevelopmentWorkspace(t, options.StatePath, `{"workspace":"debug"}`)

	state := newTestState(t, options)
	if state.activeSection != sectionDebug {
		t.Fatalf("active section = %d, want the development gallery", state.activeSection)
	}
	_, texts := paintedTexts(state)
	if !contains(texts, state.messages.DebugTitle) {
		t.Fatalf("development gallery was not painted: %v", texts)
	}
}

func TestDevelopmentClosingEverySessionPersistsAnEmptyWorkspace(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	saveReleaseState(t, options.StatePath, releaseWorkspaceFixture())
	saveDevelopmentWorkspace(t, options.StatePath, `{
		"workspace":"terminal",
		"active_session":1,
		"sessions":[{"label":"Dev Session","address":"dev.example.com","user":"me","port":22}]
	}`)

	state := newTestState(t, options)
	if len(state.sessions) != 1 {
		t.Fatalf("development sessions before close = %+v", state.sessions)
	}
	state.closeWorkspace(state.deck.ActiveWorkspace())

	restored := newTestState(t, options)
	if len(restored.sessions) != 0 || restored.deck.ActiveWorkspace() != 0 || restored.activeSection != sectionHosts {
		t.Fatalf("empty development workspace restored sessions=%d deck=%d section=%d",
			len(restored.sessions), restored.deck.ActiveWorkspace(), restored.activeSection)
	}
	if len(restored.hosts) != 1 || restored.hosts[0].Label != "Release Host" {
		t.Fatalf("shared hosts after closing development sessions = %+v", restored.hosts)
	}
}

func TestDevelopmentWorkspaceCorruptionIsLocalAndRecoverable(t *testing.T) {
	invalidDeck := workspaceDeck("Broken", 1)
	invalidDeck.Workspaces[0].ActivePane = 2
	invalidDeckJSON, err := json.Marshal(invalidDeck)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		workspace string
	}{
		{name: "malformed JSON", workspace: `{"sessions":[`},
		{name: "invalid deck", workspace: `{"sessions":[{"label":"Dev","address":"dev.example.com","port":22}],"deck":` + string(invalidDeckJSON) + `}`},
		{name: "missing session", workspace: `{"sessions":[],"deck":` + string(workspaceJSON(t, workspaceDeck("Broken", 1))) + `}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := freshOptions(t)
			options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
			release := releaseWorkspaceFixture()
			saveReleaseState(t, options.StatePath, release)
			releaseLocal := append([]byte(nil), localConfigValue(t, options.StatePath, localKey)...)
			saveDevelopmentWorkspace(t, options.StatePath, test.workspace)
			probe, _, _, probeReport, err := openConfig(options.StatePath)
			if err != nil {
				t.Fatal(err)
			}
			probe.Close()
			if probeReport.workspaceErr == nil {
				t.Fatal("openConfig did not report the corrupt development workspace")
			}

			state := newTestState(t, options)
			if len(state.sessions) != 0 || state.deck.ActiveWorkspace() != 0 || state.activeSection != sectionHosts {
				t.Fatalf("corrupt development workspace restored state: sessions=%d deck=%d section=%d",
					len(state.sessions), state.deck.ActiveWorkspace(), state.activeSection)
			}
			if len(state.hosts) != 1 || state.hosts[0].Label != "Release Host" {
				t.Fatalf("shared hosts after corruption = %+v", state.hosts)
			}
			if state.modal != modalStateNotice || state.stateNotice.workspaceErr == "" {
				t.Fatalf("restore failure has no visible notice: modal=%d notice=%+v", state.modal, state.stateNotice)
			}
			var canvas ui.Canvas
			state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
			if state.stateNotice.title != state.messages.DevelopmentWorkspaceRestoreTitle ||
				!strings.Contains(state.stateNotice.body, "Could not restore the development workspace") {
				t.Fatalf("restore notice = %+v", state.stateNotice)
			}
			if state.saveBlocked || state.stateNotice.tooNew {
				t.Fatalf("local workspace error blocked saves: %+v", state.stateNotice)
			}
			entries, err := os.ReadDir(filepath.Dir(options.StatePath))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ".corrupt") {
					t.Fatalf("workspace corruption set aside the database as %s", entry.Name())
				}
			}
			var before, after map[string]json.RawMessage
			if err := json.Unmarshal(releaseLocal, &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(localConfigValue(t, options.StatePath, localKey), &after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("release local state changed after corruption:\n got %v\nwant %v", after, before)
			}

			state.save()
			repaired, report, err := loadState(options.StatePath)
			if err != nil || report.workspaceErr != nil {
				t.Fatalf("repaired development workspace: report=%+v err=%v", report, err)
			}
			if len(repaired.Sessions) != 0 || repaired.Deck != nil {
				t.Fatalf("repair retained corrupt workspace fields: sessions=%+v deck=%+v", repaired.Sessions, repaired.Deck)
			}
			gotRelease := loadReleaseState(t, options.StatePath)
			if !reflect.DeepEqual(gotRelease.Sessions, release.Sessions) || !reflect.DeepEqual(gotRelease.Deck, release.Deck) {
				t.Fatalf("repair changed release workspace: sessions=%+v deck=%+v", gotRelease.Sessions, gotRelease.Deck)
			}
		})
	}
}

func workspaceJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
