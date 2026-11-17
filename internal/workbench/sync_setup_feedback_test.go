package workbench

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestS3SetupTransitionsWithoutWaitingForBackgroundPosts(t *testing.T) {
	state, _, _ := backupTestState(t)
	var posted []func()
	state.SetPost(func(fn func()) { posted = append(posted, fn) })
	defer state.SetPost(nil)
	state.Click(hitSyncConfigureS3)
	fillForm(state, "https://s3.example.com", "us-east-1", "test-bucket", "test.mottybak", "yes")
	state.Click(hitFormSubmit)
	if state.modal != modalForm || state.form.title != state.messages.Tools.S3LoginTitle {
		t.Fatalf("Next closed the setup instead of opening credentials: modal=%v title=%q queued=%d", state.modal, state.form.title, len(posted))
	}
	fillForm(state, "test-access-key", "test-secret", testBackupPassword)
	state.Click(hitFormBack)
	if state.modal != modalForm || state.form.title != state.messages.Tools.S3DialogTitle || state.form.value(0) != "https://s3.example.com" {
		t.Fatal("Back did not immediately restore the bucket form")
	}
	state.Click(hitFormSubmit)
	if state.form.value(0) != "test-access-key" || state.form.value(1) != "test-secret" || state.form.value(2) != testBackupPassword {
		t.Fatal("switching steps lost the credentials")
	}
}

func TestSyncSettingsShowProgressAndConnectionTestSuccess(t *testing.T) {
	server, _ := newFakeGistAPI(t)
	state, _, _ := backupTestState(t)
	state.sync.manual = true
	state.Click(hitSyncConfigureGist)
	fillForm(state, "ghp_token", "", "", server.URL, testBackupPassword)
	state.Click(hitFormSubmit)
	state.toggleSyncService(syncServiceGitHub)
	state.testSync()
	_, progress := state.syncTexts()
	if progress != state.messages.Tools.SyncTestRunning {
		t.Fatalf("connection test is invisible: %q", progress)
	}
	waitFor(t, state, func() bool { return state.status == state.messages.Tools.SyncTestOK })
	_, complete := state.syncTexts()
	if !strings.Contains(complete, state.messages.Tools.SyncTestOK) || !strings.Contains(complete, state.messages.Tools.SyncRunNever) {
		t.Fatalf("connection success must not imply data was synced: %q", complete)
	}
	state.syncNow()
	_, progress = state.syncTexts()
	if progress != state.messages.Tools.SyncWorking {
		t.Fatalf("manual sync is invisible: %q", progress)
	}
	waitFor(t, state, func() bool { return state.status == state.messages.Tools.SyncDone })
	_, synced := state.syncTexts()
	if state.sync.last.IsZero() || synced != fmt.Sprintf(state.messages.Tools.SyncRunLastFmt, state.sync.last.Local().Format("2006-01-02 15:04")) {
		t.Fatalf("successful data sync has no visible completion time: %q", synced)
	}
	state.syncFailed(errors.New("test service unavailable"))
	_, failed := state.syncTexts()
	if !strings.Contains(failed, "test service unavailable") {
		t.Fatalf("sync failure is invisible: %q", failed)
	}
}

func TestManualSyncLoginFailuresAreVisibleOnSettingsPage(t *testing.T) {
	state, _, _ := backupTestState(t)
	state.sync.kind = syncGist
	state.syncTexts() // prime the previous row
	state.syncNow()
	_, message := state.syncTexts()
	if !strings.Contains(message, state.messages.Tools.GistErrFields) {
		t.Fatalf("missing credentials were only written to the hidden status line: %q", message)
	}
}

func TestS3SetupKeepsCredentialsFormOpenWhenSavingFails(t *testing.T) {
	server, _ := newFakeBucket(t)
	state, _, _ := backupTestState(t)
	state.Click(hitSyncConfigureS3)
	fillForm(state, server.URL, "", "mybucket", "", "")
	state.Click(hitFormSubmit)
	fillForm(state, "AKIATEST", "test-secret", testBackupPassword)
	state.saveBlocked = true
	state.Click(hitFormSubmit)
	if state.modal != modalForm || state.form.dialogError == "" || state.sync.working {
		t.Fatalf("failed save disappeared: modal=%v error=%q working=%v", state.modal, state.form.dialogError, state.sync.working)
	}
	state.saveBlocked = false
	state.Click(hitFormSubmit)
	if state.modal != modalNone {
		t.Fatalf("retry did not save: %q", state.form.dialogError)
	}
	state.toggleSyncService(syncServiceS3)
	state.testSync()
	waitFor(t, state, func() bool { return state.status == state.messages.Tools.SyncTestOK })
	saved, _, err := loadState(state.options.StatePath)
	if err != nil || saved.Sync == nil || saved.Sync.S3 == nil || saved.Sync.S3.Bucket != "mybucket" {
		t.Fatalf("S3 configuration was not persisted: %v", err)
	}
}

func TestSyncConnectionTestDoesNotOverwriteNewAccountStatus(t *testing.T) {
	server, _ := newFakeGistAPI(t)
	state, _, _ := backupTestState(t)
	configureGist(t, state, server, "")
	posted := make(chan func(), 16)
	state.SetPost(func(fn func()) { posted <- fn })
	defer state.SetPost(nil)
	state.testSync()
	var completion func()
	select {
	case completion = <-posted:
	case <-time.After(5 * time.Second):
		t.Fatal("connection test did not finish")
	}
	state.switchSync(syncState{kind: syncS3, s3: s3Settings{endpoint: "https://s3.example.com", bucket: "other"}})
	state.sync.err = "new account error"
	completion()
	if state.sync.err != "new account error" || state.sync.tested || state.sync.working {
		t.Fatal("old connection test overwrote the new account's status")
	}
}
