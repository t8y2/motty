package workbench

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/backup"
	"motty/internal/i18n"
	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
)

func TestHostCatalogTreeFilterSelectionAndBatchMoveCompose(t *testing.T) {
	state := newTestState(t, testOptions(t))
	servers := state.hostGroupIndex("Servers")
	if servers < 0 {
		t.Fatalf("groups = %+v", state.hostGroups)
	}
	state.toggleHostGroup(servers)
	canvas, _ := paintedTexts(state)
	if _, ok := hostListHostHit(canvas, state, state.hosts[0].ID); ok {
		t.Fatal("collapsed group still exposes a host row")
	}
	if _, ok := hostListHostHit(canvas, state, state.hosts[2].ID); !ok {
		t.Fatal("collapsing Servers hid Network")
	}

	typeSearch(state, "Production")
	canvas, _ = paintedTexts(state)
	if _, ok := hostListHostHit(canvas, state, state.hosts[0].ID); !ok {
		t.Fatal("filter did not reveal a match inside a collapsed group")
	}
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	state.search = text.Editor{}
	state.syncVisible()
	if !state.hostSelected(state.hosts[0].ID) {
		t.Fatal("clearing the filter lost the hidden selection")
	}

	clickHostAction(state, hitMoveHostGroup)
	state.Click(hostMoveGroupChoice(t, state, "Network"))
	if state.hosts[0].Group != "Network" || !state.hostSelected(state.hosts[0].ID) {
		t.Fatalf("batch move result host=%+v selection=%v", state.hosts[0], state.hostSelections)
	}
	canvas, _ = paintedTexts(state)
	if _, ok := hostListHostHit(canvas, state, state.hosts[0].ID); !ok {
		t.Fatal("moved selected host was not placed in the expanded target branch")
	}
}

func TestHostTagsAndCompactIconEditPersistAndFilter(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitHostList.At(int(state.hosts[0].ID)))
	clickHostAction(state, hitEditHost)
	state.editor.tags = text.Editor{}
	for _, tag := range []string{"prod", "critical", "PROD"} {
		typeInto(state, hitDialogTags, tag)
		state.Key(gfx.KeyEvent{Target: hitDialogTags, Key: gfx.KeyEnter})
	}
	state.Click(hitHostTagsDismiss)
	chooseHostIcon(t, state, "Terminal")
	state.Click(hitDialogSubmit)
	host := state.hosts[0]
	if host.Tags != "prod, critical" || host.Icon != hostIconTerminal {
		t.Fatalf("edited host = %+v", host)
	}
	typeSearch(state, "critical")
	if len(state.visible) != 1 || state.hosts[state.visible[0]].ID != host.ID {
		t.Fatalf("tag filter = %v", state.visible)
	}
	restarted := newTestState(t, options)
	if restarted.hosts[0].Tags != "prod, critical" || restarted.hosts[0].Icon != hostIconTerminal || restarted.hosts[0].CatalogID != host.CatalogID {
		t.Fatalf("restored host = %+v", restarted.hosts[0])
	}
}

func TestHostCatalogExportImportRoundTripContainsNoSecrets(t *testing.T) {
	exportPath := filepath.Join(t.TempDir(), "catalog.json")
	options := testOptions(t)
	var saveOptions app.SaveFileOptions
	options.SaveFile = func(options app.SaveFileOptions) (string, error) {
		saveOptions = options
		return exportPath, nil
	}
	state := newTestState(t, options)
	setHostTags(&state.hosts[0], []string{"prod", "critical"})
	state.hosts[0].Icon = hostIconGlobe
	servers := state.hostGroupIndex("Servers")
	state.toggleHostGroup(servers)
	state.Click(hitExportHosts)
	if state.modal != modalForm {
		t.Fatalf("export dialog did not open: %v", state.modal)
	}
	state.Click(hitFormBack)
	if saveOptions.DefaultExtension != "json" || saveOptions.SuggestedName != "motty-hosts.json" {
		t.Fatalf("save options = %+v", saveOptions)
	}
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(data))
	for _, forbidden := range []string{"password", "passphrase", "secret", "identity_files", "private_key"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("export contains forbidden field %q: %s", forbidden, data)
		}
	}
	var document hostCatalogDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Format != hostCatalogFormat || document.Version != hostCatalogSchemaVersion || len(document.Hosts) != 3 {
		t.Fatalf("document = %+v", document)
	}

	importOptions := freshOptions(t)
	importOptions.OpenFiles = func(options app.OpenFileOptions) ([]string, error) {
		if options.Title == "" || len(options.Filters) != 1 {
			t.Fatalf("open options = %+v", options)
		}
		return []string{exportPath}, nil
	}
	imported := New(importOptions)
	t.Cleanup(imported.Close)
	imported.Click(hitImportHostCatalog)
	if len(imported.hosts) != len(state.hosts) {
		t.Fatalf("imported hosts = %+v", imported.hosts)
	}
	for index, want := range state.hosts {
		got := imported.hosts[index]
		if got.CatalogID != want.CatalogID || got.Label != want.Label || got.Group != want.Group || got.Address != want.Address || got.User != want.User || got.Port != want.Port || got.Icon != want.Icon || formatHostTags(got) != formatHostTags(want) {
			t.Fatalf("round trip host %d: got=%+v want=%+v", index, got, want)
		}
	}
	if imported.hostGroups[imported.hostGroupIndex("Servers")].Expanded {
		t.Fatal("collapsed group did not round trip")
	}

	imported.Click(hitImportHostCatalog)
	if len(imported.hosts) != 3 || !strings.Contains(imported.status, "3") {
		t.Fatalf("duplicate import hosts=%d status=%q", len(imported.hosts), imported.status)
	}
}

func TestHostCatalogStillImportsVersionOneDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "motty-hosts-v1.json")
	document := `{"format":"motty-host-catalog","version":1,"hosts":[{"id":"host_old","label":"Old","address":"old.example.com","port":22}]}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	read, err := readHostCatalog(path)
	if err != nil || len(read.Hosts) != 1 || read.Hosts[0].Label != "Old" {
		t.Fatalf("version-one catalog = %+v, %v", read, err)
	}
}

func TestHostCatalogExportsCheckedSubsetAndHandlesPickerErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selected.json")
	options := testOptions(t)
	options.SaveFile = func(app.SaveFileOptions) (string, error) { return path, nil }
	state := newTestState(t, options)
	state.Click(hitHostCheckList.At(int(state.hosts[1].ID)))
	state.Click(hitExportHosts)
	state.Click(hitFormBack)
	document, err := readHostCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Hosts) != 1 || document.Hosts[0].Label != "Staging" || len(document.Groups) != 1 || document.Groups[0].Name != "Servers" {
		t.Fatalf("selected export = %+v", document)
	}

	options = freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return nil, errors.New("picker failed") }
	state = newTestState(t, options)
	state.Click(hitImportHostCatalog)
	if len(state.hosts) != 0 || state.status != state.messages.ActionFeedback.FilePickerFailed {
		t.Fatalf("failed import hosts=%d status=%q", len(state.hosts), state.status)
	}
}

func TestHostCatalogEncryptedExportImportRoundTripWithPasswords(t *testing.T) {
	exportPath := filepath.Join(t.TempDir(), "motty-hosts-encrypted.json")
	options := testOptions(t)
	options.SaveFile = func(app.SaveFileOptions) (string, error) { return exportPath, nil }
	state := newTestState(t, options)

	// Set up hosts covering SSH password + OTP + inline proxy password, VNC system password,
	// RDP password, shared credential, and shared proxy profile.
	sshHost := state.hosts[0]
	sshHost.Auth = authPassword
	sshHost.Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "127.0.0.1:1080", user: "proxy-user"}
	state.hosts[0] = sshHost

	vncHost := Host{
		ID: state.nextHostID, CatalogID: "host_vnc_sys", Label: "Desktop VNC",
		Protocol: protocolVNC, Address: "vnc.example.com", User: "pi", Port: 5900,
		Credential: "cred_shared",
	}
	vncHost.finish()
	state.nextHostID++

	rdpHost := Host{
		ID: state.nextHostID, CatalogID: "host_rdp_prof", Label: "Office RDP",
		Protocol: protocolRDP, Address: "rdp.example.com", User: "corp\\admin", Port: 3389,
		ProxyProfile: "prof_corp",
	}
	rdpHost.finish()
	state.nextHostID++

	state.hosts = append(state.hosts, vncHost, rdpHost)
	state.credentials = append(state.credentials, credential{ID: "cred_shared", Name: "Shared Login", User: "shared-user"})
	state.proxyProfiles = append(state.proxyProfiles, proxyProfile{ID: "prof_corp", Name: "Corp SOCKS", Kind: sshconn.ProxySOCKS5, Address: "proxy.corp.local:1080", User: "corp-proxy"})
	state.hostDataChanged()
	state.save()

	wantSecrets := map[string]string{
		hostSecretName(sshHost.CatalogID, hostPasswordPart):            "ssh-secret-pw",
		hostSecretName(sshHost.CatalogID, hostOTPPart):                 "JBSWY3DPEHPK3PXP",
		hostProxyPasswordSecret(sshHost.CatalogID, sshHost.Proxy):      "inline-proxy-pw",
		hostSecretName(vncHost.CatalogID, vncSystemPasswordPart("pi")): "vnc-pi-pw",
		hostSecretName(rdpHost.CatalogID, hostPasswordPart):            "rdp-secret-pw",
		credentialSecretName("cred_shared"):                            "shared-cred-pw",
		proxyProfileSecretName("prof_corp"):                            "corp-proxy-pw",
	}
	for name, val := range wantSecrets {
		if err := state.secrets.vault.Set(name, val); err != nil {
			t.Fatal(err)
		}
	}

	state.Click(hitExportHosts)
	if state.modal != modalForm {
		t.Fatalf("modal = %v, want modalForm", state.modal)
	}
	// Empty password is rejected.
	state.Click(hitFormSubmit)
	if state.modal != modalForm || state.form.dialogError != state.messages.HostCatalogErrPasswordEmpty {
		t.Fatalf("empty password error = %q, modal=%v", state.form.dialogError, state.modal)
	}
	// A password shorter than a backup's is refused too: the file holds every
	// saved password of the exported hosts, and nothing else protects it.
	fillForm(state, "short", "short")
	state.Click(hitFormSubmit)
	if state.modal != modalForm || state.form.dialogError != state.messages.Tools.BackupErrPasswordShort {
		t.Fatalf("short password error = %q, modal=%v", state.form.dialogError, state.modal)
	}
	state.form.set(0, "")
	state.form.set(1, "")
	// Mismatched confirmation is rejected.
	fillForm(state, "export-pass-1", "export-pass-2")
	state.Click(hitFormSubmit)
	if state.modal != modalForm || state.form.dialogError != state.messages.Tools.BackupErrPasswordMismatch {
		t.Fatalf("mismatch error = %q, modal=%v", state.form.dialogError, state.modal)
	}
	// Matching password exports an encrypted file containing zero plaintext secrets or hosts.
	state.form.set(0, "export-pass-123")
	state.form.set(1, "export-pass-123")
	state.Click(hitFormSubmit)
	if state.modal != modalNone {
		t.Fatalf("encrypted export failed: %q", state.form.dialogError)
	}
	sealed, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range wantSecrets {
		if strings.Contains(string(sealed), secret) {
			t.Fatalf("encrypted export leaked secret %q", secret)
		}
	}
	if strings.Contains(string(sealed), "vnc.example.com") || strings.Contains(string(sealed), "rdp.example.com") {
		t.Fatal("encrypted export leaked host addresses")
	}

	// Import into a fresh instance: prompts for password, rejects wrong one, accepts right one.
	importOptions := freshOptions(t)
	importOptions.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{exportPath}, nil }
	imported := New(importOptions)
	t.Cleanup(imported.Close)

	imported.Click(hitImportHostCatalog)
	if imported.modal != modalForm {
		t.Fatalf("encrypted import did not ask for password: modal=%v", imported.modal)
	}
	fillForm(imported, "wrong-password")
	imported.Click(hitFormSubmit)
	if imported.modal != modalForm || imported.form.dialogError != imported.messages.Tools.BackupErrWrong {
		t.Fatalf("wrong password error = %q, modal=%v", imported.form.dialogError, imported.modal)
	}
	if len(imported.hosts) != 0 {
		t.Fatalf("wrong password modified hosts: %+v", imported.hosts)
	}

	imported.form.set(0, "export-pass-123")
	imported.Click(hitFormSubmit)
	if imported.modal != modalNone {
		t.Fatalf("valid password import failed: %q", imported.form.dialogError)
	}
	if len(imported.hosts) != len(state.hosts) {
		t.Fatalf("imported hosts = %d, want %d", len(imported.hosts), len(state.hosts))
	}
	if _, ok := imported.credentialByID("cred_shared"); !ok {
		t.Fatal("shared credential was not imported")
	}
	if _, ok := imported.proxyProfileByID("prof_corp"); !ok {
		t.Fatal("shared proxy profile was not imported")
	}
	for name, want := range wantSecrets {
		got, found, err := imported.secrets.vault.Get(name)
		if err != nil || !found || got != want {
			t.Fatalf("vault secret %q = (%q, %v, %v), want %q", name, got, found, err, want)
		}
	}

	// Verify persistence across restart.
	restarted := newTestState(t, importOptions)
	for name, want := range wantSecrets {
		got, found, err := restarted.secrets.vault.Get(name)
		if err != nil || !found || got != want {
			t.Fatalf("after restart secret %q = (%q, %v, %v), want %q", name, got, found, err, want)
		}
	}
}

func TestHostCatalogEncryptedImportRemapsCollidingCatalogIDsAndExportsSubset(t *testing.T) {
	exportPath := filepath.Join(t.TempDir(), "subset-encrypted.json")
	options := testOptions(t)
	options.SaveFile = func(app.SaveFileOptions) (string, error) { return exportPath, nil }
	state := newTestState(t, options)
	state.hosts[1].Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "127.0.0.1:1080", user: "u"}
	state.save()
	wantPW := "staging-pw"
	wantProxyPW := "staging-proxy-pw"
	if err := state.secrets.vault.Set(hostSecretName(state.hosts[1].CatalogID, hostPasswordPart), wantPW); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.Set(hostProxyPasswordSecret(state.hosts[1].CatalogID, state.hosts[1].Proxy), wantProxyPW); err != nil {
		t.Fatal(err)
	}
	// A host that is not part of the export keeps its password to itself.
	if err := state.secrets.vault.Set(hostSecretName(state.hosts[0].CatalogID, hostPasswordPart), "unselected-pw"); err != nil {
		t.Fatal(err)
	}

	// Export only the checked host (hosts[1]).
	state.Click(hitHostCheckList.At(int(state.hosts[1].ID)))
	state.Click(hitExportHosts)
	fillForm(state, "subset-pass", "subset-pass")
	state.Click(hitFormSubmit)
	if state.modal != modalNone {
		t.Fatalf("subset encrypted export failed: %q", state.form.dialogError)
	}
	sealed, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := backup.OpenEnvelope(hostCatalogEncryptedFormat, hostCatalogEncryptedVersion, sealed, "subset-pass")
	if err != nil {
		t.Fatal(err)
	}
	var exported hostCatalogDocument
	if err := json.Unmarshal(plain, &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported.Hosts) != 1 || exported.Hosts[0].ID != state.hosts[1].CatalogID {
		t.Fatalf("exported hosts = %+v, want only %q", exported.Hosts, state.hosts[1].CatalogID)
	}
	own, ownProxy := "host:"+state.hosts[1].CatalogID+":", hostProxyPasswordPrefix+state.hosts[1].CatalogID+":"
	for _, secret := range exported.Secrets {
		if !strings.HasPrefix(secret.Name, own) && !strings.HasPrefix(secret.Name, ownProxy) {
			t.Errorf("the file holds a secret that is not the exported host's: %q", secret.Name)
		}
	}
	if strings.Contains(string(sealed), "unselected-pw") {
		t.Fatal("the encrypted export leaked a password")
	}

	// Target already has a DIFFERENT host occupying the same CatalogID.
	targetOptions := freshOptions(t)
	targetOptions.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{exportPath}, nil }
	target := New(targetOptions)
	t.Cleanup(target.Close)
	occupier := Host{
		ID: target.nextHostID, CatalogID: state.hosts[1].CatalogID, Label: "Different Host",
		Address: "other.example.com", User: "root", Port: 22,
	}
	occupier.finish()
	target.nextHostID++
	target.hosts = append(target.hosts, occupier)
	target.hostDataChanged()
	if err := target.secrets.vault.Set(hostSecretName(occupier.CatalogID, hostPasswordPart), "occupier-pw"); err != nil {
		t.Fatal(err)
	}

	target.Click(hitImportHostCatalog)
	fillForm(target, "subset-pass")
	target.Click(hitFormSubmit)
	if target.modal != modalNone {
		t.Fatalf("import failed: %q", target.form.dialogError)
	}
	if len(target.hosts) != 2 {
		t.Fatalf("hosts = %d, want 2", len(target.hosts))
	}
	importedHost := target.hosts[1]
	if importedHost.CatalogID == occupier.CatalogID {
		t.Fatalf("colliding CatalogID was not remapped: %q", importedHost.CatalogID)
	}
	if got, found, err := target.secrets.vault.Get(hostSecretName(importedHost.CatalogID, hostPasswordPart)); err != nil || !found || got != wantPW {
		t.Fatalf("remapped host password = (%q, %v, %v), want %q", got, found, err, wantPW)
	}
	if got, found, err := target.secrets.vault.Get(hostProxyPasswordSecret(importedHost.CatalogID, importedHost.Proxy)); err != nil || !found || got != wantProxyPW {
		t.Fatalf("remapped proxy password = (%q, %v, %v), want %q", got, found, err, wantProxyPW)
	}
	if got, found, err := target.secrets.vault.Get(hostSecretName(occupier.CatalogID, hostPasswordPart)); err != nil || !found || got != "occupier-pw" {
		t.Fatalf("occupier password was overwritten: (%q, %v, %v)", got, found, err)
	}
}

// The two exports are not mistaken for each other on disk: they are offered
// under different names, and the import's picker still lists both.
func TestHostCatalogPlainAndEncryptedExportsAreOfferedUnderDifferentNames(t *testing.T) {
	var offered []app.SaveFileOptions
	options := testOptions(t)
	options.SaveFile = func(got app.SaveFileOptions) (string, error) {
		offered = append(offered, got)
		return filepath.Join(t.TempDir(), "catalog.json"), nil
	}
	state := newTestState(t, options)
	state.Click(hitExportHosts)
	state.Click(hitFormBack) // without passwords
	state.Click(hitExportHosts)
	fillForm(state, testBackupPassword, testBackupPassword)
	state.Click(hitFormSubmit)
	if state.modal != modalNone || len(offered) != 2 {
		t.Fatalf("modal %d, save dialogs %d, error %q", state.modal, len(offered), state.form.dialogError)
	}
	plain, encrypted := offered[0], offered[1]
	if plain.SuggestedName == encrypted.SuggestedName || filepath.Ext(plain.SuggestedName) != filepath.Ext(encrypted.SuggestedName) {
		t.Fatalf("plain export %q, encrypted export %q: not told apart, or not the same kind of file", plain.SuggestedName, encrypted.SuggestedName)
	}

	var picker app.OpenFileOptions
	importing := newTestState(t, freshOptions(t))
	importing.options.OpenFiles = func(got app.OpenFileOptions) ([]string, error) { picker = got; return nil, app.ErrCanceled }
	importing.Click(hitImportHostCatalog)
	for _, name := range []string{plain.SuggestedName, encrypted.SuggestedName} {
		listed := false
		for _, filter := range picker.Filters {
			if matched, _ := filepath.Match(filter.Pattern, name); matched {
				listed = true
			}
		}
		if !listed {
			t.Errorf("the import picker's filters %+v do not list %q", picker.Filters, name)
		}
	}
}

func TestHostCatalogPlainImportIgnoresInjectedSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain-with-secrets.json")
	payload := `{"format":"motty-host-catalog","version":4,"hosts":[{"id":"host_injected","label":"Injected","address":"inj.example.com","port":22}],"secrets":[{"name":"host:host_injected:password","value":"injected-pw"}]}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := New(options)
	t.Cleanup(state.Close)
	state.Click(hitImportHostCatalog)
	if len(state.hosts) != 1 {
		t.Fatalf("hosts = %d, want 1", len(state.hosts))
	}
	if _, found, err := state.secrets.vault.Get(hostSecretName("host_injected", hostPasswordPart)); err != nil || found {
		t.Fatalf("plain import should ignore secrets: found=%v err=%v", found, err)
	}
}

func TestHostCatalogExportDialogButtonsFitAcrossLanguages(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		t.Run(lang.String(), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.setLanguage(lang)
			state.Click(hitExportHosts)
			if state.modal != modalForm {
				t.Fatalf("modal = %v, want modalForm", state.modal)
			}
			canvas, _ := paintedTexts(state)
			var dialogRect gfx.Rect
			for _, hit := range canvas.Hits() {
				if hit.Block {
					dialogRect = hit.Bounds
				}
			}
			m := state.messages
			var prevRight float32
			for index, label := range []string{m.Cancel, m.HostCatalogExportPlain, m.HostCatalogExportEncrypted} {
				hit, ok := hitByLabel(canvas, label)
				if !ok {
					t.Fatalf("button %q is not painted", label)
				}
				if index > 0 && hit.Bounds.X < prevRight {
					t.Fatalf("button %q overlaps previous button (x=%.1f < prevRight=%.1f)", label, hit.Bounds.X, prevRight)
				}
				if hit.Bounds.X+hit.Bounds.Width > dialogRect.X+dialogRect.Width {
					t.Fatalf("button %q overflows dialog (right=%.1f > dialogRight=%.1f)", label, hit.Bounds.X+hit.Bounds.Width, dialogRect.X+dialogRect.Width)
				}
				prevRight = hit.Bounds.X + hit.Bounds.Width
			}
		})
	}
}

func TestHostCatalogTreeSteadyPaintIsAllocationFree(t *testing.T) {
	state := newTestState(t, testOptions(t))
	setHostTags(&state.hosts[0], []string{"prod", "critical"})
	state.hosts[0].Icon = hostIconTerminal
	state.hostDataChanged()
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("steady host catalog paint allocated %v times", allocations)
	}
}

func BenchmarkPaintHostCatalogTree(b *testing.B) {
	state := newTestState(b, testOptions(b))
	setHostTags(&state.hosts[0], []string{"prod", "critical"})
	state.hosts[0].Icon = hostIconTerminal
	state.hostDataChanged()
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ResetTimer()
	b.ReportMetric(float64(unsafe.Sizeof(Host{})), "bytes/host")
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

// A host can be given a distribution's badge by hand, the choice survives a restart,
// and editing the host again starts from it instead of from the automatic icon.
func TestHostIconCanBeASystemBadgeAndSurvivesEditingAndRestart(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitHostList.At(int(state.hosts[0].ID)))
	clickHostAction(state, hitEditHost)
	chooseHostIcon(t, state, "Ubuntu")
	state.Click(hitDialogSubmit)
	if got := state.hosts[0].Icon; got != hostIconOf(sshconn.SystemUbuntu) {
		t.Fatalf("icon after choosing Ubuntu = %v", got)
	}
	clickHostAction(state, hitEditHost)
	if state.editor.icon != hostIconOf(sshconn.SystemUbuntu) {
		t.Fatalf("editing the host started from icon %v, want the one it has", state.editor.icon)
	}
	restarted := newTestState(t, options)
	if got := restarted.hosts[0].Icon; got != hostIconOf(sshconn.SystemUbuntu) || got.String() != "ubuntu" {
		t.Fatalf("restored icon = %v (%q)", got, got.String())
	}
}

// chooseHostIcon opens the icon select and picks the option named name.
func chooseHostIcon(t *testing.T, state *State, name string) {
	t.Helper()
	chooseSelectOption(t, state, state.messages.FieldIcon, name)
}

// The icon field is a fixed list of choices: a choice marks the icon as chosen,
// and there is no way to invent one.
func TestHostIconPickerChoosesAndNeverCreates(t *testing.T) {
	state := newTestState(t, testOptions(t))
	clickHostAction(state, hitAddHost)
	chooseHostIcon(t, state, "Linux Mint")
	if state.editor.icon != hostIconOf(sshconn.SystemMint) || !state.editor.iconChosen {
		t.Fatalf("after choosing Linux Mint: icon %v chosen %v", state.editor.icon, state.editor.iconChosen)
	}
}

// With a secret that can be removed the row has three buttons; the
// code field must keep room to show what it says, so the scan and paste buttons
// go on a row of their own under it.
func TestOTPFieldKeepsItsWidthWhateverButtonsItHas(t *testing.T) {
	for _, saved := range []bool{false, true} {
		state := newTestState(t, testOptions(t))
		clickHostAction(state, hitAddHost)
		state.Click(hitDialogAdvanced)
		state.editor.otpSaved = saved
		canvas, _ := paintedTexts(state)
		state.editor.form.scrollTo(100000)
		canvas, _ = paintedTexts(state)
		// The field's label shares its identity, so take the input: the taller one.
		var field gfx.Hit
		for _, hit := range canvas.Hits() {
			if hit.ID == hitDialogOTP && hit.Bounds.Height > field.Bounds.Height {
				field = hit
			}
		}
		if field.Bounds.Width < 250 {
			t.Fatalf("saved=%v: the code field is %v wide, too narrow for its placeholder", saved, field.Bounds.Width)
		}
		scan, _ := hitByID(canvas, hitDialogOTPScan)
		if scan.Bounds.Y <= field.Bounds.Y+field.Bounds.Height-1 {
			t.Fatalf("saved=%v: scan %+v should sit under the field %+v", saved, scan.Bounds, field.Bounds)
		}
		for _, id := range []gfx.ID{hitDialogOTPScan, hitDialogOTPPaste, hitDialogForwardX11, hitDialogNoHistory, hitDialogAgentAccess} {
			hit, found := hitByID(canvas, id)
			if !found || hit.Bounds.Y+hit.Bounds.Height > float32(state.windowRect().Height) {
				t.Fatalf("saved=%v: control %v is missing or off screen: %+v", saved, id, hit)
			}
		}
		x11, _ := hitByID(canvas, hitDialogForwardX11)
		history, _ := hitByID(canvas, hitDialogNoHistory)
		if x11.Bounds.Y+x11.Bounds.Height > history.Bounds.Y {
			t.Fatalf("saved=%v: the two switches overlap: %+v and %+v", saved, x11.Bounds, history.Bounds)
		}
		access, _ := hitByID(canvas, hitDialogAgentAccess)
		if history.Bounds.Y+history.Bounds.Height > access.Bounds.Y {
			t.Fatalf("saved=%v: the agent switch overlaps the one above it: %+v and %+v", saved, history.Bounds, access.Bounds)
		}
	}
}

// The card view shows a host's tags, as the list does: they are one of the
// things a host is found by.
func TestHostCardsShowTheirTags(t *testing.T) {
	state := newTestState(t, testOptions(t))
	setHostTags(&state.hosts[0], []string{"prod", "web"})
	state.hostView = hostViewGrid
	_, texts := paintedTexts(state)
	found := map[string]bool{}
	for _, text := range texts {
		found[text] = true
	}
	if !found["prod"] || !found["web"] {
		t.Fatalf("the card does not show the host's tags; texts: %v", texts)
	}
}

func TestAutomaticHostIconsFollowTheSystemAndProtocol(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if art := state.hostIconArt(hostIconAuto, sshconn.SystemUnknown, protocolVNC); !reflect.DeepEqual(art, icons.Monitor) {
		t.Fatal("an automatic icon for a VNC host is not the monitor")
	}
	if art := state.hostIconArt(hostIconAuto, sshconn.SystemUnknown, protocolSSH); !reflect.DeepEqual(art, icons.Server) {
		t.Fatal("an automatic icon for an SSH host with no detected system is not the server")
	}
	if art := state.hostIconArt(hostIconAuto, sshconn.SystemWindows, protocolRDP); reflect.DeepEqual(art, icons.Monitor) {
		t.Fatal("a known system lost its badge on a desktop host")
	}
	// An RDP host is a Windows host before anything is detected: its account is
	// the Windows user's, so its automatic icon is the Windows badge, not the
	// generic desktop glyph.
	windows := state.hostIconArt(hostIconOf(sshconn.SystemWindows), sshconn.SystemUnknown, protocolRDP)
	if art := state.hostIconArt(hostIconAuto, sshconn.SystemUnknown, protocolRDP); !reflect.DeepEqual(art, windows) {
		t.Fatal("an automatic icon for an RDP host is not the Windows badge")
	}
	if art := state.hostIconArt(hostIconAuto, sshconn.SystemDebian, protocolRDP); reflect.DeepEqual(art, windows) {
		t.Fatal("a detected system lost its badge on an RDP host")
	}
	if art := state.hostIconArt(hostIconDesktop, sshconn.SystemUnknown, protocolSSH); !reflect.DeepEqual(art, icons.Monitor) {
		t.Fatal("the explicit desktop icon is not the monitor")
	}
	if art := state.hostIconArt(hostIconSerial, sshconn.SystemUnknown, protocolSSH); !reflect.DeepEqual(art, icons.Serial) {
		t.Fatal("the explicit serial icon is not the serial glyph")
	}
	icon, ok := parseHostIcon("desktop")
	if !ok || icon != hostIconDesktop || icon.String() != "desktop" {
		t.Fatalf("desktop icon = %v, %v", icon, ok)
	}
	serial, ok := parseHostIcon("serial")
	if !ok || serial != hostIconSerial || serial.String() != "serial" {
		t.Fatalf("serial icon = %v, %v", serial, ok)
	}
	offered := false
	for _, choice := range hostIconChoices() {
		if choice == hostIconSerial {
			offered = true
		}
	}
	if !offered {
		t.Fatal("the icon list does not offer serial")
	}
}

// Switching the form to a serial connection selects the serial icon, until the
// user picks one of their own.
func TestSerialProtocolDefaultsTheHostIcon(t *testing.T) {
	state := newTestState(t, testOptions(t))
	editor := &state.editor
	editor.reset()
	editor.setProtocol(protocolSerial)
	if editor.icon != hostIconSerial {
		t.Fatalf("serial form icon = %v, want serial", editor.icon)
	}
	editor.setProtocol(protocolSSH)
	if editor.icon != hostIconAuto {
		t.Fatalf("SSH form kept the serial icon: %v", editor.icon)
	}
	editor.setProtocol(protocolSerial)
	editor.icon, editor.iconChosen = hostIconGlobe, true
	editor.setProtocol(protocolSSH)
	if editor.icon != hostIconGlobe {
		t.Fatalf("an explicit icon changed with the protocol: %v", editor.icon)
	}
}

func TestSelectedGridCardKeepsItsIconBadgeVisible(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if fill := state.hostBadgeFill(state.pal.accentSoft); fill == state.pal.accentSoft {
		t.Fatal("a sequential host icon badge disappears on the selected card")
	}
	if fill := state.hostBadgeFill(state.pal.surface); fill != state.pal.accentSoft {
		t.Fatalf("an unselected card changed its badge fill to %v", fill)
	}
}
