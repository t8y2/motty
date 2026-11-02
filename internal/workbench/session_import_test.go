package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/sessionimport"
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
	if host.Label != "Web" || host.Group != "Clients / Acme" || host.Address != "web.acme.test" || host.User != "root" || host.Port != 2222 {
		t.Fatalf("host = %+v", host)
	}
	result := state.hostImportResult
	if !strings.Contains(result.summary, "MobaXterm") || !strings.Contains(result.summary, "1 already saved") {
		t.Fatalf("summary = %q", result.summary)
	}
	warnings := strings.Join(result.warnings[:result.visible], "\n")
	if !strings.Contains(warnings, "not imported") || !strings.Contains(warnings, "Saved passwords are not imported") {
		t.Fatalf("warnings = %q", warnings)
	}

	// It is kept.
	if restarted := newTestState(t, options); len(restarted.hosts) != len(sampleHosts)+1 {
		t.Fatalf("restored %d hosts", len(restarted.hosts))
	}
}

func TestSessionImportDeduplicatesByCatalogID(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	sshDuplicate := Host{Address: "different.example.com", User: "deploy", Port: 22}
	sshDuplicate.finish()
	streamDuplicate := Host{Protocol: protocolTelnet, Address: "different-stream.example.com", Port: 23}
	streamDuplicate.finish()
	state.hosts = []Host{
		{ID: 1, CatalogID: "host_ssh_custom", Label: "SSH custom", Address: "shared.example.com", User: "deploy", Port: 22},
		{ID: 2, CatalogID: sshDuplicate.CatalogID, Label: "SSH old", Address: "old.example.com", Port: 22},
		{ID: 3, CatalogID: "host_stream_custom", Label: "Stream custom", Protocol: protocolTelnet, Address: "shared-stream.example.com", Port: 23},
		{ID: 4, CatalogID: streamDuplicate.CatalogID, Label: "Stream old", Protocol: protocolTelnet, Address: "old-stream.example.com", Port: 23},
	}
	for index := range state.hosts {
		state.hosts[index].finish()
	}
	state.nextHostID = 5

	plan := state.sessionImportPlan(sessionimport.Result{Entries: []sessionimport.Entry{
		{Name: "SSH alias", Protocol: sessionimport.SSH, Host: "shared.example.com", User: "deploy", Port: 22},
		{Name: "SSH same ID", Protocol: sessionimport.SSH, Host: "different.example.com", User: "deploy", Port: 22},
		{Name: "SSH planned duplicate", Protocol: sessionimport.SSH, Host: "shared.example.com", User: "deploy"},
		{Name: "Stream alias", Protocol: sessionimport.Telnet, Host: "shared-stream.example.com", Port: 23},
		{Name: "Stream same ID", Protocol: sessionimport.Telnet, Host: "different-stream.example.com", Port: 23},
		{Name: "Stream planned duplicate", Protocol: sessionimport.Telnet, Host: "shared-stream.example.com"},
	}})
	if len(plan.hosts) != 2 || plan.duplicateHosts != 4 {
		t.Fatalf("hosts=%+v duplicates=%d", plan.hosts, plan.duplicateHosts)
	}
	if plan.hosts[0].Label != "SSH alias" || plan.hosts[0].CatalogID == "host_ssh_custom" ||
		plan.hosts[1].Label != "Stream alias" || plan.hosts[1].CatalogID == "host_stream_custom" {
		t.Fatalf("imported hosts = %+v", plan.hosts)
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
	if len(state.hosts) != 1 {
		t.Fatalf("hosts %+v", state.hosts)
	}
	if host := state.hosts[0]; host.Label != "Pi" || host.Group != "Lab / Prod" || host.Address != "pi.local" || host.User != "me" {
		t.Fatalf("host = %+v", host)
	}
	result := state.hostImportResult
	if !strings.Contains(result.summary, "Electerm") {
		t.Fatalf("summary = %q", result.summary)
	}
	// Electerm writes passwords into the export in the clear, which is a thing to
	// be told about rather than the usual "each program encrypts them".
	warnings := strings.Join(result.warnings[:result.visible], "\n")
	if !strings.Contains(warnings, "clear text") || strings.Contains(warnings, "each program encrypts") {
		t.Fatalf("warnings = %q", warnings)
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
	if len(state.hosts) != 1 {
		t.Fatalf("hosts %+v", state.hosts)
	}
	if host := state.hosts[0]; host.Label != "Pi" || host.Group != "Lab / Pi" || host.Address != "pi.local" || host.User != "me" {
		t.Fatalf("host = %+v", host)
	}
	result := state.hostImportResult
	if !strings.Contains(result.summary, "Netcatty") {
		t.Fatalf("summary = %q", result.summary)
	}
	warnings := strings.Join(result.warnings[:result.visible], "\n")
	if !strings.Contains(warnings, "clear text") {
		t.Fatalf("warnings = %q", warnings)
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
