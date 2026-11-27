package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	"motty/internal/vault"
)

func openCopyInfoSubmenu(t testing.TB, state *State) *gfx.Canvas {
	t.Helper()
	clickPageMenuRow(t, state, state.messages.CopyInfo)
	size := state.viewport
	if size.Width == 0 {
		size = gfx.Size{Width: 1400, Height: 900}
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	return &canvas
}

func chooseCopyInfoSubmenu(t testing.TB, state *State, label string) {
	t.Helper()
	clickPageMenuRow(t, state, state.messages.CopyInfo)
	clickPageMenuRow(t, state, label)
}

func submenuRowByLabel(canvas *gfx.Canvas, label string) (gfx.Hit, bool) {
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleMenuItem && hit.Label == label {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

func TestCopyInfoSubmenuOpensToTheRightOnTabAndHostMenus(t *testing.T) {
	state := newTestState(t, testOptions(t))
	target := state.hosts[1]
	secretName, ok := hostPasswordSecretName(target)
	if !ok {
		t.Fatal("default SSH host has no password secret name")
	}
	if err := state.secrets.vault.Set(secretName, "s3cret-pass"); err != nil {
		t.Fatal(err)
	}

	assertSubmenu := func(name string, openMenu func(), topLevelLabels []string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			openMenu()
			canvas := openCopyInfoSubmenu(t, state)
			trigger, ok := submenuRowByLabel(canvas, state.messages.CopyInfo)
			if !ok {
				t.Fatal("Copy Info trigger row missing")
			}
			firstSub, ok := submenuRowByLabel(canvas, state.messages.CopyUsername)
			if !ok {
				t.Fatal("Copy Username submenu row did not open")
			}
			if firstSub.Bounds.X < trigger.Bounds.X+trigger.Bounds.Width-2 {
				t.Fatalf("submenu opened at X=%.1f, expected to the right of trigger [%.1f..%.1f]",
					firstSub.Bounds.X, trigger.Bounds.X, trigger.Bounds.X+trigger.Bounds.Width)
			}
			// Top-level quick copy actions remain on the parent menu.
			for _, label := range topLevelLabels {
				row, found := submenuRowByLabel(canvas, label)
				if !found || row.Disabled || row.Bounds.X >= trigger.Bounds.X+trigger.Bounds.Width-2 {
					t.Errorf("top-level row %q: found=%v disabled=%v bounds=%+v, want enabled on parent menu", label, found, row.Disabled, row.Bounds)
				}
			}
			// Submenu holds only the new username, credential, and formatted info items.
			for _, label := range []string{
				state.messages.CopyUsername,
				state.messages.CopyCredential,
				state.messages.CopyConnectionInfo,
				state.messages.CopyConnectionInfoWithCredentials,
			} {
				row, found := submenuRowByLabel(canvas, label)
				if !found || row.Disabled || row.Bounds.X < trigger.Bounds.X+trigger.Bounds.Width-2 {
					t.Errorf("submenu row %q: found=%v disabled=%v bounds=%+v, want enabled in submenu", label, found, row.Disabled, row.Bounds)
				}
			}
		})
	}

	assertSubmenu("host menu", func() {
		rightClickHost(state, target.ID)
	}, []string{state.messages.CopyAddressAndPort, state.messages.CopyConnectionName})

	openHost(state)
	state.newHostSession(target)
	workspace := state.deck.ActiveWorkspace()
	assertSubmenu("tab menu", func() {
		rightClickTab(state, hitTabList.At(int(workspace)))
	}, []string{state.messages.CopyHostAddress, state.messages.CopyAddressAndPort, state.messages.CopyConnectionName})
}

func TestCopyInfoSubmenuCopiesAllFieldsAndConfirmsCredentialsFromHostAndTabMenus(t *testing.T) {
	state := newTestState(t, testOptions(t))
	useChinese(t, state)
	target := state.hosts[1]
	secretName, _ := hostPasswordSecretName(target)
	if err := state.secrets.vault.Set(secretName, "P@ssw0rd!99"); err != nil {
		t.Fatal(err)
	}
	var copied string
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	state.options.ClipboardText = func() (string, error) {
		return copied, nil
	}

	wantUser := state.login(target)
	wantInfo := strings.Join([]string{
		fmt.Sprintf("%s: %s", state.messages.CopyInfoFieldName, target.Label),
		fmt.Sprintf("%s: SSH", state.messages.CopyInfoFieldProtocol),
		fmt.Sprintf("%s: %s", state.messages.FieldAddress, target.Address),
		fmt.Sprintf("%s: %d", state.messages.FieldPort, target.Port),
		fmt.Sprintf("%s: %s", state.messages.FieldUser, wantUser),
	}, "\n")
	wantInfoWithCreds := wantInfo + "\n" + fmt.Sprintf("%s: P@ssw0rd!99", state.messages.FieldPassword)

	cases := []struct {
		label       string
		want        string
		needsPrompt bool
	}{
		{state.messages.CopyUsername, wantUser, false},
		{state.messages.CopyCredential, "P@ssw0rd!99", true},
		{state.messages.CopyConnectionInfo, wantInfo, false},
		{state.messages.CopyConnectionInfoWithCredentials, wantInfoWithCreds, true},
	}

	runCases := func(source string, openMenu func(), isOpen func() bool) {
		t.Helper()
		for _, tc := range cases {
			copied = ""
			openMenu()
			chooseCopyInfoSubmenu(t, state, tc.label)
			if isOpen() {
				t.Fatalf("%s stayed open after %q", source, tc.label)
			}
			if tc.needsPrompt {
				if state.modal != modalCopyCredentialConfirm || copied != "" {
					t.Fatalf("%s %q: modal=%v copied=%q, want confirmation before copying secret", source, tc.label, state.modal, copied)
				}
				state.Click(hitCopyCredentialConfirmOK)
			}
			if state.modal != modalNone || copied != tc.want {
				t.Fatalf("%s %q: modal=%v copied:\n%s\nwant:\n%s", source, tc.label, state.modal, copied, tc.want)
			}
		}
	}

	runCases("host menu", func() {
		rightClickHost(state, target.ID)
	}, func() bool { return state.hostMenu.open })

	state.newHostSession(target)
	workspace := state.deck.ActiveWorkspace()
	runCases("tab menu", func() {
		rightClickTab(state, hitTabList.At(int(workspace)))
	}, func() bool { return state.tabMenu.open })
}

func TestCopyCredentialConfirmCanBeCanceledAndConfirmedByKeyboard(t *testing.T) {
	state := newTestState(t, testOptions(t))
	target := state.hosts[1]
	secretName, _ := hostPasswordSecretName(target)
	if err := state.secrets.vault.Set(secretName, "top-secret"); err != nil {
		t.Fatal(err)
	}
	var copied string
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}

	// Cancel via Cancel button.
	rightClickHost(state, target.ID)
	chooseCopyInfoSubmenu(t, state, state.messages.CopyCredential)
	if state.modal != modalCopyCredentialConfirm {
		t.Fatalf("modal = %v, want modalCopyCredentialConfirm", state.modal)
	}
	state.Click(hitCopyCredentialConfirmCancel)
	if state.modal != modalNone || copied != "" {
		t.Fatalf("after cancel: modal=%v copied=%q", state.modal, copied)
	}

	// Cancel via Escape.
	rightClickHost(state, target.ID)
	chooseCopyInfoSubmenu(t, state, state.messages.CopyCredential)
	result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	if !result.Handled || state.modal != modalNone || copied != "" {
		t.Fatalf("after Escape: handled=%v modal=%v copied=%q", result.Handled, state.modal, copied)
	}

	// Confirm via Enter when confirm button is focused.
	rightClickHost(state, target.ID)
	chooseCopyInfoSubmenu(t, state, state.messages.CopyCredential)
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitCopyCredentialConfirmOK})
	if !result.Handled || state.modal != modalNone || copied != "top-secret" {
		t.Fatalf("after Enter on confirm: handled=%v modal=%v copied=%q", result.Handled, state.modal, copied)
	}
}

func TestCopiedCredentialAutoClearsClipboardOnlyWhenUnchanged(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.secretClipboardTTL = 25 * time.Millisecond
	target := state.hosts[1]
	secretName, _ := hostPasswordSecretName(target)
	if err := state.secrets.vault.Set(secretName, "ephemeral-secret"); err != nil {
		t.Fatal(err)
	}

	clipboard := ""
	state.options.SetClipboardText = func(value string) error {
		clipboard = value
		return nil
	}
	state.options.ClipboardText = func() (string, error) {
		return clipboard, nil
	}

	// 1. Unchanged clipboard is cleared automatically once TTL elapses.
	rightClickHost(state, target.ID)
	chooseCopyInfoSubmenu(t, state, state.messages.CopyCredential)
	state.Click(hitCopyCredentialConfirmOK)
	if clipboard != "ephemeral-secret" {
		t.Fatalf("clipboard = %q, want copied secret", clipboard)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if clipboard == "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if clipboard != "" || state.status != state.messages.CopyCredentialCleared {
		t.Fatalf("after TTL: clipboard=%q status=%q, want cleared clipboard and status", clipboard, state.status)
	}

	// 2. If another app replaced the clipboard before TTL expires, Motty leaves it untouched.
	rightClickHost(state, target.ID)
	chooseCopyInfoSubmenu(t, state, state.messages.CopyCredential)
	state.Click(hitCopyCredentialConfirmOK)
	if clipboard != "ephemeral-secret" {
		t.Fatalf("second copy clipboard = %q", clipboard)
	}
	clipboard = "user copied something else"
	time.Sleep(60 * time.Millisecond)
	state.Wake()
	if clipboard != "user copied something else" {
		t.Fatalf("timer overwrote newer clipboard content: %q", clipboard)
	}

	// 3. A subsequent non-sensitive copy cancels the pending secret clear timer.
	rightClickHost(state, target.ID)
	chooseCopyInfoSubmenu(t, state, state.messages.CopyCredential)
	state.Click(hitCopyCredentialConfirmOK)
	rightClickHost(state, target.ID)
	chooseMenu(t, state, hitHostMenuName)
	if clipboard != target.Label {
		t.Fatalf("non-sensitive copy clipboard = %q, want %q", clipboard, target.Label)
	}
	time.Sleep(60 * time.Millisecond)
	state.Wake()
	if clipboard != target.Label {
		t.Fatalf("canceled timer cleared non-sensitive copy: %q", clipboard)
	}
}

func TestCopyInfoSubmenuResolvesSharedCredentialAndReportsMissingSecretsOnClick(t *testing.T) {
	state := newTestState(t, testOptions(t))
	cred := credential{ID: "cred_shared_ops", Name: "Shared Ops", User: "ops-shared"}
	state.credentials = append(state.credentials, cred)
	if err := state.secrets.vault.Set(credentialSecretName(cred.ID), "shared-secret-pw"); err != nil {
		t.Fatal(err)
	}

	// Host using a shared credential with empty host.User inherits both user and password.
	sharedHost := state.hosts[1]
	sharedHost.User = ""
	sharedHost.Credential = cred.ID
	state.hosts[1] = sharedHost

	var copied string
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	rightClickHost(state, sharedHost.ID)
	chooseCopyInfoSubmenu(t, state, state.messages.CopyConnectionInfoWithCredentials)
	state.Click(hitCopyCredentialConfirmOK)
	if !strings.Contains(copied, "User: ops-shared") || !strings.Contains(copied, "Password: shared-secret-pw") {
		t.Fatalf("shared credential copy = %q, want inherited user and shared password", copied)
	}

	// Host with no saved password when the vault holds other secrets:
	// submenu stays enabled without probing the vault, and reports missing credential on click+confirm.
	copied = ""
	plainHost := state.hosts[0]
	rightClickHost(state, plainHost.ID)
	chooseCopyInfoSubmenu(t, state, state.messages.CopyCredential)
	state.Click(hitCopyCredentialConfirmOK)
	if copied != "" || state.status != state.messages.CopyCredentialMissing {
		t.Fatalf("missing password on click: copied=%q status=%q, want empty clipboard and missing notice", copied, state.status)
	}

	// With an empty vault and no pinned key, credential actions are disabled purely from vault.Len()==0.
	emptyState := newTestState(t, testOptions(t))
	rightClickHost(emptyState, emptyState.hosts[0].ID)
	canvas := openCopyInfoSubmenu(t, emptyState)
	if row, ok := submenuRowByLabel(canvas, emptyState.messages.CopyCredential); !ok || !row.Disabled {
		t.Fatalf("CopyCredential with empty vault: found=%v disabled=%v, want disabled", ok, row.Disabled)
	}
	if row, ok := submenuRowByLabel(canvas, emptyState.messages.CopyConnectionInfoWithCredentials); !ok || !row.Disabled {
		t.Fatalf("CopyConnectionInfoWithCredentials with empty vault: found=%v disabled=%v, want disabled", ok, row.Disabled)
	}
	if row, ok := submenuRowByLabel(canvas, emptyState.messages.CopyConnectionInfo); !ok || row.Disabled {
		t.Fatalf("CopyConnectionInfo with empty vault: found=%v disabled=%v, want enabled", ok, row.Disabled)
	}
}

func TestCopyInfoSubmenuHandlesProtocolsAndPrivateKeyCredentials(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setLanguage(i18n.English)

	// VNC password-only host: no username fabricated from systemUser.
	vnc := Host{ID: 901, CatalogID: "cat-vnc", Label: "Lab VNC", Protocol: protocolVNC, Address: "10.0.0.8", Port: 5900}
	state.hosts = append(state.hosts, vnc)
	vncSecret, _ := hostPasswordSecretName(vnc)
	if err := state.secrets.vault.Set(vncSecret, "vnc-pass"); err != nil {
		t.Fatal(err)
	}
	var copied string
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	rightClickHost(state, vnc.ID)
	canvas := openCopyInfoSubmenu(t, state)
	if row, ok := submenuRowByLabel(canvas, state.messages.CopyUsername); !ok || !row.Disabled {
		t.Fatalf("VNC password-only CopyUsername: found=%v disabled=%v, want disabled", ok, row.Disabled)
	}
	clickPageMenuRow(t, state, state.messages.CopyConnectionInfoWithCredentials)
	state.Click(hitCopyCredentialConfirmOK)
	if strings.Contains(copied, "User:") || !strings.Contains(copied, "Protocol: VNC") || !strings.Contains(copied, "Password: vnc-pass") {
		t.Fatalf("VNC copy with credentials = %q", copied)
	}

	// SSH key-auth host with a pinned file-backed private key and saved passphrase:
	// Copy Credential copies the PEM text; Copy Connection Info (with Credentials)
	// includes both the saved passphrase and the PEM text.
	const fakePEM = "-----BEGIN OPENSSH PRIVATE KEY-----\nTEST-KEY-BODY\n-----END OPENSSH PRIVATE KEY-----\n"
	keyPath := filepath.Join(t.TempDir(), "prod_ed25519")
	if err := os.WriteFile(keyPath, []byte(fakePEM), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.Set(passphraseSecretName(keyPath), "key-pass-42"); err != nil {
		t.Fatal(err)
	}
	state.identities = append(state.identities, identityRecord{Name: "prod-ed25519", Fingerprint: "SHA256:prodkey", Path: keyPath})
	keyHost := Host{ID: 902, CatalogID: "cat-key", Label: "Prod Key", Protocol: protocolSSH, Address: "10.0.0.9", Port: 22, User: "deploy", Auth: authPrivateKey, Identity: "SHA256:prodkey"}
	state.hosts = append(state.hosts, keyHost)

	rightClickHost(state, keyHost.ID)
	canvas = openCopyInfoSubmenu(t, state)
	if row, ok := submenuRowByLabel(canvas, state.messages.CopyCredential); !ok || row.Disabled {
		t.Fatalf("key-auth CopyCredential: found=%v disabled=%v, want enabled", ok, row.Disabled)
	}
	if row, ok := submenuRowByLabel(canvas, state.messages.CopyConnectionInfoWithCredentials); !ok || row.Disabled {
		t.Fatalf("key-auth CopyConnectionInfoWithCredentials: found=%v disabled=%v, want enabled", ok, row.Disabled)
	}
	clickPageMenuRow(t, state, state.messages.CopyCredential)
	state.Click(hitCopyCredentialConfirmOK)
	if copied != fakePEM {
		t.Fatalf("key-auth CopyCredential = %q, want PEM %q", copied, fakePEM)
	}

	rightClickHost(state, keyHost.ID)
	chooseCopyInfoSubmenu(t, state, state.messages.CopyConnectionInfoWithCredentials)
	state.Click(hitCopyCredentialConfirmOK)
	if !strings.Contains(copied, "Key passphrase: key-pass-42") || !strings.Contains(copied, "Private key:\n"+strings.TrimRight(fakePEM, "\n")) || strings.Contains(copied, "Password:") {
		t.Fatalf("key-auth copy with credentials = %q", copied)
	}

	// Hardware security key has no copyable private key material.
	state.identities = append(state.identities, identityRecord{Name: "yubikey", Fingerprint: "SHA256:sk", Path: "/tmp/sk", SecurityKey: true})
	skHost := Host{ID: 904, CatalogID: "cat-sk", Label: "SK Host", Protocol: protocolSSH, Address: "10.0.0.10", Port: 22, User: "deploy", Auth: authPrivateKey, Identity: "SHA256:sk"}
	state.hosts = append(state.hosts, skHost)
	rightClickHost(state, skHost.ID)
	canvas = openCopyInfoSubmenu(t, state)
	if row, ok := submenuRowByLabel(canvas, state.messages.CopyCredential); !ok || !row.Disabled {
		t.Fatalf("security-key CopyCredential: found=%v disabled=%v, want disabled", ok, row.Disabled)
	}
	if row, ok := submenuRowByLabel(canvas, state.messages.CopyConnectionInfoWithCredentials); !ok || !row.Disabled {
		t.Fatalf("security-key CopyConnectionInfoWithCredentials: found=%v disabled=%v, want disabled", ok, row.Disabled)
	}

	// WSL host: not treated as an IP or IP:PORT, keeps connection info with distribution.
	wslHost := Host{ID: 903, CatalogID: "cat-wsl", Label: "Ubuntu Dev", Protocol: protocolWSL, Address: "Ubuntu", User: "dev"}
	if got := connectionHostAddress(wslHost); got != "" {
		t.Fatalf("WSL connectionHostAddress = %q, want empty", got)
	}
	if got := connectionAddressAndPort(wslHost); got != "" {
		t.Fatalf("WSL connectionAddressAndPort = %q, want empty", got)
	}
	if got := state.formatConnectionInfo(wslHost, false, resolvedCopyCredential{}); !strings.Contains(got, "Protocol: WSL") || !strings.Contains(got, "Address: Ubuntu") || !strings.Contains(got, "User: dev") {
		t.Fatalf("WSL connection info = %q", got)
	}
}

func TestCopyInfoSubmenuUnlocksLegacyVaultAfterConfirmation(t *testing.T) {
	state := newTestState(t, testOptions(t))
	target := state.hosts[1]
	secretName, _ := hostPasswordSecretName(target)
	if err := state.secrets.vault.Set(secretName, "vault-secret-123"); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.LegacyForTests("master-pass"); err != nil {
		t.Fatal(err)
	}
	var copied string
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}

	rightClickHost(state, target.ID)
	chooseCopyInfoSubmenu(t, state, state.messages.CopyConnectionInfoWithCredentials)
	if state.modal != modalCopyCredentialConfirm || copied != "" {
		t.Fatalf("before confirm: modal=%v copied=%q, want confirmation dialog first", state.modal, copied)
	}
	state.Click(hitCopyCredentialConfirmOK)
	if state.modal != modalVaultUnlock || copied != "" {
		t.Fatalf("locked legacy vault: modal=%v copied=%q, want unlock prompt before copy", state.modal, copied)
	}
	state.secrets.unlock.Insert("master-pass")
	state.submitVaultUnlock()
	if state.modal != modalNone || !strings.Contains(copied, "Password: vault-secret-123") {
		t.Fatalf("after vault unlock: modal=%v copied=%q, want copied credentials", state.modal, copied)
	}
}

func TestCopyInfoSubmenuKeyboardNavigationAndLocalTab(t *testing.T) {
	state := newTestState(t, testOptions(t))
	target := state.hosts[1]
	rightClickHost(state, target.ID)

	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	trigger, ok := submenuRowByLabel(&canvas, state.messages.CopyInfo)
	if !ok {
		t.Fatal("Copy Info row missing on host menu")
	}

	windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyRight, Target: trigger.ID})
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	firstSub, ok := submenuRowByLabel(&canvas, state.messages.CopyUsername)
	if !ok {
		t.Fatal("Right arrow did not open the Copy Info submenu")
	}

	result := windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyLeft, Target: firstSub.ID})
	if result.Focus != trigger.ID {
		t.Fatalf("Left arrow focused %v, want parent Copy Info row %v", result.Focus, trigger.ID)
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	if _, stillOpen := submenuRowByLabel(&canvas, state.messages.CopyUsername); stillOpen {
		t.Fatal("Left arrow left the Copy Info submenu open")
	}
	state.hostMenu.open = false

	// Local terminal tab: top-level Copy Connection Name stays one-click enabled
	// while Copy Info submenu is disabled because local tabs have no remote fields.
	state.openLocalTerminal()
	workspace := state.deck.ActiveWorkspace()
	rightClickTab(state, hitTabList.At(int(workspace)))
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	if hit, ok := hitByID(&canvas, hitTabMenuCopyName); !ok || hit.Disabled {
		t.Fatalf("local tab top-level CopyConnectionName: found=%v disabled=%v, want enabled", ok, hit.Disabled)
	}
	for _, id := range []gfx.ID{hitTabMenuCopyHost, hitTabMenuCopyAddress, hitTabMenuCopyInfo} {
		if hit, ok := hitByID(&canvas, id); !ok || !hit.Disabled {
			t.Errorf("local tab item %d: found=%v disabled=%v, want disabled", id, ok, hit.Disabled)
		}
	}
}

type countingDeniedKeystore struct {
	loads int
	mem   *memKeys
	deny  bool
}

func (k *countingDeniedKeystore) Load(service, account string) ([]byte, error) {
	k.loads++
	if k.deny {
		return nil, vault.ErrKeystoreDenied
	}
	return k.mem.Load(service, account)
}

func (k *countingDeniedKeystore) Store(service, account string, secret []byte) error {
	return k.mem.Store(service, account, secret)
}

func (k *countingDeniedKeystore) Delete(service, account string) error {
	return k.mem.Delete(service, account)
}

// Expanding and repainting the Copy Info submenu must never touch the OS
// keystore; the vault is read only after the user clicks and confirms.
func TestCopyInfoSubmenuDoesNotProbeKeystoreUntilConfirmedClick(t *testing.T) {
	ks := &countingDeniedKeystore{mem: &memKeys{}}
	options := testOptions(t)
	options.Keystore = ks
	state := newTestState(t, options)
	target := state.hosts[1]
	secretName, _ := hostPasswordSecretName(target)
	if err := state.secrets.vault.Set(secretName, "s3cret"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.DropKeyForTests()
	ks.loads = 0
	ks.deny = true

	rightClickHost(state, target.ID)
	openCopyInfoSubmenu(t, state)
	for range 5 {
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	}
	if ks.loads != 0 {
		t.Fatalf("keystore Load calls while submenu open = %d, want 0", ks.loads)
	}

	// Clicking Copy Credential opens confirmation without touching the keystore yet.
	clickPageMenuRow(t, state, state.messages.CopyCredential)
	if state.modal != modalCopyCredentialConfirm || ks.loads != 0 {
		t.Fatalf("after clicking Copy Credential: modal=%v loads=%d, want confirm modal and 0 keystore loads", state.modal, ks.loads)
	}

	// Canceling still never touches the keystore.
	state.Click(hitCopyCredentialConfirmCancel)
	if ks.loads != 0 {
		t.Fatalf("keystore Load calls after cancel = %d, want 0", ks.loads)
	}

	// Only confirming actually reads the vault/keystore.
	rightClickHost(state, target.ID)
	chooseCopyInfoSubmenu(t, state, state.messages.CopyCredential)
	state.Click(hitCopyCredentialConfirmOK)
	if ks.loads != 1 {
		t.Fatalf("keystore Load calls after confirm = %d, want 1", ks.loads)
	}
}
