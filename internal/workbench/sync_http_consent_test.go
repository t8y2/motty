package workbench

import (
	"errors"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/cloudsync"
	"motty/internal/i18n"
)

func TestRemoteHTTPCheckboxIsAtTheBottomOfTheCloudForms(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		for _, action := range []gfx.ID{hitSyncConfigure, hitSyncConfigureS3} {
			state, _, _ := backupTestState(t)
			state.setLanguage(language)
			state.Click(action)
			var canvas gfx.Canvas
			state.Paint(&canvas, gfx.Size{Width: 1280, Height: 800})
			check, ok := hitByID(&canvas, hitFormCheckbox)
			last, _ := hitByID(&canvas, hitFormFieldList.At(4))
			if !ok || check.Role != gfx.RoleCheckbox || check.Checked || check.Bounds.Y <= last.Bounds.Y+last.Bounds.Height {
				t.Fatalf("checkbox not unchecked below the fields: %+v", check)
			}
			state.Click(hitFormCheckbox)
			if !state.form.checked {
				t.Fatal("checkbox did not toggle")
			}
			state.Click(hitFormCancel)
			state.Click(action)
			if state.form.checked || state.sync.allowInsecureHTTP {
				t.Fatal("cancel saved the draft consent")
			}
		}
	}
}

func TestWebDAVHTTPConsentSurvivesRestartAndCanBeRevoked(t *testing.T) {
	server, _ := newFakeDAV(t)
	state, options, _ := backupTestState(t)
	state.Click(hitSyncConfigure)
	fillForm(state, server.URL+"/dav", "alice", "s3cret", "", testBackupPassword)
	state.Click(hitFormCheckbox)
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool { return state.status == state.messages.Tools.SyncTestOK })
	state.Close()
	restored := newTestState(t, options)
	if !restored.sync.allowInsecureHTTP {
		t.Fatal("restart lost the saved consent")
	}
	// Reconstructed stores must enforce the saved permission for real remote URLs.
	view := restored.sync
	view.url = "http://backup.lan/dav"
	if _, err := view.newStore("secret"); err != nil {
		t.Fatalf("saved consent did not reach the WebDAV client: %v", err)
	}
	view.allowInsecureHTTP = false
	if _, err := view.newStore("secret"); !errors.Is(err, cloudsync.ErrInsecure) {
		t.Fatalf("remote HTTP without consent: %v", err)
	}
	restored.Click(hitSettings)
	restored.selectSettingsCategory(settingsBackup)
	restored.Click(hitSyncConfigureS3)
	if restored.form.checked {
		t.Fatal("WebDAV consent carried into a different service")
	}
	restored.Click(hitFormCancel)
	restored.Click(hitSyncConfigure)
	if !restored.form.checked {
		t.Fatal("edit form lost saved consent")
	}
	restored.Click(hitFormCheckbox)
	restored.Click(hitFormSubmit)
	waitFor(t, restored, func() bool { return restored.status == restored.messages.Tools.SyncTestOK })
	saved, _, err := loadState(options.StatePath)
	if err != nil || saved.Sync == nil || saved.Sync.AllowInsecureHTTP {
		t.Fatalf("revocation was not saved: %+v, %v", saved.Sync, err)
	}
}

func TestS3HTTPConsentReachesTheSecondFormAndSavedClient(t *testing.T) {
	server, _ := newFakeBucket(t)
	state, _, _ := backupTestState(t)
	state.Click(hitSyncConfigureS3)
	fillForm(state, "http://backup.lan", "", "mybucket", "", "")
	state.Click(hitFormSubmit)
	if state.form.dialogError != state.messages.Tools.SyncErrInsecure {
		t.Fatalf("remote HTTP was not refused: %q", state.form.dialogError)
	}
	state.form.set(0, server.URL)
	state.Click(hitFormCheckbox)
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool {
		return state.modal == modalForm && state.form.fields[0].label == state.messages.Tools.S3AccessKey
	})
	fillForm(state, "AKIATEST", "s3-secret-value", testBackupPassword)
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool { return state.status == state.messages.Tools.SyncTestOK })
	saved, _, err := loadState(state.options.StatePath)
	if err != nil || saved.Sync == nil || !saved.Sync.AllowInsecureHTTP {
		t.Fatalf("S3 consent was not saved: %+v, %v", saved.Sync, err)
	}
	view := state.sync
	view.s3.endpoint = "http://backup.lan"
	if _, err := view.newStore("secret"); err != nil {
		t.Fatal(err)
	}
	view.allowInsecureHTTP = false
	if _, err := view.newStore("secret"); !errors.Is(err, cloudsync.ErrInsecure) {
		t.Fatalf("S3 client ignored revocation: %v", err)
	}
}
