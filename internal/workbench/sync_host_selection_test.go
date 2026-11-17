package workbench

import (
	"fmt"
	"slices"
	"testing"

	"motty/internal/backup"
	"motty/internal/configstore"
)

func setSyncSelectionTestHosts(t *testing.T, state *State, hosts ...Host) {
	t.Helper()
	state.hosts = hosts
	for index := range state.hosts {
		state.hosts[index].ID = uint32(index + 1)
		state.hosts[index].finish()
	}
	state.hostDataChanged()
	if err := state.saveWithResult(); err != nil {
		t.Fatal(err)
	}
}

func syncSelectionTestHost(id, group string) Host {
	return Host{CatalogID: id, Label: id, Address: id + ".example.com", User: "me", Port: 22, Group: group}
}

func syncSelectionDocumentRecord(document backup.Document, kind, id string) (configstore.Record, bool) {
	for _, record := range document.Records {
		if record.Kind == kind && record.ID == id {
			return record, true
		}
	}
	return configstore.Record{}, false
}

func TestSelectedHostSyncExchangesOnlyChosenHostsAndSecrets(t *testing.T) {
	server, dav := newFakeDAV(t)
	a, _, _ := backupTestState(t)
	b, _, _ := backupTestState(t)
	configureSync(t, a, server)
	configureSync(t, b, server)
	var hosts []Host
	for index := 0; index < 10; index++ {
		hosts = append(hosts, syncSelectionTestHost(fmt.Sprintf("host-%d", index), ""))
	}
	setSyncSelectionTestHosts(t, a, hosts...)
	setSyncSelectionTestHosts(t, b)
	a.setSyncHostSelection(syncHostSelection{OnlySelected: true, IDs: []string{"host-0", "host-1", "host-2"}})
	for _, host := range hosts {
		for _, name := range []string{hostSecretName(host.CatalogID, hostPasswordPart), hostSecretName(host.CatalogID, hostOTPPart), "host-proxy:" + host.CatalogID + ":proxy:password"} {
			if err := a.secrets.vault.Set(name, "secret-"+host.CatalogID); err != nil {
				t.Fatal(err)
			}
		}
	}
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	syncAndWait(t, b, hitSyncNow, b.messages.Tools.SyncDone)
	if len(b.hosts) != 3 || b.syncScope.hosts.OnlySelected {
		t.Fatalf("receiver hosts=%d scope=%+v", len(b.hosts), b.syncScope.hosts)
	}
	// An unrelated edit forces another upload: closed hosts must stay closed.
	addTestNote(t, a, "force-upload", "note")
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	document := serverDocument(t, dav)
	for index, host := range hosts {
		_, found := syncSelectionDocumentRecord(document, "host", host.CatalogID)
		if found != (index < 3) {
			t.Fatalf("host %s on server=%v", host.CatalogID, found)
		}
		for _, name := range []string{hostSecretName(host.CatalogID, hostPasswordPart), hostSecretName(host.CatalogID, hostOTPPart), "host-proxy:" + host.CatalogID + ":proxy:password"} {
			if documentSecret(document, name) != (index < 3) {
				t.Fatalf("wrong secret selection: %s", name)
			}
		}
	}
	full, err := a.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := syncSelectionDocumentRecord(full, "host", "host-9"); !found || !documentSecret(full, hostSecretName("host-9", hostPasswordPart)) {
		t.Fatal("manual backup lost a local-only host or password")
	}
}

func TestSelectedHostSyncProtectsLocalCopiesAndPassesRemoteCopiesOn(t *testing.T) {
	server, dav := newFakeDAV(t)
	a, _, _ := backupTestState(t)
	b, _, _ := backupTestState(t)
	configureSync(t, a, server)
	configureSync(t, b, server)
	local := syncSelectionTestHost("private", "Local")
	remote := local
	remote.Address = "remote.example.com"
	setSyncSelectionTestHosts(t, a, syncSelectionTestHost("shared", "Shared"), local)
	setSyncSelectionTestHosts(t, b, remote)
	a.setSyncHostSelection(syncHostSelection{OnlySelected: true, IDs: []string{"shared"}})
	name := hostSecretName("private", hostPasswordPart)
	for state, value := range map[*State]string{a: "local-secret", b: "remote-secret"} {
		if err := state.secrets.vault.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	syncAndWait(t, b, hitSyncNow, b.messages.Tools.SyncDone)
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	if !slices.Contains(hostAddresses(a.hosts), local.Address) || slices.Contains(hostAddresses(a.hosts), remote.Address) {
		t.Fatal("an excluded remote host replaced the local one")
	}
	if value, _, err := a.secrets.vault.Get(name); err != nil || value != "local-secret" {
		t.Fatalf("local password=%q, %v", value, err)
	}
	document := serverDocument(t, dav)
	if _, found := syncSelectionDocumentRecord(document, "host", "private"); !found {
		t.Fatal("upload removed another device's host")
	}
	for _, secret := range document.Secrets {
		if secret.Name == name && secret.Value != "remote-secret" {
			t.Fatal("excluded local password overwrote the remote copy")
		}
	}
	setSyncSelectionTestHosts(t, b)
	if err := b.secrets.vault.Delete(name); err != nil {
		t.Fatal(err)
	}
	syncAndWait(t, b, hitSyncNow, b.messages.Tools.SyncDone)
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	if !slices.Contains(hostAddresses(a.hosts), local.Address) {
		t.Fatal("an excluded remote tombstone deleted the local host")
	}
	if value, _, err := a.secrets.vault.Get(name); err != nil || value != "local-secret" {
		t.Fatalf("remote deletion changed the local password=%q, %v", value, err)
	}
}

func TestSelectedHostSyncSendsTombstonesAndResumesSelection(t *testing.T) {
	server, dav := newFakeDAV(t)
	a, _, _ := backupTestState(t)
	b, _, _ := backupTestState(t)
	configureSync(t, a, server)
	configureSync(t, b, server)
	shared := syncSelectionTestHost("shared", "Team/Sub")
	private := syncSelectionTestHost("private", "Team/Sub")
	setSyncSelectionTestHosts(t, a, shared, private)
	setSyncSelectionTestHosts(t, b)
	a.setSyncHostSelection(syncHostSelection{OnlySelected: true, IDs: []string{shared.CatalogID}})
	name := hostSecretName(shared.CatalogID, hostPasswordPart)
	if err := a.secrets.vault.Set(name, "shared-secret"); err != nil {
		t.Fatal(err)
	}
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	syncAndWait(t, b, hitSyncNow, b.messages.Tools.SyncDone)
	// Turning one host off is not a remote deletion, even on a forced upload.
	a.setSyncHostSelection(syncHostSelection{OnlySelected: true})
	addTestNote(t, a, "force-upload", "note")
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	if record, found := syncSelectionDocumentRecord(serverDocument(t, dav), "host", shared.CatalogID); !found || record.Deleted {
		t.Fatal("deselecting removed the remote copy")
	}
	a.setSyncHostSelection(syncHostSelection{OnlySelected: true, IDs: []string{shared.CatalogID}})
	setSyncSelectionTestHosts(t, a, private)
	if err := a.secrets.vault.Delete(name); err != nil {
		t.Fatal(err)
	}
	syncAndWait(t, a, hitSyncNow, a.messages.Tools.SyncDone)
	syncAndWait(t, b, hitSyncNow, b.messages.Tools.SyncDone)
	if len(b.hosts) != 0 || len(a.hosts) != 1 {
		t.Fatalf("tombstone membership: a=%v b=%v", hostAddresses(a.hosts), hostAddresses(b.hosts))
	}
	if record, found := syncSelectionDocumentRecord(serverDocument(t, dav), "host", shared.CatalogID); !found || !record.Deleted {
		t.Fatal("selected tombstone did not reach the server")
	}
	if _, found, err := b.secrets.vault.Get(name); err != nil || found {
		t.Fatalf("selected password deletion did not arrive: found=%v error=%v", found, err)
	}
}

func TestSelectedHostRestorePreservesUnselectedHostsAndPasswords(t *testing.T) {
	a, _, _ := backupTestState(t)
	b, _, _ := backupTestState(t)
	private := syncSelectionTestHost("private", "Private")
	setSyncSelectionTestHosts(t, a, private, syncSelectionTestHost("shared", "Team"), syncSelectionTestHost("missing", "Team"))
	remotePrivate := private
	remotePrivate.Address = "remote.example.com"
	remoteShared := syncSelectionTestHost("shared", "Team/New")
	remoteShared.Label = "Restored"
	setSyncSelectionTestHosts(t, b, remotePrivate, remoteShared, syncSelectionTestHost("unselected-new", "Other"))
	name := hostSecretName("private", hostPasswordPart)
	for state, value := range map[*State]string{a: "local-secret", b: "remote-secret"} {
		if err := state.secrets.vault.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	a.setSyncHostSelection(syncHostSelection{OnlySelected: true, IDs: []string{"shared", "missing"}})
	document, err := b.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.applyBackupScope(document, a.syncScope); err != nil {
		t.Fatal(err)
	}
	if len(a.hosts) != 2 || !slices.Contains(hostAddresses(a.hosts), private.Address) || !slices.Contains(hostAddresses(a.hosts), remoteShared.Address) {
		t.Fatalf("restore scope: %v", hostAddresses(a.hosts))
	}
	if value, _, err := a.secrets.vault.Get(name); err != nil || value != "local-secret" {
		t.Fatalf("restore changed excluded secret: %q, %v", value, err)
	}
}

func TestSelectedHostScopePersistsLocallyAndFollowsStableIdentities(t *testing.T) {
	state, options, _ := backupTestState(t)
	setSyncSelectionTestHosts(t, state, syncSelectionTestHost("a", "Team"), syncSelectionTestHost("b", "Team/Sub"), syncSelectionTestHost("c", "Teamwork"))
	state.Click(hitSyncSelectedHosts)
	state.toggleSyncHostGroup("Team")
	if !state.syncScope.hosts.hasHost("a") || !state.syncScope.hosts.hasHost("b") || state.syncScope.hosts.hasHost("c") {
		t.Fatalf("group selection: %+v", state.syncScope.hosts)
	}
	state.hosts = append(state.hosts, syncSelectionTestHost("new", "Team"))
	state.hostDataChanged()
	state.save()
	if state.syncScope.hosts.hasHost("new") || state.syncGroupSelected("Team") {
		t.Fatal("group selection silently included a new host")
	}
	if !state.renameHostGroup("Team", "Renamed") {
		t.Fatal(state.dialogError)
	}
	if !state.syncScope.hosts.hasHost("a") || !slices.Contains(state.syncScope.hosts.Groups, "Renamed/Sub") {
		t.Fatal("rename lost the stable selection")
	}
	state.toggleSyncCategory(syncCategoryHosts)
	state.toggleSyncCategory(syncCategoryHosts)
	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.SyncHosts.OnlySelected || !saved.SyncHosts.hasHost("a") || saved.SyncHosts.hasHost("new") {
		t.Fatalf("saved selection: %+v", saved.SyncHosts)
	}
	restarted := newTestState(t, options)
	if !restarted.syncScope.hosts.OnlySelected || !restarted.syncScope.hosts.hasHost("a") || restarted.syncScope.hosts.hasHost("new") {
		t.Fatalf("selection after restart: %+v", restarted.syncScope.hosts)
	}
	records, err := state.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	scope := state.syncScope.withHostGroups(records)
	if !scope.allowsRecordKey("group", "Renamed") || scope.allowsRecordKey("group", "Teamwork") {
		t.Fatal("wrong group metadata selected")
	}
	if !scope.allowsRecordKey("host", "a") || scope.allowsRecordKey("host", "new") {
		t.Fatal("wrong stable membership after rename")
	}
}

func TestSelectedHostScopeSaveFailureAndMultipleWindows(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	state := pair.first
	state.activeSection, state.settingsCategory = sectionSettings, settingsBackup
	selection := syncHostSelection{OnlySelected: true, IDs: []string{state.hosts[0].CatalogID}}
	state.saveBlocked = true
	state.setSyncHostSelection(selection)
	if state.syncScope.hosts.OnlySelected {
		t.Fatal("failed save changed the active scope")
	}
	state.saveBlocked = false
	state.Click(hitStateNoticeOK)
	state.setSyncHostSelection(selection)
	if !pair.second.syncScope.hosts.OnlySelected || !pair.second.syncScope.hosts.hasHost(selection.IDs[0]) {
		t.Fatal("another window did not receive the local selection")
	}
	// A captured round must not observe a later click mutating its slices.
	snapshot := state.syncScope
	state.Click(hitSyncHostItems.At(0))
	if !snapshot.hosts.hasHost(selection.IDs[0]) || state.syncScope.hosts.hasHost(selection.IDs[0]) {
		t.Fatalf("selection change: snapshot=%+v current=%+v", snapshot.hosts, state.syncScope.hosts)
	}
}

func TestSelectedHostScopeEmptySelectionAndUnknownContent(t *testing.T) {
	scope := newSyncScope(syncCategories[1:])
	scope.hosts.OnlySelected = true
	if !scope.empty() {
		t.Fatal("no selected hosts should make a hosts-only scope empty")
	}
	if !scope.allowsRecordKey("future-kind", "future-id") || !scope.allowsSecret("future-secret:one") {
		t.Fatal("unknown content compatibility changed")
	}
	for _, name := range []string{"host:private:password", "host:private:otp", "host-proxy:private:proxy:password", "host::password", "host:malformed"} {
		if scope.allowsSecret(name) {
			t.Fatalf("unselected secret passed: %s", name)
		}
	}
	scope.hosts.IDs = []string{"deleted-host"}
	if scope.empty() || !scope.allowsRecordKey("host", "deleted-host") {
		t.Fatal("a selected tombstone must keep sync active")
	}
}

func TestSelectedHostGroupsCarryAncestorsAndRenameTombstones(t *testing.T) {
	state, _, _ := backupTestState(t)
	setSyncSelectionTestHosts(t, state, syncSelectionTestHost("shared", "Team/Sub"), syncSelectionTestHost("private", "Private/Sub"))
	state.setSyncHostSelection(syncHostSelection{OnlySelected: true, IDs: []string{"shared"}})
	first, err := state.mergeRemote(backup.Document{}, true)
	if err != nil || first == nil {
		t.Fatalf("first upload: %v", err)
	}
	for _, name := range []string{"Team", "Team/Sub"} {
		if record, found := syncSelectionDocumentRecord(*first, "group", name); !found || record.Deleted {
			t.Fatalf("missing selected ancestor %s", name)
		}
	}
	for _, name := range []string{"Private", "Private/Sub"} {
		if _, found := syncSelectionDocumentRecord(*first, "group", name); found {
			t.Fatalf("private group reached the server: %s", name)
		}
	}
	if !state.renameHostGroup("Team", "Renamed") {
		t.Fatal(state.dialogError)
	}
	next, err := state.mergeRemote(*first, false)
	if err != nil || next == nil {
		t.Fatalf("renamed upload: %v", err)
	}
	for _, name := range []string{"Team", "Team/Sub"} {
		if record, found := syncSelectionDocumentRecord(*next, "group", name); !found || !record.Deleted {
			t.Fatalf("missing old group tombstone %s", name)
		}
	}
	for _, name := range []string{"Renamed", "Renamed/Sub"} {
		if record, found := syncSelectionDocumentRecord(*next, "group", name); !found || record.Deleted {
			t.Fatalf("missing renamed group %s", name)
		}
	}
}
