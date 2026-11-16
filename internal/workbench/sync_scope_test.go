package workbench

import (
	"slices"
	"testing"

	"motty/internal/backup"
)

func TestEverySharedRecordKindHasASyncCategory(t *testing.T) {
	for _, entity := range entityFields {
		if _, known := recordSyncCategory(entity.kind); !known {
			t.Errorf("record kind %q has no sync category", entity.kind)
		}
	}
	if category, known := recordSyncCategory(kindSetting); !known || category != syncCategorySettings {
		t.Errorf("a setting maps to %q, want %q", category, syncCategorySettings)
	}
}

func TestEveryVaultSecretFamilyHasASyncCategory(t *testing.T) {
	cases := []struct {
		name string
		want syncCategory
	}{
		{"host:abc:password", syncCategoryHosts},
		{"host:abc:vnc-system-password:alice", syncCategoryHosts},
		{"host-proxy:abc:deadbeef:password", syncCategoryHosts},
		{proxyPasswordSecret, syncCategorySettings},
		{"credential:abc", syncCategoryCredentials},
		{"sshkey:abc", syncCategoryKeys},
		{"passphrase:abc", syncCategoryKeys},
		{"aikey:abc", syncCategoryAI},
		{"autofill:abc", syncCategoryRules},
	}
	for _, entry := range cases {
		got, known := secretSyncCategory(entry.name)
		if !known || got != entry.want {
			t.Errorf("%q maps to %q, want %q", entry.name, got, entry.want)
		}
	}
	for _, name := range []string{webdavPasswordSecret, s3SecretName, gistTokenSecret, giteeTokenSecret, backupPasswordSecret} {
		if !excludedFromBackup(name) {
			t.Errorf("the sync login %q is not excluded from every backup", name)
		}
	}
}

func TestUnknownContentIsAlwaysExchanged(t *testing.T) {
	all := append([]syncCategory(nil), syncCategories[:]...)
	scope := newSyncScope(all)
	if !scope.empty() {
		t.Fatal("closing every known category is not empty")
	}
	// A newer build's record or password must pass through rather than be
	// silently dropped by a build that does not know it yet.
	if !scope.allowsRecord("a-kind-from-the-future") {
		t.Fatal("an unknown record kind was filtered")
	}
	if !scope.allowsSecret("a-secret-from-the-future:1") {
		t.Fatal("an unknown saved password was filtered")
	}
}

func TestTheDefaultSelectionExchangesEverything(t *testing.T) {
	for _, scope := range []syncScope{{}, newSyncScope(nil)} {
		if scope.empty() {
			t.Fatal("the default scope exchanges nothing")
		}
		for _, category := range syncCategories {
			if !scope.allows(category) {
				t.Fatalf("%q is closed by default", category)
			}
		}
	}
}

func TestASelectionRoundTripsAndKeepsUnknownCategories(t *testing.T) {
	scope := newSyncScope([]syncCategory{syncCategoryNotes, "a-category-from-the-future", syncCategoryNotes, ""})
	if scope.allows(syncCategoryNotes) {
		t.Fatal("the closed note category is open")
	}
	if !scope.allows(syncCategoryHosts) {
		t.Fatal("closing notes closed hosts too")
	}
	want := []syncCategory{"a-category-from-the-future", syncCategoryNotes}
	if got := scope.excludedCopy(); !slices.Equal(got, want) {
		t.Fatalf("saved exclusions = %q, want %q", got, want)
	}
	reloaded := newSyncScope(scope.excludedCopy())
	if reloaded.allows(syncCategoryNotes) || !reloaded.allows(syncCategoryHosts) {
		t.Fatal("the saved exclusions do not mean the same thing after a reload")
	}
}

func TestTheSelectionIsSavedOnThisComputer(t *testing.T) {
	state, options, _ := backupTestState(t)
	state.toggleSyncCategory(syncCategoryNotes)
	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(saved.SyncExcluded, []syncCategory{syncCategoryNotes}) {
		t.Fatalf("saved exclusions = %q", saved.SyncExcluded)
	}
	if newSyncScope(saved.SyncExcluded).allows(syncCategoryNotes) {
		t.Fatal("the closed category is open after a reload")
	}
}

func TestTheSettingsPageTogglesOneCategoryAtATime(t *testing.T) {
	state, _, _ := backupTestState(t)
	for index, category := range syncCategories {
		before := state.syncScope.allows(category)
		state.Click(hitSyncCategories.At(index))
		if got := state.syncScope.allows(category); got == before {
			t.Fatalf("clicking row %d did not change %q", index, category)
		}
	}
}

func TestClosingACategoryReachesTheOtherWindows(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	if !pair.second.syncScope.allows(syncCategoryNotes) {
		t.Fatal("the second window starts with notes closed")
	}
	pair.first.toggleSyncCategory(syncCategoryNotes)
	if pair.second.syncScope.allows(syncCategoryNotes) {
		t.Fatal("the second window still shows notes as synced")
	}
}

// documentNote reports whether the server's file holds a note by id.
func documentNote(document backup.Document, id string) bool {
	for _, record := range document.Records {
		if note, ok := decodeNoteRecord(record); ok && note.ID == id {
			return true
		}
	}
	return false
}

func documentSecret(document backup.Document, name string) bool {
	for _, secret := range document.Secrets {
		if secret.Name == name && !secret.Deleted {
			return true
		}
	}
	return false
}

func addTestNote(t *testing.T, state *State, id, body string) {
	t.Helper()
	state.notes.list = append(state.notes.list, savedNote{ID: id, Title: id, Body: body, Revision: noteHash(body)})
	state.save()
}

func noteBodies(state *State) map[string]string {
	bodies := make(map[string]string, len(state.notes.list))
	for _, note := range state.notes.list {
		bodies[note.ID] = note.Body
	}
	return bodies
}

func hostAddresses(hosts []Host) []string {
	addresses := make([]string, len(hosts))
	for index, host := range hosts {
		addresses[index] = host.Address
	}
	return addresses
}

func TestAClosedCategoryIsNeitherUploadedNorTakenIn(t *testing.T) {
	server, dav := newFakeDAV(t)
	a, _, _ := backupTestState(t)
	configureSync(t, a, server)
	b, _, _ := backupTestState(t)
	configureSync(t, b, server)

	addTestNote(t, b, "b-note", "shared")
	syncAndWait(t, b, hitSyncNow, b.messages.Tools.SyncDone)

	addTestNote(t, a, "a-note", "private")
	a.toggleSyncCategory(syncCategoryNotes)
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)

	if bodies := noteBodies(a); len(bodies) != 1 || bodies["a-note"] != "private" {
		t.Fatalf("the closed category changed this computer's notes: %v", bodies)
	}
	document := serverDocument(t, dav)
	if documentNote(document, "a-note") {
		t.Fatal("a closed category's note reached the server")
	}
	if !documentNote(document, "b-note") {
		t.Fatal("another computer's note is missing from the server")
	}

	// A round that uploads because of an open category must still not send the
	// closed one, and must put the server's copy of it back unchanged.
	a.terminalScrollback = 12345
	a.save()
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	document = serverDocument(t, dav)
	if documentNote(document, "a-note") {
		t.Fatal("a closed category's note reached the server on a later round")
	}
	if !documentNote(document, "b-note") {
		t.Fatal("an upload from this computer deleted another computer's note")
	}
	if bodies := noteBodies(a); len(bodies) != 1 || bodies["a-note"] != "private" {
		t.Fatalf("the closed category took in the server's note: %v", bodies)
	}
}

func TestReopeningACategoryMergesBothSides(t *testing.T) {
	server, _ := newFakeDAV(t)
	a, _, _ := backupTestState(t)
	configureSync(t, a, server)
	b, _, _ := backupTestState(t)
	configureSync(t, b, server)

	addTestNote(t, a, "a-note", "private")
	a.toggleSyncCategory(syncCategoryNotes)
	addTestNote(t, b, "b-note", "shared")
	syncAndWait(t, b, hitSyncNow, b.messages.Tools.SyncDone)
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	if bodies := noteBodies(a); len(bodies) != 1 || bodies["a-note"] != "private" {
		t.Fatalf("the closed category took in notes: %v", bodies)
	}

	a.toggleSyncCategory(syncCategoryNotes)
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	bodies := noteBodies(a)
	if bodies["a-note"] != "private" || bodies["b-note"] != "shared" {
		t.Fatalf("reopening the category did not merge both sides: %v", bodies)
	}

	syncAndWait(t, b, hitSyncNow, b.messages.Tools.SyncDone)
	if bodies := noteBodies(b); bodies["a-note"] != "private" {
		t.Fatalf("this computer's note never reached the other one: %v", bodies)
	}
}

func TestAClosedSecretCategoryStaysOffTheServer(t *testing.T) {
	server, dav := newFakeDAV(t)
	a, _, _ := backupTestState(t)
	configureSync(t, a, server)
	b, _, _ := backupTestState(t)
	configureSync(t, b, server)

	if err := b.secrets.vault.Set("passphrase:b-key", "b-secret"); err != nil {
		t.Fatal(err)
	}
	syncAndWait(t, b, hitSyncNow, b.messages.Tools.SyncDone)
	if err := a.secrets.vault.Set("passphrase:a-key", "a-secret"); err != nil {
		t.Fatal(err)
	}
	a.toggleSyncCategory(syncCategoryKeys)
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)

	document := serverDocument(t, dav)
	if documentSecret(document, "passphrase:a-key") {
		t.Fatal("a closed category's password reached the server")
	}
	if !documentSecret(document, "passphrase:b-key") {
		t.Fatal("another computer's password is missing from the server")
	}
	if _, found, _ := a.secrets.vault.Get("passphrase:b-key"); found {
		t.Fatal("the closed category took in the server's password")
	}
	if _, found, _ := a.secrets.vault.Get("passphrase:a-key"); !found {
		t.Fatal("the closed category deleted this computer's password")
	}

	// An upload from an open category must put the server's password back.
	a.terminalScrollback = 12345
	a.save()
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	document = serverDocument(t, dav)
	if documentSecret(document, "passphrase:a-key") {
		t.Fatal("a later upload carried the closed category's password")
	}
	if !documentSecret(document, "passphrase:b-key") {
		t.Fatal("a later upload deleted the server's password")
	}

	a.toggleSyncCategory(syncCategoryKeys)
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	if _, found, _ := a.secrets.vault.Get("passphrase:b-key"); !found {
		t.Fatal("reopening the category did not take in the server's password")
	}
	if !documentSecret(serverDocument(t, dav), "passphrase:a-key") {
		t.Fatal("reopening the category did not upload this computer's password")
	}
}

func TestNothingSelectedStopsBeforeTheServer(t *testing.T) {
	server, dav := newFakeDAV(t)
	state, _, _ := backupTestState(t)
	configureSync(t, state, server)
	for _, category := range syncCategories {
		state.toggleSyncCategory(category)
	}
	if !state.syncScope.empty() {
		t.Fatal("closing every category left the selection non-empty")
	}
	state.status = ""
	state.Click(hitSyncNow)
	if state.status != state.messages.Tools.SyncErrNothingSelected {
		t.Fatalf("status = %q, want %q", state.status, state.messages.Tools.SyncErrNothingSelected)
	}
	if dav.puts != 0 || len(dav.files) != 0 {
		t.Fatalf("a sync with nothing selected reached the server: %d puts, %d files", dav.puts, len(dav.files))
	}
}

func TestTheSelectionIsPerComputerNotShared(t *testing.T) {
	server, _ := newFakeDAV(t)
	a, _, _ := backupTestState(t)
	configureSync(t, a, server)
	b, bOptions, _ := backupTestState(t)
	configureSync(t, b, server)

	a.toggleSyncCategory(syncCategoryNotes)
	a.terminalScrollback = 10000
	a.save()
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)

	saved, _, err := loadState(bOptions.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.SyncExcluded) != 0 {
		t.Fatalf("this computer's selection reached the other one: %q", saved.SyncExcluded)
	}

	// The other computer still exchanges notes, and this one still ignores them.
	addTestNote(t, b, "b-note", "shared")
	syncAndWait(t, b, hitSyncNow, b.messages.Tools.SyncDone)
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	if _, ok := noteBodies(a)["b-note"]; ok {
		t.Fatal("a closed category took in a note the other computer still syncs")
	}
}

func TestAScopedRestoreLeavesClosedCategoriesAlone(t *testing.T) {
	a, _, _ := backupTestState(t)
	b, _, _ := backupTestState(t)
	addTestNote(t, a, "a-note", "local")
	addTestNote(t, b, "b-note", "server")
	b.hosts = append(b.hosts, Host{Label: "Only B", Address: "only-b.example.com", User: "u", Port: 22})
	b.save()

	document, err := b.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	scope := newSyncScope([]syncCategory{syncCategoryNotes})
	if _, err := a.applyBackupScope(document, scope); err != nil {
		t.Fatal(err)
	}

	bodies := noteBodies(a)
	if bodies["a-note"] != "local" {
		t.Fatalf("the restore replaced a closed category's note: %v", bodies)
	}
	if _, ok := bodies["b-note"]; ok {
		t.Fatalf("the restore took in a closed category's note: %v", bodies)
	}
	if !slices.Contains(hostAddresses(a.hosts), "only-b.example.com") {
		t.Fatal("the restore did not apply an open category")
	}
}
