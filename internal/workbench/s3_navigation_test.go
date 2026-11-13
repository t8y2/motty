package workbench

import (
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestS3NavigationKeepsDraftUntilFinalSave(test *testing.T) {
	state, _, _ := backupTestState(test)
	state.openS3Configure()
	fillForm(state, "https://s3.example.com", "eu-west-1", "draft-bucket", "draft/key", "no")
	state.form.checked = true
	if state.form.submit == state.messages.Tools.SyncSave {
		test.Fatal("the bucket page offers Save instead of Next")
	}
	state.Click(hitFormSubmit)
	waitFor(test, state, func() bool { return state.modal == modalForm && state.form.count == 3 })
	if state.sync.configured() || state.hasSyncSecret(s3SecretName) {
		test.Fatal("moving to credentials saved the draft")
	}
	fillForm(state, "draft-access", "synthetic-secret", "synthetic-backup-password")
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1000, Height: 800})
	backID := tree.ID(formDialogHits, "form-back")
	if _, found := hitByID(&canvas, backID); !found {
		test.Fatal("the credentials page has no Back action")
	}
	state.Click(backID)
	waitFor(test, state, func() bool { return state.modal == modalForm && state.form.count == 5 })
	for index, want := range []string{"https://s3.example.com", "eu-west-1", "draft-bucket", "draft/key", "no"} {
		if got := state.form.value(index); got != want {
			test.Fatalf("bucket field %d = %q, want %q", index, got, want)
		}
	}
	if !state.form.checked {
		test.Fatal("Back lost HTTP consent")
	}
	state.form.set(2, "revised-bucket")
	state.Click(hitFormSubmit)
	waitFor(test, state, func() bool { return state.modal == modalForm && state.form.count == 3 })
	for index, want := range []string{"draft-access", "synthetic-secret", "synthetic-backup-password"} {
		if state.form.value(index) != want {
			test.Fatalf("Back/Next lost credential field %d", index)
		}
	}
	state.Click(hitFormCancel)
	if state.modal != modalNone || state.sync.configured() || state.hasSyncSecret(s3SecretName) || state.form.count != 0 {
		test.Fatal("cancel retained or saved the setup draft")
	}
	state.openS3Configure()
	if state.form.value(2) != "" {
		test.Fatal("a new setup inherited the canceled bucket")
	}
}

func TestS3NavigationSavesOnlyAfterReturningFromBucket(test *testing.T) {
	server, _ := newFakeBucket(test)
	state, _, _ := backupTestState(test)
	state.openS3Configure()
	fillForm(state, server.URL, "test-region", "mybucket", "custom/backup", "yes")
	state.Click(hitFormSubmit)
	waitFor(test, state, func() bool { return state.modal == modalForm && state.form.count == 3 })
	fillForm(state, "AKIATEST", "synthetic-secret", testBackupPassword)
	state.Click(hitFormBack)
	waitFor(test, state, func() bool { return state.modal == modalForm && state.form.count == 5 })
	state.form.set(3, "revised/backup")
	state.Click(hitFormSubmit)
	waitFor(test, state, func() bool { return state.modal == modalForm && state.form.count == 3 })
	state.Click(hitFormSubmit)
	if state.modal != modalNone {
		test.Fatalf("final Save failed: %q", state.form.dialogError)
	}
	waitFor(test, state, func() bool { return state.status == state.messages.Tools.SyncTestOK })
	restored := newTestState(test, state.options)
	if restored.sync.kind != syncS3 || restored.sync.s3.key != "revised/backup" || restored.sync.s3.accessKey != "AKIATEST" {
		test.Fatalf("saved settings = %+v", restored.sync.s3)
	}
	if got, found, err := restored.secrets.vault.Get(s3SecretName); err != nil || !found || got != "synthetic-secret" {
		test.Fatal("final Save did not retain the secret in the vault")
	}
	data, err := os.ReadFile(state.options.StatePath)
	if err != nil {
		test.Fatal(err)
	}
	if strings.Contains(string(data), "synthetic-secret") || strings.Contains(string(data), testBackupPassword) {
		test.Fatal("S3 credentials were written into configuration")
	}
}

func TestS3EmptyEndpointKeepsStopAction(test *testing.T) {
	state, _, _ := backupTestState(test)
	state.sync = syncState{kind: syncS3, s3: s3Settings{endpoint: "https://s3.example.com", bucket: "bucket"}}
	state.openS3Configure()
	state.form.set(0, "")
	if state.form.submitLabel(&state.form) != state.messages.Tools.S3Stop {
		test.Fatal("empty endpoint does not identify the stop-sync action")
	}
	state.Click(hitFormSubmit)
	state.Wake()
	if state.modal != modalNone || state.sync.configured() {
		test.Fatal("empty endpoint did not stop sync or opened the next page")
	}
}
