package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"motty/internal/ai/store"
	"motty/internal/backup"
	"motty/internal/configstore"
)

const davBackupPath = "/dav/motty/backup.mottybak"

func serverDocument(t *testing.T, dav *fakeDAV) backup.Document {
	t.Helper()
	dav.mu.Lock()
	data := dav.files[davBackupPath]
	dav.mu.Unlock()
	document, err := backup.Open(data, testBackupPassword)
	if err != nil {
		t.Fatalf("the file on the server: %v", err)
	}
	return document
}

// Providers the assistant already had, and that this window has not looked at, are
// not "deleted" because a sync brought in other records, nor do the records that
// come in get this computer's stamps.
func TestASyncDoesNotWipeProvidersThisWindowHasNotLookedAt(t *testing.T) {
	server, _ := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	configureSync(t, first, server)
	first.terminalScrollback = 10000
	first.save()
	done := first.messages.Tools.SyncDone
	syncAndWait(t, first, hitSyncNow, done)

	second, secondOptions, _ := backupTestState(t)
	configureSync(t, second, server)
	// The second computer has a provider in its assistant's database already, and
	// nothing has opened that database in this run.
	path := filepath.Join(filepath.Dir(secondOptions.StatePath), "ai.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveProvider(store.Provider{ID: "plocal", Name: "Local one", Kind: store.KindOpenAI, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	syncAndWait(t, second, hitSyncNow, done)
	if second.terminalScrollback != 10000 {
		t.Fatal("the other computer's setting did not arrive")
	}
	if _, err := second.aiStore(); err != nil {
		t.Fatal(err)
	}
	if got := providerNames(second); len(got) != 1 || got[0] != "Local one" {
		t.Fatalf("the provider the assistant already had is gone: %v", got)
	}

	// What came in kept the stamps of the computer that made it.
	records, err := second.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Kind == kindSetting && record.ID == "terminal_scrollback" && !strings.HasSuffix(string(record.Stamp), "."+first.config.Device()) {
			t.Fatalf("a record from the first computer was stamped again here: %s", record.Stamp)
		}
	}
}

func TestRecordsOfKindsThisBuildDoesNotKnowAreNeitherReadNorDeleted(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	raw, err := configstore.Open(options.StatePath, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Commit(configstore.Change{Put: []configstore.Entry{
		{Kind: "tunnel", ID: "t1", Payload: []byte(`{"d":{"x":1}}`)},
		{Kind: kindSetting, ID: "a_setting_from_the_future", Payload: []byte(`{"d":5}`)},
	}}); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	state := newTestState(t, options)
	state.terminalScrollback = 10000
	state.save()
	check, err := configstore.Open(options.StatePath, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	records, _ := check.Records()
	live := 0
	for _, record := range records {
		if (record.Kind == "tunnel" || record.ID == "a_setting_from_the_future") && !record.Deleted {
			live++
		}
	}
	if live != 2 {
		t.Fatalf("what an older build does not know was deleted: %v", records)
	}
}

// A record of the wrong shape is refused, not taken for a damaged database.
func TestARecordOfTheWrongShapeDoesNotMakeTheDatabaseLookDamaged(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	raw, err := configstore.Open(options.StatePath, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Commit(configstore.Change{Put: []configstore.Entry{
		{Kind: "host", ID: "bad", Payload: []byte(`{"o":1,"d":{"id":"bad","label":"n","address":"a.example","port":"abc"}}`)},
		{Kind: "host", ID: "good", Payload: []byte(`{"o":2,"d":{"id":"good","label":"Fine","address":"b.example","port":22}}`)},
	}}); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	saved, report, err := loadState(options.StatePath)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("outcome %d, %v", report.outcome, err)
	}
	if len(saved.Hosts) != 1 || saved.Hosts[0].Label != "Fine" {
		t.Fatalf("hosts %+v", saved.Hosts)
	}
	if _, err := os.Stat(options.StatePath); err != nil {
		t.Fatal("the database was moved aside")
	}
	kept, refused := holdableRecords([]configstore.Record{
		{Kind: "host", ID: "bad", Stamp: "x", Payload: []byte(`{"o":1,"d":{"label":"n","address":"a","port":"abc"}}`)},
		{Kind: "host", ID: "good", Stamp: "x", Payload: []byte(`{"o":1,"d":{"label":"n","address":"a","port":22}}`)},
		{Kind: "tunnel", ID: "unknown kind", Stamp: "x", Payload: []byte(`{"d":1}`)},
		{Kind: "host", ID: "gone", Stamp: "x", Deleted: true},
	})
	if refused != 1 || len(kept) != 3 {
		t.Fatalf("kept %d, refused %d", len(kept), refused)
	}
}

// What the server holds that this build cannot read goes back as it was, so an
// older build never strips what a newer one put there.
func TestWhatTheServerHoldsThatThisBuildCannotReadGoesBackUntouched(t *testing.T) {
	server, dav := newFakeDAV(t)
	state, _, _ := backupTestState(t)
	configureSync(t, state, server)
	foreign := configstore.Record{Kind: "host", ID: "from-a-newer-build", Stamp: configstore.Stamp("0000000000ff.0000.deadbeef"),
		Payload: []byte(`{"o":1,"d":{"id":"from-a-newer-build","label":"x","address":"x.example","port":"not a number"}}`)}
	sealed, err := backup.Seal(backup.Document{Format: configFormat, Records: []configstore.Record{foreign}}, testBackupPassword)
	if err != nil {
		t.Fatal(err)
	}
	dav.mu.Lock()
	dav.files[davBackupPath] = sealed
	dav.mu.Unlock()

	state.terminalScrollback = 10000
	state.save()
	syncAndWait(t, state, hitSyncNow, state.messages.Tools.SyncDone)
	found := false
	for _, record := range serverDocument(t, dav).Records {
		found = found || record.ID == foreign.ID
	}
	if !found {
		t.Fatal("the record this build could not read was dropped from the server's file")
	}

	// A file from a newer format is not touched at all.
	newer, _ := backup.Seal(backup.Document{Format: configFormat + 1}, testBackupPassword)
	dav.mu.Lock()
	dav.files[davBackupPath] = newer
	puts := dav.puts
	dav.mu.Unlock()
	syncAndWait(t, state, hitSyncNow, "")
	if !strings.Contains(state.status, state.messages.Tools.BackupErrTooNew) || dav.putCount() != puts {
		t.Fatalf("status %q, %d uploads", state.status, dav.putCount()-puts)
	}
}

// A password saved before passwords were versioned is compared by when it was
// last changed here, not lost to every stamped copy.
func TestAnUnversionedPasswordIsNotLostToAnOlderStampedOne(t *testing.T) {
	server, _ := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	second, _, _ := backupTestState(t)
	configureSync(t, first, server)
	configureSync(t, second, server)
	done := first.messages.Tools.SyncDone

	if err := first.secrets.vault.Set("password:shared@host:22", "old"); err != nil {
		t.Fatal(err)
	}
	syncAndWait(t, first, hitSyncNow, done)
	time.Sleep(5 * time.Millisecond)

	second.secrets.vault.SetStamper(nil) // as a vault from before stamps
	if err := second.secrets.vault.Set("password:shared@host:22", "new"); err != nil {
		t.Fatal(err)
	}
	second.secrets.vault.SetStamper(func() (string, error) {
		stamp, err := second.config.NextStamp()
		return string(stamp), err
	})
	syncAndWait(t, second, hitSyncNow, done)
	syncAndWait(t, first, hitSyncNow, done)
	for name, state := range map[string]*State{"first": first, "second": second} {
		if value, _, _ := state.secrets.vault.Get("password:shared@host:22"); value != "new" {
			t.Errorf("%s holds %q; the later change should win", name, value)
		}
	}
}

func TestAKeyChangedOnAnotherComputerReachesTheProviderInMemory(t *testing.T) {
	server, _ := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	second, _, _ := backupTestState(t)
	configureSync(t, first, server)
	configureSync(t, second, server)
	done := first.messages.Tools.SyncDone
	addProviderTo(t, first, "p1")
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)
	if key := second.aiKeyOf(second.ai.providers[0]); key != "sk-p1" {
		t.Fatalf("key %q", key)
	}

	time.Sleep(5 * time.Millisecond)
	if err := first.secrets.vault.Set(aiKeySecretName("p1"), "rotated"); err != nil {
		t.Fatal(err)
	}
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)
	if key := second.aiKeyOf(second.ai.providers[0]); key != "rotated" {
		t.Fatalf("the window still sends the old key: %q", key)
	}
}

// The vault is shared by the windows and was given to the last one made; when that
// one is closed a password changed afterwards still starts a sync.
func TestAPasswordChangedAfterTheWindowThatWatchesTheVaultClosedStillSyncs(t *testing.T) {
	server, _ := newFakeDAV(t)
	group := NewGroup()
	base, options, _ := backupTestState(t)
	configureSync(t, base, server)
	base.Close()
	options.Group = group
	leader := newTestState(t, options)
	last := newTestState(t, options)
	fastSync(leader)
	fastSync(last)
	waitFor(t, leader, func() bool { return idle(leader) })
	leader.syncDelay = time.Hour // so that a sync that is asked for stays visible as a pending timer
	last.Close()

	if err := leader.secrets.vault.Set("password:later@host:22", "x"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, leader, func() bool { return leader.sync.timer != nil })
}

func TestALongNameDoesNotStopEverythingFromBeingSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	long := strings.Repeat("g", 300)
	saved := richState()
	saved.HostGroups = append(saved.HostGroups, savedHostGroup{Name: long})
	saved.Snippets = append(saved.Snippets, savedSnippet{Name: strings.Repeat("s", 400), Command: "ls"})
	if err := saveState(path, saved); err != nil {
		t.Fatalf("a long name made saving fail: %v", err)
	}
	got, _, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, group := range got.HostGroups {
		names[group.Name] = true
	}
	if !names[long] || len(got.Snippets) != len(saved.Snippets) {
		t.Fatalf("groups %v, %d snippets", names, len(got.Snippets))
	}
}

func TestAChangeWhileAFailedSyncWaitsIsTriedAtOnce(t *testing.T) {
	server, dav := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	setUpAutoSync(t, first, server, dav)
	first.syncRetryBase = time.Hour
	dav.mu.Lock()
	dav.fail = 1000
	dav.mu.Unlock()
	first.terminalScrollback = 10000
	first.save()
	waitFor(t, first, func() bool { return first.sync.err != "" && first.sync.timer != nil })

	dav.mu.Lock()
	dav.fail = 0
	dav.mu.Unlock()
	base := dav.putCount()
	first.terminalScrollback = 1000
	first.save()
	waitFor(t, first, func() bool { return dav.putCount() > base && idle(first) })
}

func TestOnlyTheKnownHostsLinesThatAreMissingAreAdded(t *testing.T) {
	merged := unionLines("a ssh-ed25519 AAA\nb ssh-ed25519 BBB\n", "b ssh-ed25519 BBB\nc ssh-ed25519 CCC")
	if merged != "a ssh-ed25519 AAA\nb ssh-ed25519 BBB\nc ssh-ed25519 CCC\n" {
		t.Fatalf("union %q", merged)
	}
	if hasNewLines("a x\n", "a x\nb y\n") || !hasNewLines("a x\nc z\n", "a x\nb y\n") {
		t.Fatal("hasNewLines")
	}
}
