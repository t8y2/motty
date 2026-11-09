package workbench

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

const testBackupPassword = "correct horse battery"

func backupTestState(t *testing.T) (*State, Options, string) {
	t.Helper()
	_, options := newWorld(t)
	saved := filepath.Join(t.TempDir(), "out.mottybak")
	options.SaveFile = func(app.SaveFileOptions) (string, error) { return saved, nil }
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{saved}, nil }
	state := newTestState(t, options)
	// These tests sync when they say so; the tests of syncing by itself ask for a
	// short wait.
	state.syncDelay, state.syncRetryBase = time.Hour, time.Hour
	state.activeSection, state.settingsCategory = sectionSettings, settingsBackup
	return state, options, saved
}

func fillForm(state *State, values ...string) {
	for index, value := range values {
		typeInto(state, hitFormFieldList.At(index), value)
	}
}

func exportBackup(t *testing.T, state *State) {
	t.Helper()
	state.Click(hitBackupExport)
	if state.modal != modalForm {
		t.Fatal("the export form did not open")
	}
	fillForm(state, testBackupPassword, testBackupPassword)
	state.Click(hitFormSubmit)
	if state.modal != modalNone {
		t.Fatalf("export refused: %q", state.form.dialogError)
	}
}

func TestABackupFileCarriesHostsSettingsKeysAndPasswordsToAnotherComputer(t *testing.T) {
	source, sourceOptions, file := backupTestState(t)
	if err := source.secrets.vault.Set("aikey:deploy@prod.example.com:22", "hunter2"); err != nil {
		t.Fatal(err)
	}
	const keyLine = "prod.example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPlaceholderKeyForTheTest"
	if err := os.WriteFile(sourceOptions.KnownHostsPath, []byte(keyLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source.terminalScrollback = 10000
	source.save()
	exportBackup(t, source)
	sealed, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "prod.example.com") || strings.Contains(string(sealed), "hunter2") {
		t.Fatal("the backup is readable without its password")
	}

	_, targetOptions := newWorld(t)
	targetOptions.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	if err := saveState(targetOptions.StatePath, savedState{Hosts: []savedHost{{Label: "Old", Address: "old.example.com", User: "me", Port: 22}}}); err != nil {
		t.Fatal(err)
	}
	target := newTestState(t, targetOptions)
	target.activeSection, target.settingsCategory = sectionSettings, settingsBackup
	if len(target.hosts) != 1 {
		t.Fatalf("the target starts with %d hosts", len(target.hosts))
	}
	if err := os.WriteFile(targetOptions.KnownHostsPath, []byte("other.example.com ssh-ed25519 AAAAOther\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	target.Click(hitBackupRestore)
	if target.modal != modalForm {
		t.Fatal("the restore form did not open")
	}
	fillForm(target, "not the password")
	target.Click(hitFormSubmit)
	if target.modal != modalForm || target.form.dialogError != target.messages.Tools.BackupErrWrong || len(target.hosts) != 1 {
		t.Fatalf("a wrong password: modal %d, error %q, hosts %d", target.modal, target.form.dialogError, len(target.hosts))
	}
	target.form.set(0, "")
	fillForm(target, testBackupPassword)
	target.Click(hitFormSubmit)
	if target.modal != modalNone {
		t.Fatalf("restore refused: %q", target.form.dialogError)
	}
	if len(target.hosts) != len(sampleHosts) || target.hosts[0].Label != "Production" || target.terminalScrollback != 10000 {
		t.Fatalf("restored %d hosts, first %q, scrollback %d", len(target.hosts), target.hosts[0].Label, target.terminalScrollback)
	}
	if value, found, err := target.secrets.vault.Get("aikey:deploy@prod.example.com:22"); err != nil || !found || value != "hunter2" {
		t.Fatalf("saved password: %q %v %v", value, found, err)
	}
	known, _ := os.ReadFile(targetOptions.KnownHostsPath)
	if !strings.Contains(string(known), keyLine) || !strings.Contains(string(known), "other.example.com") {
		t.Fatalf("known_hosts after the merge:\n%s", known)
	}
	before, err := os.ReadFile(targetOptions.StatePath + beforeRestoreSuffix)
	if err != nil || !strings.Contains(string(before), "old.example.com") {
		t.Fatalf("the safety copy: %v\n%s", err, before)
	}
	reloaded, _, err := loadState(targetOptions.StatePath)
	if err != nil || len(reloaded.Hosts) != len(sampleHosts) {
		t.Fatalf("the restored state was not saved: %d hosts, %v", len(reloaded.Hosts), err)
	}
	if !strings.Contains(target.status, strconv.Itoa(len(sampleHosts))) {
		t.Fatalf("status %q", target.status)
	}
}

func TestBackupRestoresCustomPasteShortcutOnANewDevice(t *testing.T) {
	source, _, file := backupTestState(t)
	want := keymap.Chord{Modifiers: gfx.ModifierAlt | gfx.ModifierShift, Char: 'v'}
	source.shortcuts.unbind(shortcutPaste)
	if _, ok := source.shortcuts.bind(shortcutPaste, want); !ok {
		t.Fatal("cannot configure a custom paste shortcut")
	}
	source.save()
	exportBackup(t, source)

	_, options := newWorld(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	target := newTestState(t, options)
	target.activeSection, target.settingsCategory = sectionSettings, settingsBackup
	target.Click(hitBackupRestore)
	fillForm(target, testBackupPassword)
	target.Click(hitFormSubmit)
	if target.modal != modalNone {
		t.Fatalf("restore refused: %q", target.form.dialogError)
	}
	if got := target.shortcuts.chords.Chord(shortcutPaste); got != want {
		t.Fatalf("restored paste shortcut = %+v, want %+v", got, want)
	}
	reopened := newTestState(t, options)
	if got := reopened.shortcuts.chords.Chord(shortcutPaste); got != want {
		t.Fatalf("paste shortcut after restart = %+v, want %+v", got, want)
	}
}

func TestOldBackupLeavesDeviceShortcutsUntouched(t *testing.T) {
	source, _, _ := backupTestState(t)
	document, err := source.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	document.ShortcutPreferences = nil
	target, _, _ := backupTestState(t)
	want := keymap.Chord{Modifiers: gfx.ModifierAlt | gfx.ModifierShift, Char: 'v'}
	target.shortcuts.unbind(shortcutPaste)
	if _, ok := target.shortcuts.bind(shortcutPaste, want); !ok {
		t.Fatal("cannot configure a custom paste shortcut")
	}
	target.save()
	if _, err := target.applyBackup(document); err != nil {
		t.Fatal(err)
	}
	if got := target.shortcuts.chords.Chord(shortcutPaste); got != want {
		t.Fatalf("old backup changed this device's paste shortcut: %+v", got)
	}
}

func TestABackupDoesNotCarryTheLoginsToTheBackupItself(t *testing.T) {
	state, _, _ := backupTestState(t)
	for _, name := range []string{webdavPasswordSecret, backupPasswordSecret, "aikey:a@b:22"} {
		if err := state.secrets.vault.Set(name, "value-"+name); err != nil {
			t.Fatal(err)
		}
	}
	document, err := state.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Secrets) != 1 || document.Secrets[0].Name != "aikey:a@b:22" || document.Secrets[0].Value == "" || document.Secrets[0].Stamp == "" {
		t.Fatalf("secrets in the backup: %v", document.Secrets)
	}
}

func TestExportNeedsAMatchingLongPassword(t *testing.T) {
	state, _, file := backupTestState(t)
	state.Click(hitBackupExport)
	fillForm(state, "short", "short")
	state.Click(hitFormSubmit)
	if state.form.dialogError != state.messages.Tools.BackupErrPasswordShort {
		t.Fatalf("a short password: %q", state.form.dialogError)
	}
	state.form.set(0, "")
	state.form.set(1, "")
	fillForm(state, testBackupPassword, testBackupPassword+"x")
	state.Click(hitFormSubmit)
	if state.form.dialogError != state.messages.Tools.BackupErrPasswordMismatch {
		t.Fatalf("mismatched passwords: %q", state.form.dialogError)
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatal("a refused export wrote a file")
	}
}

func TestRestoreIsRefusedWhileSessionsAreOpen(t *testing.T) {
	state, _, file := backupTestState(t)
	exportBackup(t, state)
	state.activeSection = sectionHosts
	openHost(state)
	state.activeSection = sectionSettings
	if len(state.sessions) == 0 {
		t.Fatal("no session opened")
	}
	state.Click(hitBackupRestore)
	if state.modal == modalForm || state.status != state.messages.Tools.BackupErrSessions {
		t.Fatalf("modal %d, status %q", state.modal, state.status)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
}

func TestARestoreKeepsTheLocalWebDAVServer(t *testing.T) {
	state, _, _ := backupTestState(t)
	exportBackup(t, state)
	state.sync = syncState{url: "https://dav.example.com/dav", user: "me", path: "x/y.mottybak", etag: `"v1"`}
	state.Click(hitBackupRestore)
	fillForm(state, testBackupPassword)
	state.Click(hitFormSubmit)
	if state.modal != modalNone || state.sync.url != "https://dav.example.com/dav" || state.sync.etag != `"v1"` {
		t.Fatalf("modal %d, sync %+v", state.modal, state.sync)
	}
	saved, _, _ := loadState(state.options.StatePath)
	if saved.Sync == nil || saved.Sync.URL != "https://dav.example.com/dav" {
		t.Fatalf("the server was not kept in the file: %+v", saved.Sync)
	}
}

// paintedSyncHit paints the settings page and returns the control the user
// would see, so a button that is never drawn cannot be clicked by a test either.
func paintedSyncHit(t *testing.T, state *State, id gfx.ID) gfx.Hit {
	t.Helper()
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	hit, found := hitByID(canvas, id)
	if !found {
		t.Fatalf("hit %d is not painted on the backup page", id)
	}
	return hit
}

// clickPainted clicks a control only if the settings page draws it enabled.
func clickPainted(t *testing.T, state *State, id gfx.ID) {
	t.Helper()
	if hit := paintedSyncHit(t, state, id); hit.Disabled {
		t.Fatalf("%q is disabled", hit.Label)
	}
	state.Click(id)
}

func TestTheSyncRowOffersARemoteRestoreOnceAServerIsSet(t *testing.T) {
	server, _ := newFakeDAV(t)
	state, _, _ := backupTestState(t)
	hit := paintedSyncHit(t, state, hitSyncRestore)
	if hit.Label != state.messages.Tools.SyncRestore || !hit.Disabled {
		t.Fatalf("with no server: %+v", hit)
	}
	configureSync(t, state, server)
	if hit := paintedSyncHit(t, state, hitSyncRestore); hit.Disabled {
		t.Fatal("the restore button stays disabled with a server set")
	}
	state.sync.working = true
	if hit := paintedSyncHit(t, state, hitSyncRestore); !hit.Disabled {
		t.Fatal("the restore button is enabled while a sync runs")
	}
}

// davTestForm opens the server form, fills it as typed and presses Test connection.
func davTestForm(t *testing.T, state *State, server *httptest.Server, password string) {
	t.Helper()
	state.Click(hitSyncConfigure)
	if state.modal != modalForm {
		t.Fatal("the server form did not open")
	}
	fillForm(state, server.URL+"/dav", "alice", password, "")
	state.Click(hitFormTest)
}

func TestWebDAVFormTestsTheTypedConnectionWithoutSaving(t *testing.T) {
	server, _ := newFakeDAV(t)
	state, _, _ := backupTestState(t)
	davTestForm(t, state, server, "s3cret")
	if !state.form.testRunning || state.status != state.messages.Tools.SyncTestRunning {
		t.Fatalf("the test did not start: running %v, status %q", state.form.testRunning, state.status)
	}
	waitFor(t, state, func() bool { return !state.form.testRunning })
	if state.status != state.messages.Tools.SyncTestOK || state.form.dialogError != "" || state.form.testNote != state.messages.Tools.SyncTestOK {
		t.Fatalf("status %q, form error %q, note %q", state.status, state.form.dialogError, state.form.testNote)
	}
	if _, texts := paintAt(state, gfx.Size{Width: 1400, Height: 900}); !contains(texts, state.messages.Tools.SyncTestOK) {
		t.Fatal("the dialog does not say the test went through")
	}
	if state.modal != modalForm || state.sync.configured() {
		t.Fatalf("the test saved the form: modal %d, sync %+v", state.modal, state.sync)
	}
}

func TestWebDAVFormTestSaysWhatIsWrongAndClearsTheStatusLine(t *testing.T) {
	server, _ := newFakeDAV(t)
	state, _, _ := backupTestState(t)
	davTestForm(t, state, server, "wrong")
	waitFor(t, state, func() bool { return !state.form.testRunning })
	if state.form.dialogError != state.messages.Tools.SyncErrUnauthorized {
		t.Fatalf("form error %q", state.form.dialogError)
	}
	if state.status == state.messages.Tools.SyncTestRunning {
		t.Fatal("the status line still says the test is running")
	}
}

func TestWebDAVFormTestNamesAFolderTheServerWillNotMake(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/dav/motty":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
		default:
			w.WriteHeader(http.StatusConflict)
		}
	}))
	t.Cleanup(server.Close)
	state, _, _ := backupTestState(t)
	davTestForm(t, state, server, "s3cret")
	waitFor(t, state, func() bool { return !state.form.testRunning })
	if !strings.Contains(state.form.dialogError, `"motty"`) || !strings.Contains(state.form.dialogError, "409") {
		t.Fatalf("the test did not name the missing folder: %q", state.form.dialogError)
	}
}

func TestWebDAVFormTestUsesTheSavedPasswordWhenTheFieldIsLeftEmpty(t *testing.T) {
	server, _ := newFakeDAV(t)
	state, _, _ := backupTestState(t)
	configureSync(t, state, server)
	state.status = ""
	state.Click(hitSyncConfigure) // the address, user and file come back; the password does not
	state.Click(hitFormTest)
	waitFor(t, state, func() bool { return !state.form.testRunning })
	if state.status != state.messages.Tools.SyncTestOK || state.form.dialogError != "" {
		t.Fatalf("status %q, form error %q", state.status, state.form.dialogError)
	}
}

func TestWebDAVFormTestWithoutAnyPasswordAsksForOneAndSendsNothing(t *testing.T) {
	server, dav := newFakeDAV(t)
	state, _, _ := backupTestState(t)
	davTestForm(t, state, server, "")
	if state.form.testRunning || state.form.dialogError != state.messages.Tools.SyncErrFields {
		t.Fatalf("running %v, form error %q", state.form.testRunning, state.form.dialogError)
	}
	if dav.putCount() != 0 {
		t.Fatal("a request went out")
	}
}

func TestWebDAVFormTestDoesNotEchoACredentialTypedIntoTheAddress(t *testing.T) {
	state, _, _ := backupTestState(t)
	state.Click(hitSyncConfigure)
	fillForm(state, "https://alice:hunter2@dav.example.com/dav", "alice", "s3cret", "")
	state.Click(hitFormTest)
	if strings.Contains(state.form.dialogError, "hunter2") || state.form.dialogError != state.messages.Tools.SyncErrAddress {
		t.Fatalf("form error %q", state.form.dialogError)
	}
}

func TestCancellingTheFormWhileItsTestRunsClearsTheStatusLine(t *testing.T) {
	server, dav := newFakeDAV(t)
	release := dav.holdRequests(t)
	state, _, _ := backupTestState(t)
	davTestForm(t, state, server, "s3cret")
	if state.status != state.messages.Tools.SyncTestRunning {
		t.Fatalf("status %q", state.status)
	}
	state.Click(hitFormCancel)
	if state.status == state.messages.Tools.SyncTestRunning {
		t.Fatal("the status line says testing after the form was cancelled")
	}
	release()
}

// restoreTwoComputers leaves the server holding the first computer's backup
// (scrollback 10000, one saved password) and returns a second computer, set up
// for the same server, whose own scrollback is 7.
func restoreTwoComputers(t *testing.T) (second *State, dav *fakeDAV, server *httptest.Server) {
	t.Helper()
	server, dav = newFakeDAV(t)
	first, _, _ := backupTestState(t)
	first.terminalScrollback = 10000
	first.save()
	if err := first.secrets.vault.Set("aikey:restore@prod.example.com:22", "hunter2"); err != nil {
		t.Fatal(err)
	}
	configureSync(t, first, server)
	syncAndWait(t, first, hitSyncNow, first.messages.Tools.SyncDone)

	second, _, _ = backupTestState(t)
	second.terminalScrollback = 7
	second.save()
	configureSync(t, second, server)
	return second, dav, server
}

func restoreRemote(t *testing.T, state *State) {
	t.Helper()
	state.status = ""
	clickPainted(t, state, hitSyncRestore)
	if state.modal != modalForm || !state.form.danger || state.form.description != state.messages.Tools.SyncRestoreWarning {
		t.Fatalf("the restore confirmation did not open: modal %d", state.modal)
	}
	state.Click(hitFormSubmit)
}

func waitRestored(t *testing.T, state *State) {
	t.Helper()
	waitFor(t, state, func() bool {
		return !state.sync.working && state.status != "" && state.status != state.messages.Tools.SyncWorking
	})
}

func TestSyncCanRestoreARemoteBackup(t *testing.T) {
	state, dav, _ := restoreTwoComputers(t)
	restoreRemote(t, state)
	waitRestored(t, state)
	if state.status != fmt.Sprintf(state.messages.Tools.BackupRestoredFmt, len(sampleHosts)) {
		t.Fatalf("restore failed: %q", state.status)
	}
	if state.terminalScrollback != 10000 {
		t.Fatalf("the remote backup was not restored: %d", state.terminalScrollback)
	}
	if value, found, err := state.secrets.vault.Get("aikey:restore@prod.example.com:22"); err != nil || !found || value != "hunter2" {
		t.Fatalf("the restored password: %q %v %v", value, found, err)
	}
	if state.sync.etag == "" || state.sync.last.IsZero() {
		t.Fatal("the restored version was not remembered")
	}
	safety, _, err := loadState(state.options.StatePath + beforeRestoreSuffix)
	if err != nil || safety.TerminalScrollback != 7 {
		t.Fatalf("the safety copy: %d, %v", safety.TerminalScrollback, err)
	}
	if dav.putCount() != 1 {
		t.Fatalf("restore uploaded (%d uploads)", dav.putCount())
	}
}

// The records a restore writes carry new stamps, so a sync started by them would
// upload them. Restoring must not start one; a change made afterwards still does.
func TestARemoteRestoreDoesNotStartASyncOfItsOwn(t *testing.T) {
	state, dav, _ := restoreTwoComputers(t)
	state.syncDelay = 20 * time.Millisecond
	restoreRemote(t, state)
	waitRestored(t, state)
	waitFor(t, state, func() bool { return !state.sync.holdBack })
	time.Sleep(300 * time.Millisecond)
	state.Wake()
	if dav.putCount() != 1 || state.sync.working {
		t.Fatalf("the restore was followed by a sync: %d uploads, working %v", dav.putCount(), state.sync.working)
	}

	state.terminalScrollback = 5000
	state.save()
	if state.sync.timer == nil {
		t.Fatal("a change after the restore no longer starts a sync")
	}
}

func TestRestoringARemoteBackupTwiceKeepsTheFirstSafetyCopy(t *testing.T) {
	state, _, _ := restoreTwoComputers(t)
	restoreRemote(t, state)
	waitRestored(t, state)
	state.terminalScrollback = 3
	state.save()
	restoreRemote(t, state)
	waitFor(t, state, func() bool { return !state.sync.working && state.terminalScrollback == 10000 })

	kept, _, err := loadState(state.options.StatePath + beforeRestoreSuffix)
	if err != nil || kept.TerminalScrollback != 3 {
		t.Fatalf("the latest safety copy: %d, %v", kept.TerminalScrollback, err)
	}
	aside, _ := filepath.Glob(state.options.StatePath + beforeRestoreSuffix + ".*")
	if len(aside) != 1 {
		t.Fatalf("the first safety copy was not kept aside: %v", aside)
	}
	original, _, err := loadState(aside[0])
	if err != nil || original.TerminalScrollback != 7 {
		t.Fatalf("the first safety copy: %d, %v", original.TerminalScrollback, err)
	}
}

func TestARemoteRestoreThatFindsNothingOrCannotOpenItChangesNothing(t *testing.T) {
	t.Run("nothing is stored there", func(t *testing.T) {
		state, dav, _ := restoreTwoComputers(t)
		dav.deleteFiles()
		restoreRemote(t, state)
		waitRestored(t, state)
		if !strings.Contains(state.status, state.messages.Tools.SyncErrNotFound) {
			t.Fatalf("status %q", state.status)
		}
		assertUntouched(t, state)
	})
	t.Run("the backup password is wrong", func(t *testing.T) {
		state, _, _ := restoreTwoComputers(t)
		if err := state.secrets.vault.Set(backupPasswordSecret, "some other long password"); err != nil {
			t.Fatal(err)
		}
		restoreRemote(t, state)
		waitRestored(t, state)
		if !strings.Contains(state.status, state.messages.Tools.BackupErrWrong) || state.sync.err != state.messages.Tools.BackupErrWrong {
			t.Fatalf("status %q, sync error %q", state.status, state.sync.err)
		}
		assertUntouched(t, state)
	})
}

func assertUntouched(t *testing.T, state *State) {
	t.Helper()
	if state.terminalScrollback != 7 || state.sync.working || state.sync.etag != "" {
		t.Fatalf("scrollback %d, working %v, etag %q", state.terminalScrollback, state.sync.working, state.sync.etag)
	}
	if _, err := os.Stat(state.options.StatePath + beforeRestoreSuffix); err == nil {
		t.Fatal("a failed restore wrote a safety copy")
	}
}

func TestARemoteRestoreIsRefusedWhileSessionsAreOpenEvenIfOneOpensLater(t *testing.T) {
	state, _, _ := restoreTwoComputers(t)
	clickPainted(t, state, hitSyncRestore)
	state.modal = modalNone // a session opens behind the confirmation
	state.activeSection = sectionHosts
	openHost(state)
	state.activeSection, state.modal = sectionSettings, modalForm
	if len(state.sessions) == 0 {
		t.Fatal("no session opened")
	}
	state.Click(hitFormSubmit)
	if state.modal != modalForm || state.form.dialogError != state.messages.Tools.BackupErrSessions {
		t.Fatalf("modal %d, form error %q", state.modal, state.form.dialogError)
	}
	if state.sync.working || state.terminalScrollback != 7 {
		t.Fatalf("working %v, scrollback %d", state.sync.working, state.terminalScrollback)
	}
	state.closeModal()
	state.Click(hitSyncRestore)
	if state.modal == modalForm || state.status != state.messages.Tools.BackupErrSessions {
		t.Fatalf("a restore was offered with a session open: modal %d, status %q", state.modal, state.status)
	}
}

func TestARemoteRestoreWaitsForARunningSync(t *testing.T) {
	state, _, _ := restoreTwoComputers(t)
	clickPainted(t, state, hitSyncRestore)
	state.sync.working = true // a sync that started while the confirmation was open
	state.Click(hitFormSubmit)
	if state.modal != modalForm || state.form.dialogError != state.messages.Tools.SyncWorking || state.terminalScrollback != 7 {
		t.Fatalf("modal %d, form error %q, scrollback %d", state.modal, state.form.dialogError, state.terminalScrollback)
	}
}

func TestAServerChangedWhileARemoteRestoreDownloadsIsNotRestoredFrom(t *testing.T) {
	state, dav, _ := restoreTwoComputers(t)
	release := dav.holdRequests(t)
	restoreRemote(t, state)
	if !state.sync.working {
		t.Fatal("the restore did not start")
	}
	state.Click(hitSyncConfigure)
	state.form.set(0, "") // an empty address stops syncing
	state.Click(hitFormSubmit)
	if state.sync.configured() {
		t.Fatal("the service was not cleared")
	}
	release()
	waitFor(t, state, func() bool { return state.status == state.messages.Tools.SyncRestoreChanged })
	if state.terminalScrollback != 7 || state.sync.etag != "" || state.sync.configured() {
		t.Fatalf("scrollback %d, sync %+v", state.terminalScrollback, state.sync)
	}
}

func TestARestoreThatCannotWriteTheSavedPasswordsDoesNotClaimSuccess(t *testing.T) {
	state, _, _ := restoreTwoComputers(t)
	// The vault file can no longer be replaced: the hosts and settings come
	// through, the passwords cannot.
	vaultFile := filepath.Join(filepath.Dir(state.options.StatePath), "vault.json")
	if err := os.Remove(vaultFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(vaultFile, 0o700); err != nil {
		t.Fatal(err)
	}
	restoreRemote(t, state)
	waitRestored(t, state)
	if !strings.HasPrefix(state.status, strings.SplitN(state.messages.Tools.BackupSecretsFailedFmt, "%", 2)[0]) {
		t.Fatalf("status %q", state.status)
	}
	if state.sync.etag != "" || !state.sync.last.IsZero() {
		t.Fatalf("a restore that lost the passwords was remembered: %+v", state.sync)
	}
}

func TestAFileRestoreThatCannotWriteTheSavedPasswordsDoesNotClaimSuccess(t *testing.T) {
	source, _, file := backupTestState(t)
	if err := source.secrets.vault.Set("aikey:deploy@prod.example.com:22", "hunter2"); err != nil {
		t.Fatal(err)
	}
	exportBackup(t, source)

	target, _, _ := backupTestState(t)
	target.options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	if err := target.secrets.vault.Set("aikey:other@example.com:22", "x"); err != nil {
		t.Fatal(err)
	}
	vaultFile := filepath.Join(filepath.Dir(target.options.StatePath), "vault.json")
	if err := os.Remove(vaultFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(vaultFile, 0o700); err != nil {
		t.Fatal(err)
	}
	target.Click(hitBackupRestore)
	fillForm(target, testBackupPassword)
	target.Click(hitFormSubmit)
	if target.status == fmt.Sprintf(target.messages.Tools.BackupRestoredFmt, len(sampleHosts)) || !strings.HasPrefix(target.status, strings.SplitN(target.messages.Tools.BackupSecretsFailedFmt, "%", 2)[0]) {
		t.Fatalf("status %q", target.status)
	}
}

// fakeDAV is a WebDAV folder in memory with a version tag that changes on every
// write, and basic auth.
type fakeDAV struct {
	mu      sync.Mutex
	files   map[string][]byte
	version int
	puts    int
	// raceOnce makes the next write find that someone else wrote first.
	raceOnce bool
	// fail is how many of the next requests the server answers with an error.
	fail int
	// gate, if set, holds every download until it is closed.
	gate chan struct{}
}

// holdRequests makes downloads wait; the returned function lets them go.
func (dav *fakeDAV) holdRequests(t *testing.T) (release func()) {
	gate := make(chan struct{})
	dav.mu.Lock()
	dav.gate = gate
	dav.mu.Unlock()
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release) // the server cannot close while a request waits
	return release
}

func (dav *fakeDAV) deleteFiles() {
	dav.mu.Lock()
	defer dav.mu.Unlock()
	clear(dav.files)
}

func (dav *fakeDAV) putCount() int {
	dav.mu.Lock()
	defer dav.mu.Unlock()
	return dav.puts
}

func newFakeDAV(t *testing.T) (*httptest.Server, *fakeDAV) {
	t.Helper()
	dav := &fakeDAV{files: map[string][]byte{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dav.mu.Lock()
		gate := dav.gate
		dav.mu.Unlock()
		if gate != nil {
			<-gate
		}
		dav.mu.Lock()
		defer dav.mu.Unlock()
		if user, password, ok := r.BasicAuth(); !ok || user != "alice" || password != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if dav.fail > 0 {
			dav.fail--
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		tag := func() string { return `"v` + strconv.Itoa(dav.version) + `"` }
		switch r.Method {
		case "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
		case "MKCOL":
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			if r.Header.Get("If-None-Match") == "*" && dav.files[r.URL.Path] != nil {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			if dav.raceOnce {
				dav.raceOnce = false
				dav.version++
			}
			if match := r.Header.Get("If-Match"); match != "" && (dav.files[r.URL.Path] == nil || match != tag()) {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			dav.files[r.URL.Path], _ = io.ReadAll(r.Body)
			dav.version++
			dav.puts++
			w.Header().Set("ETag", tag())
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			body, ok := dav.files[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("ETag", tag())
			w.Write(body)
		}
	}))
	t.Cleanup(server.Close)
	return server, dav
}

func configureSync(t *testing.T, state *State, server *httptest.Server) {
	t.Helper()
	state.Click(hitSyncConfigure)
	if state.modal != modalForm {
		t.Fatal("the server form did not open")
	}
	fillForm(state, server.URL+"/dav", "alice", "s3cret", "", testBackupPassword)
	state.Click(hitFormSubmit)
	if state.modal != modalNone {
		t.Fatalf("the server was refused: %q", state.form.dialogError)
	}
	waitFor(t, state, func() bool { return state.status == state.messages.Tools.SyncTestOK })
}

func syncAndWait(t *testing.T, state *State, id gfx.ID, want string) {
	t.Helper()
	state.status = ""
	state.Click(id)
	waitFor(t, state, func() bool {
		return state.status != "" && state.status != state.messages.Tools.SyncWorking || state.modal == modalForm
	})
	if want != "" && state.status != want {
		t.Fatalf("status %q, want %q", state.status, want)
	}
}

func TestSyncMergesTwoComputersThroughAWebDAVServer(t *testing.T) {
	server, dav := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	configureSync(t, first, server)
	if first.sync.path != defaultSyncPath || first.sync.user != "alice" {
		t.Fatalf("the server settings: %+v", first.sync)
	}
	saved, _, _ := loadState(first.options.StatePath)
	if saved.Sync == nil || saved.Sync.URL != server.URL+"/dav" {
		t.Fatalf("the server was not saved: %+v", saved.Sync)
	}
	rawState, _ := os.ReadFile(first.options.StatePath)
	if strings.Contains(string(rawState), "s3cret") || strings.Contains(string(rawState), testBackupPassword) {
		t.Fatal("a password is in the settings database")
	}

	first.terminalScrollback = 10000
	first.save()
	if err := first.secrets.vault.Set("aikey:deploy@prod.example.com:22", "hunter2"); err != nil {
		t.Fatal(err)
	}
	syncAndWait(t, first, hitSyncNow, first.messages.Tools.SyncDone)
	if first.sync.etag == "" || first.sync.last.IsZero() || dav.puts != 1 {
		t.Fatalf("after the first sync: %+v, %d uploads", first.sync, dav.puts)
	}
	for path, body := range dav.files {
		if strings.Contains(string(body), "prod.example.com") || strings.Contains(string(body), "hunter2") {
			t.Fatalf("%s is readable on the server", path)
		}
	}
	syncAndWait(t, first, hitSyncNow, first.messages.Tools.SyncDone)
	if dav.puts != 1 {
		t.Fatalf("a sync with nothing to send uploaded again (%d uploads)", dav.puts)
	}

	// Another computer takes what is there, and has nothing to take back.
	second, _, _ := backupTestState(t)
	configureSync(t, second, server)
	syncAndWait(t, second, hitSyncNow, second.messages.Tools.SyncDone)
	if second.terminalScrollback != 10000 {
		t.Fatalf("the setting did not arrive: %d", second.terminalScrollback)
	}
	if value, found, _ := second.secrets.vault.Get("aikey:deploy@prod.example.com:22"); !found || value != "hunter2" {
		t.Fatalf("the saved password did not arrive: %q %v", value, found)
	}
	if second.sync.etag == "" {
		t.Fatal("no version tag after the sync")
	}
}

// Each computer changes something while the other is not looking, and one
// deletes; after both sync, in either order, they hold the same.
func TestComputersThatChangedDifferentThingsEndUpWithBoth(t *testing.T) {
	server, _ := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	second, _, _ := backupTestState(t)
	configureSync(t, first, server)
	configureSync(t, second, server)
	done := first.messages.Tools.SyncDone

	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)
	syncAndWait(t, first, hitSyncNow, done)
	if len(first.hosts) != len(sampleHosts) || len(second.hosts) != len(sampleHosts) {
		t.Fatalf("hosts %d and %d before the changes", len(first.hosts), len(second.hosts))
	}

	// First deletes a host and sets a password; second adds a host and a snippet.
	deleted := first.hosts[0].Label
	first.hosts = first.hosts[1:]
	first.hostDataChanged()
	first.save()
	if err := first.secrets.vault.Set("aikey:only@first:22", "one"); err != nil {
		t.Fatal(err)
	}
	second.hosts = append(second.hosts, Host{ID: second.nextHostID, Label: "Added on second", Address: "second.example.com", User: "me", Port: 22})
	second.nextHostID++
	second.hosts[len(second.hosts)-1].finish()
	second.snippets.list = append(second.snippets.list, snippet{Name: "greet", Command: "echo hi"})
	second.hostDataChanged()
	second.save()

	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)
	syncAndWait(t, first, hitSyncNow, done)

	for name, state := range map[string]*State{"first": first, "second": second} {
		labels := map[string]bool{}
		for _, host := range state.hosts {
			labels[host.Label] = true
		}
		if labels[deleted] || !labels["Added on second"] || len(state.hosts) != len(sampleHosts) {
			t.Errorf("%s holds %v; %q should be gone and the added host there", name, labels, deleted)
		}
		if len(state.snippets.list) != 1 || state.snippets.list[0].Name != "greet" {
			t.Errorf("%s snippets %+v", name, state.snippets.list)
		}
		if value, found, _ := state.secrets.vault.Get("aikey:only@first:22"); !found || value != "one" {
			t.Errorf("%s did not get the password: %q %v", name, value, found)
		}
	}
}

// A deleted password goes the way a deleted host does.
func TestADeletedPasswordIsDeletedOnTheOtherComputerToo(t *testing.T) {
	server, _ := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	second, _, _ := backupTestState(t)
	configureSync(t, first, server)
	configureSync(t, second, server)
	done := first.messages.Tools.SyncDone
	if err := first.secrets.vault.Set("aikey:old@host:22", "secret"); err != nil {
		t.Fatal(err)
	}
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)
	if _, found, _ := second.secrets.vault.Get("aikey:old@host:22"); !found {
		t.Fatal("the password did not arrive")
	}
	if err := second.secrets.vault.Delete("aikey:old@host:22"); err != nil {
		t.Fatal(err)
	}
	syncAndWait(t, second, hitSyncNow, done)
	syncAndWait(t, first, hitSyncNow, done)
	if _, found, _ := first.secrets.vault.Get("aikey:old@host:22"); found {
		t.Fatal("the deletion did not reach the first computer")
	}
}

// Another computer replaces the file between this one's download and upload:
// the sync starts over from what is there now instead of failing or overwriting.
func TestASyncThatLosesTheRaceStartsOver(t *testing.T) {
	server, dav := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	configureSync(t, first, server)
	first.terminalScrollback = 10000
	first.save()
	dav.mu.Lock()
	dav.raceOnce = true
	dav.mu.Unlock()
	syncAndWait(t, first, hitSyncNow, first.messages.Tools.SyncDone)
	if dav.puts != 1 || first.sync.etag == "" {
		t.Fatalf("%d uploads, tag %q", dav.puts, first.sync.etag)
	}
	second, _, _ := backupTestState(t)
	configureSync(t, second, server)
	syncAndWait(t, second, hitSyncNow, second.messages.Tools.SyncDone)
	if second.terminalScrollback != 10000 {
		t.Fatalf("what was uploaded after the race did not arrive: %d", second.terminalScrollback)
	}
}

func TestAWrongBackupPasswordStopsTheSyncWithoutTouchingTheServer(t *testing.T) {
	server, dav := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	configureSync(t, first, server)
	syncAndWait(t, first, hitSyncNow, first.messages.Tools.SyncDone)
	puts := dav.puts

	second, _, _ := backupTestState(t)
	configureSync(t, second, server)
	if err := second.secrets.vault.Set(backupPasswordSecret, "another password"); err != nil {
		t.Fatal(err)
	}
	syncAndWait(t, second, hitSyncNow, "")
	if !strings.Contains(second.status, second.messages.Tools.BackupErrWrong) || dav.puts != puts {
		t.Fatalf("status %q, %d uploads", second.status, dav.puts-puts)
	}
}

func TestTheServerFormRefusesAnInsecureAddressAndNeedsALogin(t *testing.T) {
	state, _, _ := backupTestState(t)
	state.Click(hitSyncConfigure)
	fillForm(state, "http://dav.example.com/dav", "alice", "pw", "", testBackupPassword)
	state.Click(hitFormSubmit)
	if state.form.dialogError != state.messages.Tools.SyncErrInsecure {
		t.Fatalf("an http address: %q", state.form.dialogError)
	}
	state.form.set(0, "")
	fillForm(state, "https://dav.example.com/dav")
	state.form.set(2, "")
	state.Click(hitFormSubmit)
	if state.form.dialogError != state.messages.Tools.SyncErrFields {
		t.Fatalf("no password: %q", state.form.dialogError)
	}
	if state.sync.url != "" {
		t.Fatal("a refused form still saved the server")
	}
}

func TestSyncButtonsAreIdleWithoutAServer(t *testing.T) {
	state, _, _ := backupTestState(t)
	state.Click(hitSyncNow)
	if state.modal != modalNone || state.sync.working {
		t.Fatalf("modal %d working %v", state.modal, state.sync.working)
	}
}

func TestTheBackupPageAndTheFormPaintTheirControls(t *testing.T) {
	state, _, _ := backupTestState(t)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1280, Height: 800}
	state.Paint(&canvas, size)
	for _, id := range []gfx.ID{hitBackupExport, hitBackupRestore, hitSyncConfigure} {
		if hit, ok := hitByID(&canvas, id); !ok || hit.Disabled {
			t.Fatalf("control %d: found %v", id, ok)
		}
	}
	for _, id := range []gfx.ID{hitSyncNow} {
		if hit, ok := hitByID(&canvas, id); ok && !hit.Disabled {
			t.Fatalf("control %d is usable without a server", id)
		}
	}
	state.Click(hitSyncConfigure)
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	for _, id := range []gfx.ID{hitFormFieldList.At(0), hitFormFieldList.At(4), hitFormSubmit, hitFormCancel} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatalf("form control %d is not painted", id)
		}
	}
	if canvas.InitialFocus() != hitFormFieldList.At(0) {
		t.Fatalf("initial focus %d", canvas.InitialFocus())
	}
	state.form.dialogError = "a problem"
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
}
