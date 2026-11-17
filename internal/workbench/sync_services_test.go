package workbench

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func saveServiceForTest(t *testing.T, state *State, id syncServiceID, password string) {
	t.Helper()
	switch id {
	case syncServiceWebDAV:
		state.Click(hitSyncConfigure)
		fillForm(state, "https://dav.example.com", "user", "webdav-secret", "backup.mottybak", password)
	case syncServiceS3:
		state.Click(hitSyncConfigureS3)
		fillForm(state, "https://s3.example.com", "us-east-1", "bucket", "backup.mottybak", "yes")
		state.Click(hitFormSubmit)
		fillForm(state, "access-key", "s3-secret", password)
	case syncServiceGitHub:
		state.Click(hitSyncConfigureGist)
		fillForm(state, "github-token", "", "", "", password)
	case syncServiceGitee:
		state.Click(hitSyncConfigureGitee)
		fillForm(state, "gitee-token", "", "", "", password)
	}
	state.Click(hitFormSubmit)
	if state.modal != modalNone {
		t.Fatalf("save %s: %s", id, state.form.dialogError)
	}
}

func TestSyncServicesSaveFourProfilesWithoutEnabling(t *testing.T) {
	state, options, _ := backupTestState(t)
	for _, id := range syncServices {
		saveServiceForTest(t, state, id, "password-for-"+string(id))
		if state.sync.configured() || state.sync.working || state.sync.timer != nil {
			t.Fatalf("saving %s enabled or started sync", id)
		}
	}
	saved, _, err := loadState(options.StatePath)
	if err != nil || saved.SyncServices == nil || len(saved.SyncServices.Profiles) != 4 || saved.SyncServices.Active != "" {
		t.Fatalf("four profiles not persisted: %v", err)
	}
	restarted := newTestState(t, options)
	if restarted.sync.configured() || len(restarted.syncProfiles) != 4 {
		t.Fatal("restart lost independent disabled profiles")
	}
	for _, id := range syncServices {
		profile := restarted.syncProfile(id)
		_, password, err := restarted.syncDraftSecrets(profile, "", "")
		if err != nil || password != "password-for-"+string(id) {
			t.Fatalf("password for %s: %v", id, err)
		}
	}
	document, err := state.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range document.Secrets {
		if strings.HasPrefix(secret.Name, "sync-password:") {
			t.Fatal("sync password escaped into backup")
		}
	}
}

func TestSyncServicesEnableIsExclusiveAndDisablePreservesAccounts(t *testing.T) {
	state, options, _ := backupTestState(t)
	for _, id := range syncServices {
		saveServiceForTest(t, state, id, "password-for-"+string(id))
	}
	state.sync.manual = true
	for _, id := range syncServices {
		state.toggleSyncService(id)
		if state.activeSyncService() != id || !state.sync.manual {
			t.Fatalf("enable %s failed", id)
		}
	}
	state.toggleSyncService(syncServiceGitee)
	if state.sync.configured() || len(state.syncProfiles) != 4 {
		t.Fatal("disable removed accounts or kept a service active")
	}
	restarted := newTestState(t, options)
	if restarted.sync.configured() || !restarted.sync.manual {
		t.Fatal("disabled/manual state did not survive restart")
	}
	for _, id := range syncServices {
		if _, _, err := restarted.syncDraftSecrets(restarted.syncProfile(id), "", ""); err != nil {
			t.Fatalf("disable lost %s credentials: %v", id, err)
		}
	}
}

func TestEditingInactiveSyncServiceKeepsActiveServiceAndSecrets(t *testing.T) {
	state, _, _ := backupTestState(t)
	saveServiceForTest(t, state, syncServiceGitHub, "github-password")
	state.toggleSyncService(syncServiceGitHub)
	epoch := state.syncEpoch
	saveServiceForTest(t, state, syncServiceS3, "s3-password")
	if state.activeSyncService() != syncServiceGitHub || state.syncEpoch != epoch {
		t.Fatal("editing S3 changed enabled GitHub service")
	}
	state.Click(hitSyncConfigureGist)
	if state.form.value(0) != "" || state.form.value(4) != "" {
		t.Fatal("edit exposed saved secrets")
	}
	state.Click(hitFormSubmit) // blank secrets retain only this account's values
	if state.modal != modalNone {
		t.Fatal(state.form.dialogError)
	}
	_, password, err := state.syncDraftSecrets(state.sync, "", "")
	if err != nil || password != "github-password" {
		t.Fatal("blank GitHub password inherited S3 password")
	}
}

func TestSyncProfileSaveFailureRollsBackCredentialsAndConfiguration(t *testing.T) {
	state, _, _ := backupTestState(t)
	saveServiceForTest(t, state, syncServiceWebDAV, "webdav-password")
	state.toggleSyncService(syncServiceWebDAV)
	state.Click(hitSyncConfigureGist)
	fillForm(state, "new-token", "", "", "", "new-backup-password")
	state.saveBlocked = true
	state.Click(hitFormSubmit)
	state.saveBlocked = false
	if state.modal != modalForm || state.form.dialogError == "" || state.syncProfiles[syncServiceGitHub] != nil || state.activeSyncService() != syncServiceWebDAV {
		t.Fatal("failed save changed configuration")
	}
	for _, name := range []string{gistTokenSecret, syncServiceGitHub.passwordName()} {
		if _, found, err := state.secrets.vault.Get(name); err != nil || found {
			t.Fatalf("failed save retained secret %s: %v", name, err)
		}
	}
	state.Click(hitFormSubmit)
	if state.modal != modalNone || state.syncProfiles[syncServiceGitHub] == nil {
		t.Fatal("retry did not save")
	}
}

func TestLegacySyncProfileKeepsItsPasswordAndEnabledState(t *testing.T) {
	state, options, _ := backupTestState(t)
	for name, value := range map[string]string{gistTokenSecret: "legacy-token", backupPasswordSecret: "legacy-password"} {
		if err := state.secrets.vault.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	legacy := &savedSync{Kind: savedKindGist, Gist: &savedGist{ID: "abc123"}, Manual: true}
	state.restoreSyncServices(nil, legacy)
	if state.activeSyncService() != syncServiceGitHub || !state.sync.manual {
		t.Fatal("legacy enabled state changed")
	}
	if err := state.saveWithResult(); err != nil {
		t.Fatal(err)
	}
	restarted := newTestState(t, options)
	_, password, err := restarted.syncDraftSecrets(restarted.sync, "", "")
	if err != nil || password != "legacy-password" {
		t.Fatal("legacy password was lost")
	}
	restarted.Click(hitSyncConfigureS3)
	fillForm(restarted, "https://s3.example.com", "", "bucket", "", "")
	restarted.Click(hitFormSubmit)
	fillForm(restarted, "key", "secret", "")
	restarted.Click(hitFormSubmit)
	if restarted.modal != modalForm || restarted.form.dialogError == "" {
		t.Fatal("a new service inherited the legacy password")
	}
	restarted.Click(hitFormCancel)
	restarted.Click(hitSyncConfigureGist)
	restarted.Click(hitFormSubmit)
	if restarted.modal != modalNone || restarted.syncLegacyPassword != "" {
		t.Fatal("editing did not finish legacy password migration")
	}
	if value, found, err := restarted.secrets.vault.Get(syncServiceGitHub.passwordName()); err != nil || !found || value != "legacy-password" {
		t.Fatal("password was not migrated into the account")
	}
}

func TestSyncServiceSelectionReachesOtherWindows(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	first, second := pair.first, pair.second
	saveServiceForTest(t, first, syncServiceGitHub, "github-password")
	if second.syncProfiles[syncServiceGitHub] == nil || second.sync.configured() {
		t.Fatal("saved disabled profile not shared between windows")
	}
	first.toggleSyncService(syncServiceGitHub)
	if second.activeSyncService() != syncServiceGitHub {
		t.Fatal("enable did not reach second window")
	}
	second.toggleSyncService(syncServiceGitHub)
	if first.sync.configured() {
		t.Fatal("disable did not reach first window")
	}
}

type canceledSyncRead struct {
	started  chan struct{}
	canceled chan struct{}
}

func (store canceledSyncRead) Test(context.Context) error { return nil }
func (store canceledSyncRead) Put(context.Context, []byte, string) (string, error) {
	return "", errors.New("old service must not upload")
}
func (store canceledSyncRead) Get(ctx context.Context) ([]byte, string, error) {
	close(store.started)
	<-ctx.Done()
	close(store.canceled)
	return nil, "", ctx.Err()
}

func TestSwitchingSyncServiceCancelsOldRequestAndIgnoresItsCallback(t *testing.T) {
	state, _, _ := backupTestState(t)
	saveServiceForTest(t, state, syncServiceGitHub, "github-password")
	saveServiceForTest(t, state, syncServiceS3, "s3-password")
	state.toggleSyncService(syncServiceGitHub)
	posted := make(chan func(), 16)
	state.SetPost(func(fn func()) { posted <- fn })
	defer state.SetPost(nil)
	store := canceledSyncRead{make(chan struct{}), make(chan struct{})}
	state.sync.working = true
	state.syncRound(store, "password", 1)
	select {
	case <-store.started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	state.toggleSyncService(syncServiceS3)
	select {
	case <-store.canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("old request was not canceled")
	}
	select {
	case fn := <-posted:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("completion callback missing")
	}
	if state.activeSyncService() != syncServiceS3 || state.sync.err != "" || state.sync.working {
		t.Fatal("old completion corrupted new service state")
	}
}

func TestSyncDraftRejectsWrongBackupPasswordWithoutChangingService(t *testing.T) {
	server, dav := newFakeDAV(t)
	state, _, _ := backupTestState(t)
	configureSync(t, state, server)
	syncAndWait(t, state, hitSyncNow, state.messages.Tools.SyncDone)
	before := dav.putCount()
	state.Click(hitSyncConfigure)
	state.form.set(4, "different-password")
	state.Click(hitFormTest)
	waitFor(t, state, func() bool { return !state.form.testRunning })
	if state.form.dialogError != state.messages.Tools.BackupErrWrong {
		t.Fatalf("wrong password test: %q", state.form.dialogError)
	}
	if dav.putCount() != before || state.activeSyncService() != syncServiceWebDAV {
		t.Fatal("test wrote backup or changed service")
	}
	// The test never replaced the saved password.
	_, password, err := state.syncDraftSecrets(state.sync, "", "")
	if err != nil || password != testBackupPassword {
		t.Fatal("test stored the draft password")
	}
}

func TestEditingEnabledSyncPasswordCancelsOtherWindowsOldRequest(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	first, second := pair.first, pair.second
	saveServiceForTest(t, first, syncServiceGitHub, "old-password")
	first.toggleSyncService(syncServiceGitHub)
	ctx, cancel := first.beginSyncRequest()
	defer cancel()
	first.sync.working = true
	before := first.syncEpoch
	saveServiceForTest(t, second, syncServiceGitHub, "new-password")
	if first.syncEpoch == before || first.sync.working {
		t.Fatal("secret-only edit did not invalidate the old request")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("old network context is still live")
	}
	_, password, err := first.syncDraftSecrets(first.sync, "", "")
	if err != nil || password != "new-password" {
		t.Fatal("other window did not adopt new credentials")
	}
}

func TestSyncDraftRejectsAResultForEditedFields(t *testing.T) {
	server, dav := newFakeDAV(t)
	release := dav.holdRequests(t)
	state, _, _ := backupTestState(t)
	davTestForm(t, state, server, "s3cret")
	state.form.set(3, "different-backup.mottybak")
	release()
	waitFor(t, state, func() bool { return !state.form.testRunning })
	if state.form.dialogError != state.messages.Tools.SyncTestEdited || state.status == state.messages.Tools.SyncTestRunning {
		t.Fatal("an obsolete test was reported as current")
	}
	if state.sync.configured() || len(state.syncProfiles) != 0 {
		t.Fatal("test saved or enabled its draft")
	}
	dav.mu.Lock()
	remaining := len(dav.files)
	dav.mu.Unlock()
	if remaining != 0 {
		t.Fatal("obsolete test left temporary files behind")
	}
}

func TestSyncDraftDropsResultAfterAnotherWindowChangesSavedPassword(t *testing.T) {
	server, dav := newFakeDAV(t)
	pair := newWindowsOnOneStore(t)
	first, second := pair.first, pair.second
	first.syncDelay = time.Hour
	configureSync(t, first, server)
	first.sync.manual = true
	first.save()
	release := dav.holdRequests(t)
	first.Click(hitSyncConfigure)
	first.Click(hitFormTest)
	second.Click(hitSyncConfigure)
	second.form.set(4, "new-backup-password")
	second.Click(hitFormSubmit)
	if second.modal != modalNone {
		t.Fatal(second.form.dialogError)
	}
	release()
	waitFor(t, first, func() bool { return !first.form.testRunning })
	if first.form.dialogError != first.messages.Tools.SyncTestEdited {
		t.Fatal("test accepted obsolete saved credentials")
	}
}
