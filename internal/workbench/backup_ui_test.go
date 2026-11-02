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
	"github.com/vivaui/viva/ui"
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

func TestWebDAVFormTestsTheTypedConnectionWithoutSaving(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, password, ok := r.BasicAuth(); !ok || user != "alice" || password != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == "PROPFIND" {
			if r.URL.Path == "/dav/motty-sync" {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.WriteHeader(http.StatusMultiStatus)
			}
			return
		}
		w.WriteHeader(http.StatusConflict)
	}))
	t.Cleanup(server.Close)
	state, _, _ := backupTestState(t)
	state.Click(hitSyncConfigure)
	if state.modal != modalForm {
		t.Fatal("the server form did not open")
	}
	fillForm(state, server.URL+"/dav", "alice", "s3cret", "motty-sync/backup.mottybak")
	state.Click(hitFormTest)
	waitFor(t, state, func() bool {
		return !state.form.testRunning && state.form.dialogError != ""
	})
	if state.modal != modalForm || state.sync.configured() {
		t.Fatalf("the test saved the form: modal %d, sync %+v", state.modal, state.sync)
	}
	if !strings.Contains(state.form.dialogError, `"motty-sync"`) || !strings.Contains(state.form.dialogError, "409") {
		t.Fatalf("the test did not name the missing folder: %q", state.form.dialogError)
	}
}

func TestSyncCanRestoreARemoteBackup(t *testing.T) {
	server, dav := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	first.terminalScrollback = 10000
	first.save()
	if err := first.secrets.vault.Set("aikey:restore@prod.example.com:22", "hunter2"); err != nil {
		t.Fatal(err)
	}
	configureSync(t, first, server)
	syncAndWait(t, first, hitSyncNow, first.messages.Tools.SyncDone)

	second, _, _ := backupTestState(t)
	second.terminalScrollback = 7
	second.save()
	configureSync(t, second, server)
	state := second
	state.status = ""
	state.Click(hitSyncRestore)
	if state.modal != modalForm || !state.form.danger {
		t.Fatalf("the restore confirmation did not open: modal %d", state.modal)
	}
	state.Click(hitFormSubmit)
	waitRestore := func() bool {
		return !state.sync.working && state.status != "" && state.status != state.messages.Tools.SyncWorking
	}
	waitFor(t, state, waitRestore)
	if state.status != fmt.Sprintf(state.messages.Tools.BackupRestoredFmt, len(sampleHosts)) {
		t.Fatalf("restore failed: %q", state.status)
	}
	if state.terminalScrollback != 10000 {
		t.Fatalf("the remote backup was not restored: %d", state.terminalScrollback)
	}
	if value, found, err := state.secrets.vault.Get("aikey:restore@prod.example.com:22"); err != nil || !found || value != "hunter2" {
		t.Fatalf("the restored password: %q %v %v", value, found, err)
	}
	if state.sync.etag == "" {
		t.Fatal("the restored version was not remembered")
	}
	safety, _, err := loadState(state.options.StatePath + beforeRestoreSuffix)
	if err != nil || safety.TerminalScrollback != 7 {
		t.Fatalf("the safety copy: %d, %v", safety.TerminalScrollback, err)
	}
	if dav.puts != 1 {
		t.Fatalf("restore uploaded again (%d uploads)", dav.puts)
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

func syncAndWait(t *testing.T, state *State, id ui.ID, want string) {
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
	var canvas ui.Canvas
	size := ui.Size{Width: 1280, Height: 800}
	state.Paint(&canvas, size)
	for _, id := range []ui.ID{hitBackupExport, hitBackupRestore, hitSyncConfigure} {
		if hit, ok := hitByID(&canvas, id); !ok || hit.Disabled {
			t.Fatalf("control %d: found %v", id, ok)
		}
	}
	for _, id := range []ui.ID{hitSyncNow} {
		if hit, ok := hitByID(&canvas, id); ok && !hit.Disabled {
			t.Fatalf("control %d is usable without a server", id)
		}
	}
	state.Click(hitSyncConfigure)
	canvas = ui.Canvas{}
	state.Paint(&canvas, size)
	for _, id := range []ui.ID{hitFormFieldList.At(0), hitFormFieldList.At(4), hitFormSubmit, hitFormCancel} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatalf("form control %d is not painted", id)
		}
	}
	if canvas.InitialFocus() != hitFormFieldList.At(0) {
		t.Fatalf("initial focus %d", canvas.InitialFocus())
	}
	state.form.dialogError = "a problem"
	canvas = ui.Canvas{}
	state.Paint(&canvas, size)
}
