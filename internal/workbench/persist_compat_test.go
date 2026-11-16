package workbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"motty/internal/backup"
	"motty/internal/buildinfo"
	"motty/internal/configstore"
)

const futureHost = `{"o":1,"future_envelope":true,"d":{"id":"future-host","label":"Original","address":"127.0.0.1","port":22,"future_host":{"value":42},"proxy":{"kind":"socks5","address":"127.0.0.1:1080","future_proxy":true}}}`

func futureConfigState(t *testing.T, format int) *State {
	t.Helper()
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	store, err := configstore.Open(options.StatePath, format)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Commit(configstore.Change{
		Put: []configstore.Entry{
			{Kind: kindHost, ID: "future-host", Payload: []byte(futureHost)},
			{Kind: "future-kind", ID: "opaque", Payload: []byte(`{"d":{"unknown":true}}`)},
		},
		Local: map[string][]byte{localKey: []byte(`{"theme":"dark","future_local":{"value":42},"toolbars":{"future_toolbar":true}}`)},
	})
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	return newTestState(t, options)
}

func assertFutureHost(t *testing.T, records []configstore.Record, label, proxy string) {
	t.Helper()
	for _, record := range records {
		if record.Kind != kindHost || record.ID != "future-host" || record.Deleted {
			continue
		}
		var got struct {
			Future bool `json:"future_envelope"`
			Data   struct {
				Label  string `json:"label"`
				Future struct {
					Value int `json:"value"`
				} `json:"future_host"`
				Proxy struct {
					Address string `json:"address"`
					Future  bool   `json:"future_proxy"`
				} `json:"proxy"`
			} `json:"d"`
		}
		if err := json.Unmarshal(record.Payload, &got); err != nil {
			t.Fatal(err)
		}
		if !got.Future || got.Data.Future.Value != 42 || !got.Data.Proxy.Future ||
			got.Data.Label != label || got.Data.Proxy.Address != proxy {
			t.Fatalf("lossy record: %s", record.Payload)
		}
		return
	}
	t.Fatal("future host missing")
}

func TestCompatibleWriterPreservesFutureFieldsAcrossFullAndIncrementalSaves(t *testing.T) {
	state := futureConfigState(t, configFormat)
	if len(state.hosts) != 1 {
		t.Fatal("compatible host was not loaded")
	}
	state.hosts[0].Label = "Full save"
	if _, err := writeConfig(state.config, state.configBase, state.configDefaults, state.snapshot()); err != nil {
		t.Fatalf("full save: %v", err)
	}
	state.lastSaved, state.hasSaved = state.snapshot(), true
	state.hosts[0].Label = "Incremental save"
	state.hosts[0].Proxy.address = "127.0.0.1:1081"
	state.save()
	if state.lastSaved.Hosts[0].Label != "Incremental save" {
		t.Fatalf("incremental save: %s", state.status)
	}
	records, err := state.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	assertFutureHost(t, records, "Incremental save", "127.0.0.1:1081")
	_, local, err := state.config.Load()
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(local[localKey], &fields); err != nil {
		t.Fatal(err)
	}
	var toolbars map[string]json.RawMessage
	if err := json.Unmarshal(fields["toolbars"], &toolbars); err != nil {
		t.Fatal(err)
	}
	if string(fields["future_local"]) != `{"value":42}` || string(toolbars["future_toolbar"]) != "true" {
		t.Fatalf("local fields lost: %s", local[localKey])
	}
	reopened, report, err := loadState(state.options.StatePath)
	if err != nil || report.outcome != loadOK || len(reopened.Hosts) != 1 || reopened.Hosts[0].Label != "Incremental save" {
		t.Fatalf("reopen: %+v %+v %v", reopened.Hosts, report, err)
	}
	// Deleting a whole entity is intentional, not mistaken for a missing field.
	state.hosts = nil
	state.save()
	records, err = state.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Kind == kindHost && !record.Deleted {
			t.Fatal("explicit host deletion failed")
		}
		if record.Kind == "future-kind" && record.Deleted {
			t.Fatal("unknown record was mistaken for deleted")
		}
	}
}

func TestCompatibleBackupRestoreAndSyncPreserveRawRecords(t *testing.T) {
	source := futureConfigState(t, configFormat)
	document, err := source.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	document.Writer = "Motty 999.0.0"
	sealed, err := backup.Seal(document, testBackupPassword)
	if err != nil {
		t.Fatal(err)
	}
	document, err = backup.Open(sealed, testBackupPassword)
	if err != nil {
		t.Fatal(err)
	}
	target, _, _ := backupTestState(t)
	if _, err := target.applyBackup(document); err != nil {
		t.Fatal(err)
	}
	target.hosts[0].Label = "Restored"
	target.save()
	outgoing, err := target.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	assertFutureHost(t, outgoing.Records, "Restored", "127.0.0.1:1080")
	found := false
	for _, record := range outgoing.Records {
		found = found || record.Kind == "future-kind" && !record.Deleted
	}
	if !found {
		t.Fatal("restore dropped an opaque record")
	}
	server, dav := newFakeDAV(t)
	configureSync(t, target, server)
	syncAndWait(t, target, hitSyncNow, target.messages.Tools.SyncDone)
	assertFutureHost(t, serverDocument(t, dav).Records, "Restored", "127.0.0.1:1080")
}

func TestCompatibilityEpochMigratesLegacyAndRejectsIncompatibleData(t *testing.T) {
	for _, format := range []int{19, 20, 21, 22, 23, 24} {
		t.Run(fmt.Sprint(format), func(t *testing.T) {
			state := futureConfigState(t, format)
			if state.saveBlocked || len(state.hosts) != 1 {
				t.Fatal("legacy data was not migrated")
			}

			if _, _, err := configstore.ReadOnly(state.options.StatePath, format); !errors.Is(err, configstore.ErrTooNew) {
				t.Fatalf("legacy lossy writer can still open the migrated database: %v", err)
			}
		})
	}
	state := futureConfigState(t, configFormat)
	before, err := state.config.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	newer := backup.Document{Format: configFormat + 1}
	if _, err := state.applyBackup(newer); !errors.Is(err, backup.ErrTooNew) {
		t.Fatalf("restore accepted incompatible data: %v", err)
	}
	if _, err := state.mergeRemote(newer, false); !errors.Is(err, backup.ErrTooNew) {
		t.Fatalf("sync accepted incompatible data: %v", err)
	}
	after, err := state.config.Snapshot()
	if err != nil || string(before) != string(after) {
		t.Fatalf("incompatible data changed the database: %v", err)
	}
}

func TestCompatibleWindowsPatchOnlyTheirChangedFields(t *testing.T) {
	state := futureConfigState(t, configFormat)
	store, first, firstBase, _, err := openConfig(state.options.StatePath, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	secondStore, second, secondBase, _, err := openConfig(state.options.StatePath, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	first.Hosts[0].Label = "First window"
	second.Hosts[0].Proxy.Address = "127.0.0.1:1090"
	if _, err := writeConfig(store, firstBase, nil, first); err != nil {
		t.Fatal(err)
	}
	if _, err := writeConfig(secondStore, secondBase, nil, second); err != nil {
		t.Fatal(err)
	}
	second.Hosts[0].User = "second"
	if _, err := writeConfig(secondStore, secondBase, nil, second); err != nil {
		t.Fatal(err)
	}
	records, err := store.Records()
	if err != nil {
		t.Fatal(err)
	}
	assertFutureHost(t, records, "First window", "127.0.0.1:1090")
}

func TestDevelopmentProfileDoesNotImportProductionSyncOrCredentials(t *testing.T) {
	if !buildinfo.Development {
		t.Skip("development profile")
	}
	options := Options{}.withDefaults()
	if filepath.Base(filepath.Dir(options.KnownHostsPath)) != "motty-dev" {
		t.Fatalf("development uses production data: %s", options.KnownHostsPath)
	}
	if keyService("motty vault") == "motty vault" {
		t.Fatal("development uses production credential namespace")
	}
	source := futureConfigState(t, configFormat)
	source.sync.url, source.sync.user, source.sync.path = "https://production.invalid", "user", "backup"
	document, err := source.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	target, _, _ := backupTestState(t)
	if _, err := target.applyBackup(document); err != nil {
		t.Fatal(err)
	}
	if target.sync.configured() {
		t.Fatal("one-way import enabled production cloud sync")
	}
}
