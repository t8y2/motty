package workbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestImportingMobaXtermSessionsAddsGroupedHostsOnceAndSaysWhatItLeftOut(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sessions.mxtsessions")
	content := "[Bookmarks]\nSubRep=Clients\\Acme\nImgNum=42\n" +
		"Web=#109#0%web.acme.test%2222%root%\n" +
		"Same as saved=#109#0%prod.example.com%22%deploy%\n" +
		"Desktop=#91#4%10.1.1.1%3389%Administrator%\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	options.Keystore = &memKeys{}
	// A host already saved, which the second session repeats.
	if err := saveState(options.StatePath, savedState{Hosts: sampleHosts}); err != nil {
		t.Fatal(err)
	}
	var request app.OpenFileOptions
	options.OpenFiles = func(asked app.OpenFileOptions) ([]string, error) {
		request = asked
		return []string{file}, nil
	}
	state := newTestState(t, options)

	state.Click(hitImportMenu)
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitImportSessions); !ok {
		t.Fatal("the hosts page has no button to import sessions")
	}
	state.Click(hitImportSessions)
	if state.modal != modalImportSessions {
		t.Fatalf("modal = %d", state.modal)
	}
	state.Click(hitImportMobaXterm)
	if !request.AllowMultiple || !strings.Contains(request.Title, "MobaXterm") {
		t.Fatalf("OpenFiles request = %+v", request)
	}
	if state.modal != modalHostImportResult || len(state.hosts) != len(sampleHosts)+1 {
		t.Fatalf("modal %d, hosts %d", state.modal, len(state.hosts))
	}
	host := state.hosts[len(state.hosts)-1]
	if host.Label != "Web" || host.Group != "Clients/Acme" || host.Address != "web.acme.test" || host.User != "root" || host.Port != 2222 {
		t.Fatalf("host = %+v", host)
	}
	result := state.hostImportResult
	if !strings.Contains(result.summary, "MobaXterm") || !strings.Contains(result.summary, "1 already saved") {
		t.Fatalf("summary = %q", result.summary)
	}
	warnings := strings.Join(result.warnings[:result.visible], "\n")
	if !strings.Contains(warnings, "not imported") || !strings.Contains(warnings, "No readable SSH passwords") {
		t.Fatalf("warnings = %q", warnings)
	}

	// It is kept.
	if restarted := newTestState(t, options); len(restarted.hosts) != len(sampleHosts)+1 {
		t.Fatalf("restored %d hosts", len(restarted.hosts))
	}
}

func TestImportingAmbiguousMobaXtermChineseWithExplicitEncoding(t *testing.T) {
	text := "[Bookmarks]\nSubRep=Production\n隆=#109#0%host.example.test%22%root%\n"
	legacy, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "legacy.mxtsessions")
	if err := os.WriteFile(file, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportMobaXterm)
	if state.modal != modalMobaEncoding || len(state.hosts) != 0 {
		t.Fatal("ambiguous default import persisted a garbled host")
	}
	_, texts := paintedTexts(state)
	if !contains(texts, "UTF-8: Production / ¡ · root@host.example.test") || !contains(texts, "GB18030: Production / 隆 · root@host.example.test") {
		t.Fatalf("encoding choices do not show distinguishable names: %v", texts)
	}
	state.Click(hitMobaChooseGB18030)
	if len(state.hosts) != 1 || state.hosts[0].Label != "隆" || state.hosts[0].Group != "Production" {
		t.Fatalf("explicit encoding did not preserve labels/groups: %+v", state.hosts)
	}
}

func TestAmbiguousMobaXtermBytesOutsideSessionsDoNotAsk(t *testing.T) {
	// The comment's bytes read as both encodings, but no imported session does:
	// there is nothing to choose between.
	text := "[Bookmarks]\n; 客户\nWeb=#109#0%host.example.test%22%root%\n"
	file := filepath.Join(t.TempDir(), "utf8.mxtsessions")
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportMobaXterm)
	if state.modal == modalMobaEncoding {
		t.Fatal("bytes that do not change a session asked which encoding to use")
	}
	if len(state.hosts) != 1 || state.hosts[0].Label != "Web" {
		t.Fatalf("import = %+v", state.hosts)
	}
}

func TestAmbiguousMobaXtermFilesMustBeImportedSeparately(t *testing.T) {
	dir := t.TempDir()
	legacy, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("[Bookmarks]\n隆=#109#0%gb.example.test%22%root%\n"))
	if err != nil {
		t.Fatal(err)
	}
	gbPath := filepath.Join(dir, "legacy.mxtsessions")
	utf8Path := filepath.Join(dir, "utf8.mxtsessions")
	if err := os.WriteFile(gbPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(utf8Path, []byte("[Bookmarks]\n中文=#109#0%utf8.example.test%22%root%\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{gbPath, utf8Path}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportMobaXterm)
	if len(state.hosts) != 0 || state.modal == modalMobaEncoding || !strings.Contains(state.status, "one file") {
		t.Fatalf("mixed encodings were not rejected before import: hosts=%d modal=%d status=%q", len(state.hosts), state.modal, state.status)
	}
}

func TestMobaEncodingPreviewShowsUsernameDifferences(t *testing.T) {
	legacy, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("[Bookmarks]\nWeb=#109#0%web.example.test%22%隆%\n"))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "user.mxtsessions")
	if err := os.WriteFile(file, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportMobaXterm)
	if state.modal != modalMobaEncoding || !strings.Contains(state.mobaEncoding.gb, "隆") || !strings.Contains(state.mobaEncoding.utf8, "¡") {
		t.Fatalf("username difference not shown: %+v", state.mobaEncoding)
	}
}

func TestImportWithoutPasswordsDoesNotClaimSuccessWhenHostSaveIsBlocked(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sessions.mxtsessions")
	if err := os.WriteFile(file, []byte("[Bookmarks]\nWeb=#109#0%web.example.test%22%root%\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	state.saveBlocked = true
	state.Click(hitImportSessions)
	state.Click(hitImportMobaXterm)
	if state.modal == modalHostImportResult || len(state.hosts) != 0 {
		t.Fatalf("blocked config reported successful import: modal=%d hosts=%d", state.modal, len(state.hosts))
	}
}

func TestImportingSessionsToleratesCancelAndBadFiles(t *testing.T) {
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	var answer error
	var paths []string
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return paths, answer }
	state := newTestState(t, options)

	answer = app.ErrCanceled
	state.Click(hitImportSessions)
	state.Click(hitImportXshell)
	if state.modal != modalNone || state.status != state.messages.Tools.ImportCanceled {
		t.Fatalf("modal %d status %q", state.modal, state.status)
	}

	answer = errors.New("no picker")
	state.Click(hitImportSessions)
	state.Click(hitImportPuTTY)
	if state.status != state.messages.ActionFeedback.FilePickerFailed {
		t.Fatalf("status = %q", state.status)
	}

	answer = nil
	paths = []string{filepath.Join(t.TempDir(), "nothing.xsh")}
	state.Click(hitImportSessions)
	state.Click(hitImportXshell)
	if want := fmt.Sprintf(state.messages.ActionFeedback.ImportSessionsFailedFmt, "Xshell"); state.modal != modalNone || len(state.hosts) != 0 || state.status != want {
		t.Fatalf("modal %d, hosts %d, status %q", state.modal, len(state.hosts), state.status)
	}

	state.Click(hitImportSessions)
	state.Click(hitImportSessionsCancel)
	if state.modal != modalNone {
		t.Fatalf("modal = %d", state.modal)
	}
}

func TestImportingElectermBookmarksAddsHostsWithTheirNestedGroups(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bookmarks-2026-10-26.json")
	content := `{"bookmarkGroups": [
		{"id": "lab", "title": "Lab", "bookmarkIds": [], "bookmarkGroupIds": ["prod"]},
		{"id": "prod", "title": "Prod", "bookmarkIds": ["pi"], "bookmarkGroupIds": []}
	], "bookmarks": [
		{"id": "pi", "title": "Pi", "host": "pi.local", "port": 22, "username": "me", "type": "ssh", "authType": "password", "password": "secret"}
	]}`
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportElecterm)
	if state.modal != modalSessionImportPreview || len(state.hosts) != 0 {
		t.Fatalf("import applied before password consent: modal=%d hosts=%d", state.modal, len(state.hosts))
	}
	state.Click(hitImportSavePasswords)
	if len(state.hosts) != 1 {
		t.Fatalf("hosts %+v", state.hosts)
	}
	if host := state.hosts[0]; host.Label != "Pi" || host.Group != "Lab/Prod" || host.Address != "pi.local" || host.User != "me" {
		t.Fatalf("host = %+v", host)
	}
	result := state.hostImportResult
	if !strings.Contains(result.summary, "Electerm") {
		t.Fatalf("summary = %q", result.summary)
	}
	// Electerm writes passwords into the export in the clear, which is a thing to
	// be told about rather than the usual "each program encrypts them".
	warnings := strings.Join(result.warnings[:result.visible], "\n")
	if !strings.Contains(warnings, "readable passwords") || strings.Contains(warnings, "each program encrypts") {
		t.Fatalf("warnings = %q", warnings)
	}
	if value, found := vaultValue(t, state, hostSecretName(state.hosts[0].CatalogID, hostPasswordPart)); !found || value != "secret" {
		t.Fatal("Electerm password was not saved in the host vault")
	}
	state.Click(hitHostImportDone)
	state.openEditHostID(state.hosts[0].ID)
	if state.editor.password.String() != "secret" {
		t.Fatal("imported password was not available when editing its host")
	}
}

func TestImportGuideLinkOpensTheDocsAndKeepsTheChooserOpen(t *testing.T) {
	var opened []string
	options := freshOptions(t)
	options.OpenURL = func(url string) error { opened = append(opened, url); return nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportGuide)
	if len(opened) != 1 || !strings.HasPrefix(opened[0], "https://motty.page/") {
		t.Fatalf("opened %q", opened)
	}
	if state.modal != modalImportSessions {
		t.Fatalf("modal = %d, want the chooser to stay open", state.modal)
	}
}

func TestImportingNetcattyHostsAddsHostsWithTheirGroups(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hosts_export_2026-10-31.csv")
	content := "Groups,Label,Tags,Notes,Hostname/IP,Protocol,Port,Username,Password,KeyPath,Passphrase\r\n" +
		"Lab/Pi,Pi,,,pi.local,ssh,22,me,secret,,\r\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportNetcatty)
	if state.modal != modalSessionImportPreview || len(state.hosts) != 0 {
		t.Fatalf("import applied before password consent: modal=%d hosts=%d", state.modal, len(state.hosts))
	}
	state.Click(hitImportConnectionsOnly)
	if len(state.hosts) != 1 {
		t.Fatalf("hosts %+v", state.hosts)
	}
	if host := state.hosts[0]; host.Label != "Pi" || host.Group != "Lab/Pi" || host.Address != "pi.local" || host.User != "me" {
		t.Fatalf("host = %+v", host)
	}
	result := state.hostImportResult
	if !strings.Contains(result.summary, "Netcatty") {
		t.Fatalf("summary = %q", result.summary)
	}
	warnings := strings.Join(result.warnings[:result.visible], "\n")
	if !strings.Contains(warnings, "readable passwords") {
		t.Fatalf("warnings = %q", warnings)
	}
	if _, found := vaultValue(t, state, hostSecretName(state.hosts[0].CatalogID, hostPasswordPart)); found {
		t.Fatal("connection-only import saved a password")
	}
}

func TestImportPasswordsOnlyFillsMissingHostsAndKeepsExistingValues(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hosts.csv")
	content := "Hostname/IP,Protocol,Port,Username,Password\n" +
		"prod.example.com,ssh,22,deploy,fill-me\n" +
		"staging.example.com,ssh,22,deploy,do-not-overwrite\n" +
		"new.example.com,ssh,22,root,new-secret\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	state.hosts = append(state.hosts, Host{ID: 1, Label: "Prod", Address: "prod.example.com", User: "deploy", Port: 22})
	state.hosts = append(state.hosts, Host{ID: 2, Label: "Staging", Address: "staging.example.com", User: "deploy", Port: 22})
	for index := range state.hosts {
		state.hosts[index].finish()
	}
	state.nextHostID = 3
	if err := state.secrets.vault.Set(hostSecretName(state.hosts[1].CatalogID, hostPasswordPart), "keep-me"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitImportSessions)
	state.Click(hitImportNetcatty)
	if state.modal != modalSessionImportPreview || len(state.hosts) != 2 {
		t.Fatal("expected a preview without changing hosts")
	}
	if state.sessionImport.eligible != 2 {
		t.Fatalf("eligible passwords=%d, want only missing existing and new", state.sessionImport.eligible)
	}
	canvas, texts := paintedTexts(state)
	if _, ok := hitByID(canvas, hitImportSavePasswords); !ok || !strings.Contains(strings.Join(texts, " "), "2 can be saved") {
		t.Fatal("preview does not show an explicit password-save choice and eligible count")
	}
	state.Click(hitImportSavePasswords)
	if state.modal != modalHostImportResult || len(state.hosts) != 3 {
		t.Fatalf("modal=%d hosts=%d error=%q", state.modal, len(state.hosts), state.dialogError)
	}
	for index, expected := range []string{"fill-me", "keep-me", "new-secret"} {
		value, found := vaultValue(t, state, hostSecretName(state.hosts[index].CatalogID, hostPasswordPart))
		if !found || value != expected {
			t.Fatalf("password for host %d not preserved/filled", index)
		}
	}
	if restarted := newTestState(t, options); len(restarted.hosts) != 3 {
		t.Fatalf("restored %d hosts", len(restarted.hosts))
	} else {
		for index, expected := range []string{"fill-me", "keep-me", "new-secret"} {
			value, found := vaultValue(t, restarted, hostSecretName(restarted.hosts[index].CatalogID, hostPasswordPart))
			if !found || value != expected {
				t.Fatalf("password for restored host %d was lost", index)
			}
		}
	}
}

func TestImportPasswordDoesNotOverwriteCredentialSavedAfterPreview(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hosts.csv")
	if err := os.WriteFile(file, []byte("Hostname/IP,Username,Password\nnew.example.com,root,imported\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportNetcatty)
	name := hostSecretName(state.sessionImport.passwords[0].host.CatalogID, hostPasswordPart)
	if err := state.secrets.vault.Set(name, "other-window"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitImportSavePasswords)
	if value, found := vaultValue(t, state, name); !found || value != "other-window" {
		t.Fatal("import overwrote a credential saved after its preview")
	}
}

func TestImportPasswordCancelAndVaultFailureLeaveHostsUnchanged(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hosts.csv")
	if err := os.WriteFile(file, []byte("Hostname/IP,Username,Password\nnew.example.com,root,secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportNetcatty)
	state.Click(hitImportPreviewCancel)
	if len(state.hosts) != 0 || state.modal != modalNone {
		t.Fatal("cancel imported hosts or secrets")
	}
	state.Click(hitImportSessions)
	state.Click(hitImportNetcatty)
	state.secrets.vault = nil
	state.Click(hitImportSavePasswords)
	if len(state.hosts) != 0 || state.modal != modalSessionImportPreview || state.dialogError == "" {
		t.Fatalf("vault failure created hosts: modal=%d hosts=%d error=%q", state.modal, len(state.hosts), state.dialogError)
	}
}

func TestImportPasswordRefusesBlockedOrFailedHostPersistence(t *testing.T) {
	for _, failure := range []string{"newer config", "config write"} {
		t.Run(failure, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "hosts.csv")
			if err := os.WriteFile(file, []byte("Hostname/IP,Username,Password\nnew.example.com,root,keep-private\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			options := freshOptions(t)
			options.Keystore = &memKeys{}
			options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
			state := newTestState(t, options)
			state.Click(hitImportSessions)
			state.Click(hitImportNetcatty)
			if state.modal != modalSessionImportPreview || len(state.sessionImport.passwords) != 1 {
				t.Fatal("test did not reach a password preview")
			}
			secretName := hostSecretName(state.sessionImport.passwords[0].host.CatalogID, hostPasswordPart)
			previousLocal := append([]byte(nil), state.configBase.local...)
			if failure == "newer config" {
				state.saveBlocked = true
			} else {
				state.lastSaved, state.hasSaved = state.snapshot(), true
				state.configBase.local = []byte("{invalid local state")
				t.Cleanup(func() { state.configBase.local = previousLocal })
			}
			state.Click(hitImportSavePasswords)
			if state.modal != modalSessionImportPreview || len(state.hosts) != 0 || state.dialogError == "" {
				t.Fatalf("failed save appeared successful: modal=%d hosts=%d error=%q", state.modal, len(state.hosts), state.dialogError)
			}
			if _, found := vaultValue(t, state, secretName); found {
				t.Fatal("host persistence failed but an orphan password remains in the vault")
			}
			secrets, err := state.secrets.vault.Export()
			if err != nil || len(secrets) != 0 {
				t.Fatalf("rollback left publishable vault records: count=%d err=%v", len(secrets), err)
			}
			if failure == "config write" {
				state.configBase.local = previousLocal
				state.Click(hitImportSavePasswords)
				if state.modal != modalHostImportResult || len(state.hosts) != 1 {
					t.Fatal("retry after restoring config did not import the host")
				}
				if value, found := vaultValue(t, state, secretName); !found || value != "keep-private" {
					t.Fatal("retry did not save the password")
				}
			}
		})
	}
}

func TestImportPasswordConflictsAndKeyHostsAreSkipped(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hosts.csv")
	content := "Hostname/IP,Protocol,Port,Username,Password\n" +
		"same.example.com,ssh,22,root,s3cr3t-first-471\n" +
		"same.example.com,ssh,22,root,s3cr3t-second-471\n" +
		"key.example.com,ssh,22,deploy,s3cr3t-for-key-471\n" +
		"fresh.example.com,ssh,22,root,s3cr3t-allowed-471\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	keyHost := Host{ID: 1, Label: "Key", Address: "key.example.com", Port: 22, User: "deploy", Auth: authPrivateKey}
	keyHost.finish()
	state.hosts = append(state.hosts, keyHost)
	state.nextHostID = 2
	state.Click(hitImportSessions)
	state.Click(hitImportNetcatty)
	if state.modal != modalSessionImportPreview || len(state.sessionImport.passwords) != 1 || state.sessionImport.conflicts != 1 {
		t.Fatalf("preview has %d candidates, %d conflicts", len(state.sessionImport.passwords), state.sessionImport.conflicts)
	}
	state.Click(hitImportSavePasswords)
	if state.modal != modalHostImportResult || len(state.hosts) != 3 {
		t.Fatalf("modal=%d hosts=%d", state.modal, len(state.hosts))
	}
	for _, host := range state.hosts {
		value, found := vaultValue(t, state, hostSecretName(host.CatalogID, hostPasswordPart))
		if host.Address == "fresh.example.com" && (!found || value != "s3cr3t-allowed-471") {
			t.Fatal("fresh host lost its password")
		}
		if host.Address != "fresh.example.com" && found {
			t.Fatal("conflicting or key host got an imported password")
		}
	}
	serialized, err := json.Marshal(state.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"s3cr3t-first-471", "s3cr3t-second-471", "s3cr3t-for-key-471", "s3cr3t-allowed-471"} {
		if strings.Contains(string(serialized), secret) || strings.Contains(state.status, secret) || strings.Contains(strings.Join(state.hostImportResult.warnings[:state.hostImportResult.visible], "\n"), secret) {
			t.Fatal("an imported secret appeared in host metadata or result text")
		}
	}
}

func TestImportingWindTermSessionsAddsHostsWithTheirGroups(t *testing.T) {
	file := filepath.Join(t.TempDir(), "user.sessions")
	content := `[{"session.group":"Lab","session.label":"Pi","session.port":22,"session.protocol":"SSH","session.target":"pi.local"}]`
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportWindTerm)
	if len(state.hosts) != 1 || state.hosts[0].Label != "Pi" || state.hosts[0].Group != "Lab" || !strings.Contains(state.hostImportResult.summary, "WindTerm") {
		t.Fatalf("hosts %+v, summary %q", state.hosts, state.hostImportResult.summary)
	}
}

// A MobaXterm folder that holds both sessions and subfolders must arrive as one
// group: the folder's own sessions and its children share that folder's path.
func TestImportingMobaXtermFolderWithSessionsAndSubfoldersKeepsOneGroup(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sessions.mxtsessions")
	content := "[Bookmarks]\nSubRep=65-Uyghur\\Kashgar\n" +
		"Report=#109#0%report.kashgar.test%22%root%\n" +
		"[Bookmarks_1]\nSubRep=65-Uyghur\\Kashgar\\3.0\n" +
		"Oracle=#109#0%oracle.kashgar.test%22%root%\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportMobaXterm)
	if state.modal != modalHostImportResult || len(state.hosts) != 2 {
		t.Fatalf("modal %d hosts %d", state.modal, len(state.hosts))
	}
	groups := map[string]string{}
	for _, host := range state.hosts {
		groups[host.Label] = host.Group
	}
	if got := groups["Report"]; got != "65-Uyghur/Kashgar" {
		t.Fatalf("folder's own session is in %q, want the folder's path", got)
	}
	if got := groups["Oracle"]; got != "65-Uyghur/Kashgar/3.0" {
		t.Fatalf("subfolder's session is in %q", got)
	}
	// The folder is one group, and the only one an empty-named parent leads to.
	var kashgar []string
	for _, group := range state.hostGroups {
		if hostGroupLeaf(group.Name) == "Kashgar" {
			kashgar = append(kashgar, group.Name)
		}
	}
	if len(kashgar) != 1 || kashgar[0] != "65-Uyghur/Kashgar" {
		t.Fatalf("Kashgar appears as %q, want one group", kashgar)
	}
}

// A catalog an earlier version imported with " / " between folder names reads
// back as one group per folder, not as a second group beside the folder.
func TestSavedSpacedGroupPathReadsBackAsOneGroup(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	saved := savedState{
		Hosts: []savedHost{
			{ID: "host_report", Label: "Report", Group: "65-Uyghur / Kashgar", Address: "report.kashgar.test", User: "root", Port: 22},
			{ID: "host_oracle", Label: "Oracle", Group: "65-Uyghur / Kashgar / 3.0", Address: "oracle.kashgar.test", User: "root", Port: 22},
		},
		HostGroups: []savedHostGroup{{Name: "65-Uyghur / Kashgar", Explicit: true}},
	}
	if err := saveState(options.StatePath, saved); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	groups := map[string]string{}
	for _, host := range state.hosts {
		groups[host.Label] = host.Group
	}
	if got := groups["Report"]; got != "65-Uyghur/Kashgar" {
		t.Fatalf("the folder's own session is in %q", got)
	}
	if got := groups["Oracle"]; got != "65-Uyghur/Kashgar/3.0" {
		t.Fatalf("the subfolder's session is in %q", got)
	}
	var kashgar []string
	for _, group := range state.hostGroups {
		if hostGroupLeaf(group.Name) == "Kashgar" {
			kashgar = append(kashgar, group.Name)
		}
	}
	if len(kashgar) != 1 {
		t.Fatalf("Kashgar is listed as %q, want one group", kashgar)
	}
	if index := state.hostGroupIndex("65-Uyghur/Kashgar"); index < 0 || !state.hostGroups[index].Explicit {
		t.Fatalf("the folder lost its explicit group: %+v", state.hostGroups[index])
	}
}
