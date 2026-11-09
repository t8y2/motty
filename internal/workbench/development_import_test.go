//go:build dev

package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/configstore"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

func developmentImportFixture(t *testing.T) (Options, *memKeys) {
	t.Helper()
	return developmentImportFixtureAt(t, 22, "fixture-password")
}

func developmentImportFixtureAt(t *testing.T, port int, password string) (Options, *memKeys) {
	t.Helper()
	dir := t.TempDir()
	store, err := configstore.Open(filepath.Join(dir, "config.db"), 21)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Commit(configstore.Change{
		Put: []configstore.Entry{
			{Kind: kindHost, ID: "release-host", Payload: []byte(fmt.Sprintf(`{"o":1,"d":{"id":"release-host","label":"Release host","address":"127.0.0.1","user":"tester","port":%d,"future":42}}`, port))},
			{Kind: kindSetting, ID: "terminal_scrollback", Payload: []byte(`{"d":10000}`)},
		},
		Local: map[string][]byte{localKey: []byte(`{"workspace":"terminal","sessions":[{"address":"127.0.0.1"}],"sync":{"url":"https://production.invalid","user":"production"}}`)},
	})
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	keys := &memKeys{}
	opened, err := vault.Open(filepath.Join(dir, "vault.json"), keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.Apply([]vault.Change{
		{Name: hostSecretName("release-host", hostPasswordPart), Value: password},
		{Name: backupPasswordSecret, Value: "production-backup-password"},
		{Name: webdavPasswordSecret, Value: "production-login"},
	}); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	options.DevelopmentSource = &DevelopmentSource{Directory: dir, Keys: keys}
	return options, keys
}

func TestDevelopmentImportIsReadOnlyAndRequiresConsent(t *testing.T) {
	options, sourceKeys := developmentImportFixture(t)
	source := options.DevelopmentSource.Directory
	configBefore, err := os.ReadFile(filepath.Join(source, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	vaultBefore, err := os.ReadFile(filepath.Join(source, "vault.json"))
	if err != nil {
		t.Fatal(err)
	}
	loads := sourceKeys.loadCount()
	state := newTestState(t, options)
	if state.modal != modalDevelopmentImport || len(state.hosts) != 0 {
		t.Fatalf("no first-run choice: modal=%d hosts=%d", state.modal, len(state.hosts))
	}
	if sourceKeys.loadCount() != loads {
		t.Fatal("source credentials were accessed before consent")
	}
	state.Click(hitDevelopmentImport)
	if state.modal != modalNone || len(state.hosts) != 1 || state.terminalScrollback != 10000 {
		t.Fatalf("import failed: %s", state.developmentImport.err)
	}
	if state.sync.configured() || len(state.sessions) != 0 {
		t.Fatal("production sync or sessions were copied")
	}
	password, found, err := state.secrets.vault.Get(hostSecretName("release-host", hostPasswordPart))
	if err != nil || !found || password != "fixture-password" {
		t.Fatalf("password was not re-encrypted into destination: found=%t err=%v", found, err)
	}
	for _, key := range []string{backupPasswordSecret, webdavPasswordSecret} {
		if _, found, err := state.secrets.vault.Get(key); err != nil || found {
			t.Fatalf("cloud credential copied: %s, %v", key, err)
		}
	}
	configAfter, err := os.ReadFile(filepath.Join(source, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	vaultAfter, err := os.ReadFile(filepath.Join(source, "vault.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(configBefore, configAfter) || !bytes.Equal(vaultBefore, vaultAfter) {
		t.Fatal("source was modified or upgraded")
	}
	releaseKey, _ := sourceKeys.Load(vault.KeyService, vault.KeyAccount)
	developmentKey, _ := options.Keystore.Load(vault.KeyService, vault.KeyAccount)
	if bytes.Equal(releaseKey, developmentKey) {
		t.Fatal("source encryption key was reused")
	}
	state.Close()
	reopened := newTestState(t, options)
	if reopened.modal == modalDevelopmentImport || len(reopened.hosts) != 1 {
		t.Fatal("import was not remembered after restart")
	}
}

func TestDevelopmentImportDismissalAndExplicitEmptyStart(t *testing.T) {
	options, _ := developmentImportFixture(t)
	state := newTestState(t, options)
	state.Click(hitDevelopmentClose)
	state.Close()
	reopened := newTestState(t, options)
	if reopened.modal != modalDevelopmentImport {
		t.Fatal("closing the prompt silently opted out")
	}
	reopened.Click(hitDevelopmentEmpty)
	reopened.Close()
	empty := newTestState(t, options)
	if empty.modal == modalDevelopmentImport || len(empty.hosts) != 0 {
		t.Fatal("explicit empty choice was not retained")
	}
}

func TestDevelopmentImportDoesNotReplaceExistingOrNewlyAddedData(t *testing.T) {
	options, _ := developmentImportFixture(t)
	state := newTestState(t, options)
	if _, err := state.config.Commit(configstore.Change{Put: []configstore.Entry{
		{Kind: kindHost, ID: "new", Payload: []byte(`{"d":{"id":"new","label":"New host","address":"new.invalid"}}`)},
	}}); err != nil {
		t.Fatal(err)
	}
	state.Click(hitDevelopmentImport)
	if state.modal != modalDevelopmentImport || state.developmentImport.err == "" {
		t.Fatal("a stale dialog overwrote new data")
	}
	state.Close()
	reopened := newTestState(t, options)
	if reopened.modal == modalDevelopmentImport || len(reopened.hosts) != 1 || reopened.hosts[0].CatalogID != "new" {
		t.Fatal("an existing profile was treated as empty")
	}
}

func TestDevelopmentImportSourceFailureCanBeRetriedWithoutPasswords(t *testing.T) {
	options, sourceKeys := developmentImportFixture(t)
	sourceKeys.unavailable = true
	state := newTestState(t, options)
	state.Click(hitDevelopmentImport)
	if state.developmentImport.err == "" || len(state.hosts) != 0 || state.modal != modalDevelopmentImport {
		t.Fatal("source key failure was hidden or caused a partial config import")
	}
	// The user leaves the passwords behind by clicking the checkbox's caption, as in
	// the window: the checkbox writes the choice into the dialog's state itself.
	driver := modalEventDriver(t, state)
	clickCaption(t, driver, state.messages.DevelopmentImportPasswords)
	if state.developmentImport.passwords {
		t.Fatal("the caption did not clear the passwords choice")
	}
	state.Click(hitDevelopmentImport)
	if state.modal != modalNone || len(state.hosts) != 1 || state.secrets.vault.Len() != 0 {
		t.Fatalf("explicit no-password import failed: %s", state.developmentImport.err)
	}
}

func TestDevelopmentImportResumesAfterDestinationKeyFailureWithoutOverwritingHosts(t *testing.T) {
	options, _ := developmentImportFixture(t)
	destinationKeys := &memKeys{unavailable: true}
	options.Keystore = destinationKeys
	state := newTestState(t, options)
	state.Click(hitDevelopmentImport)
	if state.modal != modalDevelopmentImport || !state.developmentImport.recordsImported || state.developmentImport.err == "" || len(state.hosts) != 1 {
		t.Fatalf("partial import was not reported: %+v", state.developmentImport)
	}
	state.hosts[0].Label = "My edit after partial import"
	state.save()
	state.Close()
	destinationKeys.unavailable = false
	reopened := newTestState(t, options)
	if reopened.modal != modalDevelopmentImport || !reopened.developmentImport.recordsImported {
		t.Fatal("unfinished import was forgotten")
	}
	reopened.Click(hitDevelopmentImport)
	if reopened.modal != modalNone || reopened.hosts[0].Label != "My edit after partial import" {
		t.Fatalf("retry replaced current data: %s", reopened.developmentImport.err)
	}
	if _, found, err := reopened.secrets.vault.Get(hostSecretName("release-host", hostPasswordPart)); err != nil || !found {
		t.Fatalf("retry did not import password: %t %v", found, err)
	}
}

func TestDevelopmentImportWaitsForStartupNotice(t *testing.T) {
	options, _ := developmentImportFixture(t)
	options.PreviousCrash = "fixture-crash.log"
	state := newTestState(t, options)
	if state.modal != modalStateNotice {
		t.Fatal("import hid startup notice")
	}
	state.Click(hitStateNoticeOK)
	if state.modal != modalDevelopmentImport {
		t.Fatal("import invitation was lost after startup notice")
	}
}

func TestDevelopmentImportReadOnlyKeysRejectWrites(t *testing.T) {
	keys := importReadOnlyKeys{source: &memKeys{}}
	if err := keys.Store("service", "account", nil); err == nil {
		t.Fatal("source key write allowed")
	}
	if err := keys.Delete("service", "account"); err == nil {
		t.Fatal("source key deletion allowed")
	}
}

// The dialog's geometry is checked in English only: CheckLayout compares the
// width the shared text metrics give a line with the width the platform engine
// laid it out at, and an engine with no CJK font lays a Chinese paragraph out by
// the advance of its missing-glyph boxes, narrower than the one em the metrics
// give a CJK character, so the dialog reports a spill the product does not have.
func TestDevelopmentImportNativeDialog(t *testing.T) {
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 900, Height: 600}} {
		options, sourceKeys := developmentImportFixture(t)
		state := newTestState(t, options)
		if size.Width == 900 {
			sourceKeys.unavailable = true
			state.Click(hitDevelopmentImport)
			sourceKeys.unavailable = false
		}
		view, err := headless.Open(app.Config{
			Width: int(size.Width), Height: int(size.Height), Tree: Tree(),
			Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
		}, nil, headless.Options{Scale: 1})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(view.Close)
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		// Read the text from the view's own paint: a throwaway canvas has no
		// platform text engine, so it would wrap the dialog by the reference
		// metrics and leave those lines for the view to reuse.
		var texts []string
		for _, text := range view.Snapshot().Texts {
			texts = append(texts, text.Value)
		}
		if !contains(texts, state.messages.DevelopmentImportPasswords) {
			t.Fatal("password consent checkbox has no visible description")
		}
		if problems := headless.CheckLayout(view.Snapshot()); len(problems) != 0 {
			t.Fatalf("import dialog layout: %+v", problems)
		}
		if dir := os.Getenv("MOTTY_DEV_IMPORT_SCREENSHOTS"); dir != "" {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("import-%d.png", int(size.Width)))); err != nil {
				t.Fatal(err)
			}
		}
		button, found := hitByIDSnapshot(view.Snapshot().Hits, hitDevelopmentImport)
		if !found {
			t.Fatal("no actionable import button")
		}
		if err := view.Click(button.Bounds.X+button.Bounds.Width/2, button.Bounds.Y+button.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if state.modal != modalNone || len(state.hosts) != 1 {
			t.Fatalf("native click did not import: %s", state.developmentImport.err)
		}
		view.Close()
	}
}

func TestDevelopmentImportRejectsIncompatibleSource(t *testing.T) {
	options, _ := developmentImportFixture(t)
	newer, err := configstore.Open(filepath.Join(options.DevelopmentSource.Directory, "config.db"), configFormat+1)
	if err != nil {
		t.Fatal(err)
	}
	newer.Close()
	state := newTestState(t, options)
	if state.modal != modalDevelopmentImport || state.developmentImport.err == "" {
		t.Fatal("unreadable source was silently ignored")
	}
	if err := state.importDevelopmentData(); !errors.Is(err, configstore.ErrTooNew) {
		t.Fatalf("incompatible source accepted: %v", err)
	}
}

func TestDevelopmentImportLoopbackSSH(t *testing.T) {
	value := os.Getenv("MOTTY_IMPORT_SSH_PORT")
	if value == "" {
		t.Skip("set MOTTY_IMPORT_SSH_PORT to the loopback tools/e2esshd port")
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		t.Fatalf("invalid loopback port %q", value)
	}
	options, _ := developmentImportFixtureAt(t, port, "secret")
	options.Dial = func(config sshconn.Config) Connection { return realConn{sshconn.Dial(config)} }
	state := newTestState(t, options)
	view, err := headless.Open(app.Config{
		Width: 1000, Height: 700, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	button, found := hitByIDSnapshot(view.Snapshot().Hits, hitDevelopmentImport)
	if !found {
		t.Fatal("import action missing")
	}
	if err := view.Click(button.Bounds.X+button.Bounds.Width/2, button.Bounds.Y+button.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone || len(state.hosts) != 1 {
		t.Fatalf("import failed: %s", state.developmentImport.err)
	}
	view.Close()
	for attempt := 0; attempt < 2; attempt++ {
		session := state.newHostSession(state.hosts[0])
		if session == nil {
			t.Fatal("no SSH session")
		}
		waitUntil(t, state, "SSH shell using the imported password", func() bool {
			if session.phase == sshconn.PhaseHostKeyPrompt {
				state.Click(hitHostKeyTrust)
			}
			if state.modal == modalPassword {
				t.Fatal("imported password did not authenticate the SSH session")
			}
			return session.term != nil && session.term.shell != nil
		})
		if session.phase != sshconn.PhaseConnected {
			t.Fatalf("SSH not connected: %v", session.phase)
		}
		waitUntil(t, state, "remote shell output", func() bool {
			return strings.Contains(screenText(state, len(state.sessions)-1), "tester@e2e")
		})
		state.Close()
		if attempt == 0 {
			vault.Forget(filepath.Join(filepath.Dir(state.options.StatePath), "vault.json"))
			state = newTestState(t, options)
			if state.modal == modalDevelopmentImport {
				t.Fatal("completed import offered again after restart")
			}
		}
	}
	opened, err := vault.Open(filepath.Join(filepath.Dir(state.options.StatePath), "vault.json"), options.Keystore)
	if err != nil {
		t.Fatal(err)
	}
	if password, found, err := opened.Get(hostSecretName("release-host", hostPasswordPart)); err != nil || !found || password != "secret" {
		t.Fatalf("imported password not durable: found=%t err=%v", found, err)
	}
}

// clickCaption presses the text beside a checkbox or switch, which is a label of it.
func clickCaption(t *testing.T, driver *app.Driver, caption string) {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.IsLabel && hit.Label == caption {
			if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no caption %q on screen", caption)
}
