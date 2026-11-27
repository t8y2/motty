package workbench

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

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

	assertSubmenu := func(name string, openMenu func()) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			openMenu()
			canvas := openCopyInfoSubmenu(t, state)
			trigger, ok := submenuRowByLabel(canvas, state.messages.CopyInfo)
			if !ok {
				t.Fatal("Copy Info trigger row missing")
			}
			firstSub, ok := submenuRowByLabel(canvas, state.messages.CopyHostAddress)
			if !ok {
				t.Fatal("Copy IP submenu row did not open")
			}
			if firstSub.Bounds.X < trigger.Bounds.X+trigger.Bounds.Width-2 {
				t.Fatalf("submenu opened at X=%.1f, expected to the right of trigger [%.1f..%.1f]",
					firstSub.Bounds.X, trigger.Bounds.X, trigger.Bounds.X+trigger.Bounds.Width)
			}
			for _, label := range []string{
				state.messages.CopyHostAddress,
				state.messages.CopyAddressAndPort,
				state.messages.CopyConnectionName,
				state.messages.CopyUsername,
				state.messages.CopyPassword,
				state.messages.CopyConnectionInfo,
				state.messages.CopyConnectionInfoWithCredentials,
			} {
				row, found := submenuRowByLabel(canvas, label)
				if !found || row.Disabled {
					t.Errorf("submenu row %q: found=%v disabled=%v, want enabled", label, found, row.Disabled)
				}
			}
		})
	}

	assertSubmenu("host menu", func() {
		rightClickHost(state, target.ID)
	})

	openHost(state)
	state.newHostSession(target)
	workspace := state.deck.ActiveWorkspace()
	assertSubmenu("tab menu", func() {
		rightClickTab(state, hitTabList.At(int(workspace)))
	})
}

func TestCopyInfoSubmenuCopiesAllFieldsAndCredentialsFromHostAndTabMenus(t *testing.T) {
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

	wantAddressPort := net.JoinHostPort(target.Address, strconv.Itoa(int(target.Port)))
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
		label string
		want  string
	}{
		{state.messages.CopyHostAddress, target.Address},
		{state.messages.CopyAddressAndPort, wantAddressPort},
		{state.messages.CopyConnectionName, target.Label},
		{state.messages.CopyUsername, wantUser},
		{state.messages.CopyPassword, "P@ssw0rd!99"},
		{state.messages.CopyConnectionInfo, wantInfo},
		{state.messages.CopyConnectionInfoWithCredentials, wantInfoWithCreds},
	}

	for _, tc := range cases {
		copied = ""
		rightClickHost(state, target.ID)
		chooseCopyInfoSubmenu(t, state, tc.label)
		if state.hostMenu.open {
			t.Fatalf("host menu stayed open after %q", tc.label)
		}
		if copied != tc.want {
			t.Fatalf("host menu %q copied:\n%s\nwant:\n%s", tc.label, copied, tc.want)
		}
	}

	state.newHostSession(target)
	workspace := state.deck.ActiveWorkspace()
	for _, tc := range cases {
		copied = ""
		rightClickTab(state, hitTabList.At(int(workspace)))
		chooseCopyInfoSubmenu(t, state, tc.label)
		if state.tabMenu.open {
			t.Fatalf("tab menu stayed open after %q", tc.label)
		}
		if copied != tc.want {
			t.Fatalf("tab menu %q copied:\n%s\nwant:\n%s", tc.label, copied, tc.want)
		}
	}
}

func TestCopyInfoSubmenuResolvesSharedCredentialAndDisablesMissingCredentials(t *testing.T) {
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
	if !strings.Contains(copied, "User: ops-shared") || !strings.Contains(copied, "Password: shared-secret-pw") {
		t.Fatalf("shared credential copy = %q, want inherited user and shared password", copied)
	}

	// Host with no saved password and no pinned key disables Copy Password and
	// Copy Connection Info (with Credentials), while keeping plain Copy Connection Info enabled.
	plainHost := state.hosts[0]
	rightClickHost(state, plainHost.ID)
	canvas := openCopyInfoSubmenu(t, state)
	if row, ok := submenuRowByLabel(canvas, state.messages.CopyPassword); !ok || !row.Disabled {
		t.Fatalf("CopyPassword on passwordless host: found=%v disabled=%v, want disabled", ok, row.Disabled)
	}
	if row, ok := submenuRowByLabel(canvas, state.messages.CopyConnectionInfoWithCredentials); !ok || !row.Disabled {
		t.Fatalf("CopyConnectionInfoWithCredentials on passwordless host: found=%v disabled=%v, want disabled", ok, row.Disabled)
	}
	if row, ok := submenuRowByLabel(canvas, state.messages.CopyConnectionInfo); !ok || row.Disabled {
		t.Fatalf("CopyConnectionInfo on passwordless host: found=%v disabled=%v, want enabled", ok, row.Disabled)
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
	if strings.Contains(copied, "User:") || !strings.Contains(copied, "Protocol: VNC") || !strings.Contains(copied, "Password: vnc-pass") {
		t.Fatalf("VNC copy with credentials = %q", copied)
	}

	// SSH key-auth host with pinned identity: Copy Password disabled, Copy Info (with Credentials) includes key name.
	state.identities = append(state.identities, identityRecord{Name: "prod-ed25519", Fingerprint: "SHA256:prodkey", Path: "/tmp/prod"})
	keyHost := Host{ID: 902, CatalogID: "cat-key", Label: "Prod Key", Protocol: protocolSSH, Address: "10.0.0.9", Port: 22, User: "deploy", Auth: authPrivateKey, Identity: "SHA256:prodkey"}
	state.hosts = append(state.hosts, keyHost)
	rightClickHost(state, keyHost.ID)
	canvas = openCopyInfoSubmenu(t, state)
	if row, ok := submenuRowByLabel(canvas, state.messages.CopyPassword); !ok || !row.Disabled {
		t.Fatalf("key-auth CopyPassword: found=%v disabled=%v, want disabled", ok, row.Disabled)
	}
	if row, ok := submenuRowByLabel(canvas, state.messages.CopyConnectionInfoWithCredentials); !ok || row.Disabled {
		t.Fatalf("key-auth CopyConnectionInfoWithCredentials: found=%v disabled=%v, want enabled", ok, row.Disabled)
	}
	clickPageMenuRow(t, state, state.messages.CopyConnectionInfoWithCredentials)
	if !strings.Contains(copied, "Private key: prod-ed25519") || strings.Contains(copied, "Password:") {
		t.Fatalf("key-auth copy with credentials = %q", copied)
	}

	// WSL host: not treated as an IP or IP:PORT, keeps connection info with distribution.
	wslHost := Host{ID: 903, CatalogID: "cat-wsl", Label: "Ubuntu Dev", Protocol: protocolWSL, Address: "Ubuntu", User: "dev"}
	if got := connectionHostAddress(wslHost); got != "" {
		t.Fatalf("WSL connectionHostAddress = %q, want empty", got)
	}
	if got := connectionAddressAndPort(wslHost); got != "" {
		t.Fatalf("WSL connectionAddressAndPort = %q, want empty", got)
	}
	if got := state.formatConnectionInfo(wslHost, false, ""); !strings.Contains(got, "Protocol: WSL") || !strings.Contains(got, "Address: Ubuntu") || !strings.Contains(got, "User: dev") {
		t.Fatalf("WSL connection info = %q", got)
	}
}

func TestCopyInfoSubmenuUnlocksLegacyVaultBeforeCopyingCredentials(t *testing.T) {
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
	firstSub, ok := submenuRowByLabel(&canvas, state.messages.CopyHostAddress)
	if !ok {
		t.Fatal("Right arrow did not open the Copy Info submenu")
	}

	result := windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyLeft, Target: firstSub.ID})
	if result.Focus != trigger.ID {
		t.Fatalf("Left arrow focused %v, want parent Copy Info row %v", result.Focus, trigger.ID)
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	if _, stillOpen := submenuRowByLabel(&canvas, state.messages.CopyHostAddress); stillOpen {
		t.Fatal("Left arrow left the Copy Info submenu open")
	}
	state.hostMenu.open = false

	// Local terminal tab: Copy Info stays enabled for Copy Connection Name while
	// remote-only entries stay disabled.
	state.openLocalTerminal()
	workspace := state.deck.ActiveWorkspace()
	rightClickTab(state, hitTabList.At(int(workspace)))
	subCanvas := openCopyInfoSubmenu(t, state)
	if row, ok := submenuRowByLabel(subCanvas, state.messages.CopyConnectionName); !ok || row.Disabled {
		t.Fatalf("local tab CopyConnectionName: found=%v disabled=%v, want enabled", ok, row.Disabled)
	}
	for _, label := range []string{
		state.messages.CopyHostAddress,
		state.messages.CopyAddressAndPort,
		state.messages.CopyUsername,
		state.messages.CopyPassword,
		state.messages.CopyConnectionInfo,
		state.messages.CopyConnectionInfoWithCredentials,
	} {
		if row, ok := submenuRowByLabel(subCanvas, label); !ok || !row.Disabled {
			t.Errorf("local tab %q: found=%v disabled=%v, want disabled", label, ok, row.Disabled)
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

// Painting an open Copy Info submenu repeatedly must probe the keystore at most
// once per menu opening so a denied keystore never triggers a loop of OS prompts.
func TestCopyInfoSubmenuProbesKeystoreAtMostOncePerOpen(t *testing.T) {
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
	if ks.loads != 1 {
		t.Fatalf("keystore Load calls across 6 submenu paints = %d, want 1", ks.loads)
	}
}
