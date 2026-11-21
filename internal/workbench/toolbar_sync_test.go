package workbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"motty/internal/backup"
	"motty/internal/configstore"
	"motty/internal/i18n"
)

func customSyncToolbars() toolbarPreferences {
	var prefs toolbarPreferences
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		entries := slices.Clone(toolbarDefaults[group])
		for index := range entries {
			entries[index].Enabled = index < 5 && entries[index].Key != "latency"
		}
		prefs.layouts[group] = append(entries[2:], entries[:2]...)
	}
	prefs.restoreDock([]toolbarDockEntry{
		{Group: toolbarTerminal, Key: "notes", Zone: toolbarDockLeftTop},
		{Group: toolbarTerminal, Key: "files", Zone: toolbarDockBottomRight},
		{Group: toolbarWindow, Key: "settings", Zone: toolbarDockSideBottom},
	})
	return prefs
}

func setCustomSyncToolbars(state *State) {
	state.toolbars = customSyncToolbars()
	state.terminalToolbarPosition = terminalToolbarLeft
	state.terminalToolbarSize = terminalToolbarLarge
	state.save()
}

func assertCustomSyncToolbars(t *testing.T, state *State) {
	t.Helper()
	want := customSyncToolbars()
	if !reflect.DeepEqual(state.toolbars.snapshot(), want.snapshot()) {
		t.Fatalf("toolbar visibility, order or dock changed: got %+v, want %+v", state.toolbars.snapshot(), want.snapshot())
	}
	if state.terminalToolbarPosition != terminalToolbarLeft || state.terminalToolbarSize != terminalToolbarLarge {
		t.Fatalf("toolbar placement or size was not restored: %v, %v", state.terminalToolbarPosition, state.terminalToolbarSize)
	}
}

func TestToolbarPreferencesSyncToAnotherComputerAndSurviveRestart(t *testing.T) {
	server, dav := newFakeDAV(t)
	source, _, _ := backupTestState(t)
	setCustomSyncToolbars(source)
	configureSync(t, source, server)
	syncAndWait(t, source, hitSyncNow, source.messages.Tools.SyncDone)

	target, targetOptions, _ := backupTestState(t)
	target.setLanguage(i18n.TraditionalChinese)
	target.chooseAppearanceMode(appearancePinnedLight)
	target.localShell = "/this-computer/shell"
	configureSync(t, target, server)
	syncAndWait(t, target, hitSyncNow, target.messages.Tools.SyncDone)
	assertCustomSyncToolbars(t, target)
	if target.language != i18n.TraditionalChinese || target.appearanceMode != appearancePinnedLight || target.localShell != "/this-computer/shell" {
		t.Fatal("toolbar sync replaced device-local language, appearance or shell")
	}
	assertCustomSyncToolbars(t, newTestState(t, targetOptions))

	held := map[string]bool{}
	for _, record := range serverDocument(t, dav).Records {
		if record.Kind == kindSetting && record.ID == "toolbars" && !record.Deleted {
			held[record.ID] = true
		}
	}
	if len(held) != 1 {
		t.Fatalf("the cloud did not retain all toolbar preferences: %v", held)
	}
	// A new computer's untouched defaults must not outrank this configuration.
	syncAndWait(t, source, hitSyncNow, source.messages.Tools.SyncDone)
	assertCustomSyncToolbars(t, source)
}

func TestToolbarPreferencesHonorSettingsScopeAndSyncAnExplicitReset(t *testing.T) {
	server, _ := newFakeDAV(t)
	source, _, _ := backupTestState(t)
	setCustomSyncToolbars(source)
	configureSync(t, source, server)
	syncAndWait(t, source, hitSyncNow, source.messages.Tools.SyncDone)
	target, _, _ := backupTestState(t)
	configureSync(t, target, server)
	target.toggleSyncCategory(syncCategorySettings)
	syncAndWait(t, target, hitSyncNow, target.messages.Tools.SyncDone)
	if target.toolbars.snapshot() != nil || target.terminalToolbarPosition != terminalToolbarRight || target.terminalToolbarSize != terminalToolbarSmall {
		t.Fatal("closing settings sync still imported toolbar preferences")
	}
	target.toggleSyncCategory(syncCategorySettings)
	syncAndWait(t, target, hitSyncNow, target.messages.Tools.SyncDone)
	assertCustomSyncToolbars(t, target)

	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		entries := source.toolbars.customize(group)
		for index := range entries {
			entries[index].Enabled = false
		}
	}
	source.save()
	syncAndWait(t, source, hitSyncNow, source.messages.Tools.SyncDone)
	syncAndWait(t, target, hitSyncNow, target.messages.Tools.SyncDone)
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		for _, entry := range target.toolbars.entries(group) {
			if entry.Enabled {
				t.Fatalf("all-off was confused with defaults: group %v, entry %q", group, entry.Key)
			}
		}
	}

	source.toolbars = toolbarPreferences{}
	source.terminalToolbarPosition, source.terminalToolbarSize = terminalToolbarRight, terminalToolbarSmall
	source.save()
	syncAndWait(t, source, hitSyncNow, source.messages.Tools.SyncDone)
	syncAndWait(t, target, hitSyncNow, target.messages.Tools.SyncDone)
	if target.toolbars.snapshot() != nil || target.terminalToolbarPosition != terminalToolbarRight || target.terminalToolbarSize != terminalToolbarSmall {
		t.Fatal("resetting the source did not restore default toolbars on the other computer")
	}
}

func TestToolbarPreferencesKeepLayoutPositionAndSizeTogether(t *testing.T) {
	server, _ := newFakeDAV(t)
	target, _, _ := backupTestState(t)
	setCustomSyncToolbars(target)
	targetRecords, err := target.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	source, _, _ := backupTestState(t)
	var stamps []configstore.Stamp
	for _, record := range targetRecords {
		stamps = append(stamps, record.Stamp)
	}
	if err := source.config.Observe(stamps...); err != nil {
		t.Fatal(err)
	}
	entries := source.toolbars.customize(toolbarTerminal)
	entries[1].Enabled = false
	// This computer customized visibility while keeping its default right/small
	// placement. A destination's older left/large placement must not survive.
	source.save()
	configureSync(t, source, server)
	syncAndWait(t, source, hitSyncNow, source.messages.Tools.SyncDone)
	configureSync(t, target, server)
	syncAndWait(t, target, hitSyncNow, target.messages.Tools.SyncDone)
	if target.terminalToolbarPosition != terminalToolbarRight || target.terminalToolbarSize != terminalToolbarSmall ||
		!reflect.DeepEqual(target.toolbars.snapshot(), source.toolbars.snapshot()) {
		t.Fatal("the received layout was mixed with this computer's older toolbar placement or size")
	}
}

func TestToolbarPreferencesRoundTripThroughEncryptedBackup(t *testing.T) {
	source, _, _ := backupTestState(t)
	setCustomSyncToolbars(source)
	document, err := source.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := backup.Seal(document, testBackupPassword)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := backup.Open(sealed, testBackupPassword)
	if err != nil {
		t.Fatal(err)
	}
	target, _, _ := backupTestState(t)
	if _, err := target.applyBackup(opened); err != nil {
		t.Fatal(err)
	}
	assertCustomSyncToolbars(t, target)

	source.toolbars = toolbarPreferences{}
	source.terminalToolbarPosition, source.terminalToolbarSize = terminalToolbarRight, terminalToolbarSmall
	document, err = source.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.applyBackup(document); err != nil {
		t.Fatal(err)
	}
	if target.toolbars.snapshot() != nil || target.terminalToolbarPosition != terminalToolbarRight || target.terminalToolbarSize != terminalToolbarSmall {
		t.Fatal("a backup's explicit reset was mistaken for missing legacy preferences")
	}
}

func TestToolbarPreferencesRestoreFromTheCloud(t *testing.T) {
	server, _ := newFakeDAV(t)
	source, _, _ := backupTestState(t)
	setCustomSyncToolbars(source)
	configureSync(t, source, server)
	syncAndWait(t, source, hitSyncNow, source.messages.Tools.SyncDone)
	target, _, _ := backupTestState(t)
	configureSync(t, target, server)
	restoreRemote(t, target)
	waitRestored(t, target)
	if target.status != fmt.Sprintf(target.messages.Tools.BackupRestoredFmt, len(sampleHosts)) {
		t.Fatalf("cloud restore failed: %q", target.status)
	}
	assertCustomSyncToolbars(t, target)
}

func TestUntouchedToolbarDefaultsRoundTripThroughBackup(t *testing.T) {
	source, _, _ := backupTestState(t)
	document, err := source.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	records, err := source.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Kind == kindSetting && record.ID == "toolbars" && !record.Deleted {
			t.Fatal("export wrote untouched defaults into shared settings")
		}
	}
	sealed, err := backup.Seal(document, testBackupPassword)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := backup.Open(sealed, testBackupPassword)
	if err != nil {
		t.Fatal(err)
	}
	target, options, _ := backupTestState(t)
	setCustomSyncToolbars(target)
	if _, err := target.applyBackup(opened); err != nil {
		t.Fatal(err)
	}
	for _, state := range []*State{target, newTestState(t, options)} {
		if state.toolbars.snapshot() != nil || state.terminalToolbarPosition != terminalToolbarRight || state.terminalToolbarSize != terminalToolbarSmall {
			t.Fatal("the backup did not restore the untouched default toolbar layout")
		}
	}
}

func TestOldBackupWithoutToolbarRecordsKeepsCurrentLayout(t *testing.T) {
	source, _, _ := backupTestState(t)
	source.terminalScrollback = 10000
	document, err := source.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	document.Records = slices.DeleteFunc(document.Records, func(record configstore.Record) bool {
		return record.Kind == kindSetting && record.ID == "toolbars"
	})
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "custom"}[custom], func(t *testing.T) {
			target, _, _ := backupTestState(t)
			if custom {
				setCustomSyncToolbars(target)
			}
			target.terminalScrollback = 7
			target.save()
			if _, err := target.applyBackup(document); err != nil {
				t.Fatal(err)
			}
			if target.terminalScrollback != 10000 {
				t.Fatal("the old backup did not restore its other settings")
			}
			if custom {
				assertCustomSyncToolbars(t, target)
			} else if target.toolbars.snapshot() != nil {
				t.Fatal("the old backup invented a toolbar layout")
			}
		})
	}
}

func TestLegacyLocalToolbarPreferencesArePromotedAndSharedRecordsWin(t *testing.T) {
	for _, sharedReset := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy-custom", true: "shared-reset"}[sharedReset], func(t *testing.T) {
			_, options := newWorld(t)
			options.StatePath = filepath.Join(t.TempDir(), "config.db")
			prefs := customSyncToolbars()
			raw, err := json.Marshal(prefs.snapshot())
			if err != nil {
				t.Fatal(err)
			}
			var legacyToolbars map[string]json.RawMessage
			if err := json.Unmarshal(raw, &legacyToolbars); err != nil {
				t.Fatal(err)
			}
			legacyToolbars["future_toolbar"] = json.RawMessage(`true`)
			local, err := json.Marshal(map[string]any{
				"toolbars": legacyToolbars, "terminal_toolbar_position": "left", "terminal_toolbar_size": "large",
			})
			if err != nil {
				t.Fatal(err)
			}
			store, err := configstore.Open(options.StatePath, configFormat)
			if err != nil {
				t.Fatal(err)
			}
			change := configstore.Change{Local: map[string][]byte{localKey: local}}
			if sharedReset {
				raw, _ := json.Marshal(&savedToolbars{Position: "right", Size: "small"})
				payload, _ := json.Marshal(recordPayload{Data: raw})
				change.Put = append(change.Put, configstore.Entry{Kind: kindSetting, ID: "toolbars", Payload: payload})
			}
			if _, err := store.Commit(change); err != nil {
				store.Close()
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			state := newTestState(t, options)
			state.syncDelay, state.syncRetryBase = time.Hour, time.Hour
			if sharedReset {
				if state.toolbars.snapshot() != nil || state.terminalToolbarPosition != terminalToolbarRight || state.terminalToolbarSize != terminalToolbarSmall {
					t.Fatal("legacy local data overrode the shared reset")
				}
			} else {
				assertCustomSyncToolbars(t, state)
				server, _ := newFakeDAV(t)
				configureSync(t, state, server)
				syncAndWait(t, state, hitSyncNow, state.messages.Tools.SyncDone)
				target, _, _ := backupTestState(t)
				configureSync(t, target, server)
				syncAndWait(t, target, hitSyncNow, target.messages.Tools.SyncDone)
				assertCustomSyncToolbars(t, target)
			}
			state.save()
			_, locals, err := state.config.Load()
			if err != nil {
				t.Fatal(err)
			}
			var remaining map[string]json.RawMessage
			if err := json.Unmarshal(locals[localKey], &remaining); err != nil {
				t.Fatal(err)
			}
			var kept map[string]json.RawMessage
			if err := json.Unmarshal(remaining["toolbars"], &kept); err != nil || string(kept["future_toolbar"]) != "true" {
				t.Fatalf("unknown local toolbar metadata was lost: %s, %v", remaining["toolbars"], err)
			}
			for _, name := range []string{"window", "terminal", "sftp", "dock", "position", "size"} {
				if _, present := kept[name]; present {
					t.Fatalf("legacy toolbar field %q was not migrated", name)
				}
			}
			records, err := state.config.Records()
			if err != nil {
				t.Fatal(err)
			}
			for _, record := range records {
				if record.Kind == kindSetting && record.ID == "toolbars" && bytes.Contains(record.Payload, []byte("future_toolbar")) {
					t.Fatal("unknown device-local toolbar metadata leaked into sync")
				}
			}
		})
	}
}

func TestLegacyToolbarMigrationLetsStaleWindowsSaveIndependentFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	prefs := customSyncToolbars()
	local, err := json.Marshal(savedState{Toolbars: prefs.snapshot(), TerminalToolbarPosition: "left", TerminalToolbarSize: "large"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	_, err = initial.Commit(configstore.Change{Local: map[string][]byte{localKey: local}})
	if closeErr := initial.Close(); err != nil || closeErr != nil {
		t.Fatalf("seed legacy state: %v, %v", err, closeErr)
	}
	firstStore, first, firstBase, _, err := openConfig(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer firstStore.Close()
	secondStore, second, secondBase, _, err := openConfig(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	first.TerminalScrollback, second.CommandHistory = 10000, true
	if _, err := writeConfig(firstStore, firstBase, nil, first); err != nil {
		t.Fatal(err)
	}
	if _, err := writeConfig(secondStore, secondBase, nil, second); err != nil {
		t.Fatal(err)
	}
	saved, _, err := loadState(path)
	if err != nil || saved.TerminalScrollback != 10000 || !saved.CommandHistory ||
		!reflect.DeepEqual(saved.Toolbars, prefs.sharedSnapshot(terminalToolbarLeft, terminalToolbarLarge)) {
		t.Fatalf("legacy migration lost concurrent settings or toolbar preferences: %+v, %v", saved.Toolbars, err)
	}
}

func TestToolbarPreferencesReachOtherLocalWindows(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	setCustomSyncToolbars(pair.first)
	assertCustomSyncToolbars(t, pair.second)
}

func TestToolbarPreferencesRefitOpenTerminalsOnSharedReload(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	session.term.railScroll = 90
	state.toolbarDock.sideScroll[0] = 40
	saved := state.snapshot()
	saved.Toolbars.Position, saved.Toolbars.Size = "left", "large"
	state.restoreShared(saved)
	if session.term.railScroll != 0 || state.toolbarDock.sideScroll[0] != 0 {
		t.Fatal("received toolbar placement kept stale scrolling state")
	}
	geometry := state.layoutForSession(session.ID)
	if cols, rows := session.term.screen.Size(); cols != geometry.cols || rows != geometry.rows {
		t.Fatalf("open terminal was not resized: screen %dx%d, layout %dx%d", cols, rows, geometry.cols, geometry.rows)
	}
	// Another shared setting must not reset the toolbar's current scroll.
	session.term.railScroll, state.toolbarDock.sideScroll[0] = 30, 20
	saved = state.snapshot()
	saved.ConnectionKeepAliveSeconds++
	state.restoreShared(saved)
	if session.term.railScroll != 30 || state.toolbarDock.sideScroll[0] != 20 {
		t.Fatal("an unrelated shared setting reset toolbar scrolling")
	}
}
