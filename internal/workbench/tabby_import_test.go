package workbench

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/ssh"
	"motty/internal/portforward"
	"motty/internal/sshconn"
)

// A Tabby configuration is YAML with a vault blob in it, so these tests write one
// the way Tabby does: PBKDF2-SHA512 over an eight-byte salt with 100000 rounds
// into AES-256-CBC. The parser is tested against Tabby's own output in
// internal/sessionimport; here the point is what the workbench does with what it
// read, so a blob this file made is enough.

// tabbyVaultFor seals contents under passphrase and answers the three strings
// the configuration carries.
func tabbyVaultFor(t *testing.T, contents, passphrase string) (blob, salt, iv string) {
	t.Helper()
	saltBytes := make([]byte, 8)
	ivBytes := make([]byte, aes.BlockSize)
	for _, target := range [][]byte{saltBytes, ivBytes} {
		if _, err := rand.Read(target); err != nil {
			t.Fatal(err)
		}
	}
	block, err := aes.NewCipher(pbkdf2.Key([]byte(passphrase), saltBytes, 100000, 32, sha512.New))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(contents)
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(plain, bytes.Repeat([]byte{byte(padding)}, padding)...)
	sealed := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, ivBytes).CryptBlocks(sealed, plain)
	return base64.StdEncoding.EncodeToString(sealed), hex.EncodeToString(saltBytes), hex.EncodeToString(ivBytes)
}

// tabbyHash is Tabby's name for a key's passphrase: the SHA-512 of its bytes.
func tabbyHash(key []byte) string {
	sum := sha512.Sum512(key)
	return hex.EncodeToString(sum[:])
}

// newEd25519Key writes an OpenSSH private key this test made, so the identity the
// import adds is one that can actually be inspected.
func newEd25519Key(t *testing.T) []byte {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block)
}

// tabbyConfigWithVault is a configuration whose sessions are in the file and
// whose secrets are in the vault: what Tabby writes unless the user asked it to
// encrypt the configuration too.
const tabbyConfigWithVault = `version: 8
groups:
  - id: g-lab
    name: Lab
profiles:
  - id: ssh:bastion
    type: ssh
    name: bastion
    options:
      host: bastion.example.com
      port: 22
      user: ops
      auth: publicKey
      privateKeys:
        - vault://key-1
      proxyCommand: nc %%h %%p
  - id: ssh:web
    type: ssh
    name: web
    group: g-lab
    color: "#3b82f6"
    options:
      host: web.example.com
      port: 2222
      user: deploy
      auth: password
      x11: true
      jumpHost: ssh:bastion
      socksProxyHost: socks.example.com
      socksProxyPort: 1080
      forwardedPorts:
        - type: Local
          host: 127.0.0.1
          port: 8080
          targetAddress: db.internal
          targetPort: 5432
  - id: local:1
    type: local
    name: Shell
    options: {}
vault:
  version: 1
  contents: %s
  keySalt: %s
  iv: %s
`

// writeTabbyConfig writes the configuration above with the secrets sealed under
// passphrase, and answers its path and the key that is in it.
func writeTabbyConfig(t *testing.T, passphrase string) (path string, key []byte) {
	t.Helper()
	key = newEd25519Key(t)
	contents, err := json.Marshal(map[string]any{
		"secrets": []any{
			map[string]any{
				"type": "ssh:password",
				"key":  map[string]any{"user": "deploy", "host": "web.example.com", "port": 2222},
				// The password the session is reached with.
				"value": "hunter2",
			},
			map[string]any{
				"type":  "file",
				"key":   map[string]any{"id": "key-1"},
				"value": base64.StdEncoding.EncodeToString(key),
			},
			map[string]any{
				"type":  "ssh:key-passphrase",
				"key":   map[string]any{"hash": tabbyHash(key)},
				"value": "key-secret",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	blob, salt, iv := tabbyVaultFor(t, string(contents), passphrase)
	path = filepath.Join(t.TempDir(), "config.yaml")
	content := fmt.Sprintf(tabbyConfigWithVault, blob, salt, iv)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, key
}

func TestImportingTabbyAsksForThePassphraseThenBringsHostsRoutesTunnelsKeysAndPasswords(t *testing.T) {
	path, key := writeTabbyConfig(t, "correct horse")
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	options.Keystore = &memKeys{}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)

	state.Click(hitImportSessions)
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitImportTabby); !ok {
		t.Fatal("the import chooser has no Tabby button")
	}
	state.Click(hitImportTabby)
	if state.modal != modalTabbyPassphrase {
		t.Fatalf("modal = %d, want the passphrase dialog", state.modal)
	}
	// The dialog says what it is asking for and offers to go ahead.
	_, texts := paintedTexts(state)
	if !contains(texts, state.messages.Tools.ImportTabbyPassphraseTitle) ||
		!contains(texts, state.messages.Tools.ImportTabbyPassphraseSubmit) {
		t.Fatalf("the passphrase dialog shows %v", texts)
	}
	// Nothing is added before the vault is open.
	if len(state.hosts) != 0 {
		t.Fatalf("%d hosts were added before the passphrase", len(state.hosts))
	}
	typeInto(state, hitTabbyPassphraseField, "wrong horse")
	state.Click(hitTabbyPassphraseSubmit)
	if state.modal != modalTabbyPassphrase || state.tabby.dialogError != state.messages.Tools.ImportTabbyPassphraseWrong {
		t.Fatalf("modal %d, error %q", state.modal, state.tabby.dialogError)
	}
	if len(state.hosts) != 0 {
		t.Fatalf("%d hosts were added by a wrong passphrase", len(state.hosts))
	}
	typeInto(state, hitTabbyPassphraseField, "correct horse")
	state.Click(hitTabbyPassphraseSubmit)
	if state.modal != modalHostImportResult {
		t.Fatalf("modal = %d, error %q", state.modal, state.tabby.dialogError)
	}

	// The bastion and the web server; the local shell is not a host.
	if len(state.hosts) != 2 {
		t.Fatalf("hosts = %+v", state.hosts)
	}
	bastion, web := state.hosts[0], state.hosts[1]
	if bastion.Label != "bastion" || bastion.Address != "bastion.example.com" || bastion.Auth != authPrivateKey {
		t.Fatalf("bastion = %+v", bastion)
	}
	if web.Label != "web" || web.Group != "Lab" || web.Address != "web.example.com" || web.Port != 2222 ||
		web.User != "deploy" || web.Auth != authPassword || !web.ForwardX11 || web.Color != "#3b82f6" {
		t.Fatalf("web = %+v", web)
	}
	// A session that named its own route keeps it, and a proxy command waits for
	// confirmation instead of running.
	if web.Proxy == nil || web.Proxy.kind != sshconn.ProxySOCKS5 || web.Proxy.address != "socks.example.com:1080" {
		t.Fatalf("web proxy = %+v", web.Proxy)
	}
	if bastion.Proxy == nil || bastion.Proxy.kind != sshconn.ProxyCommand || !bastion.Proxy.unconfirmed {
		t.Fatalf("bastion proxy = %+v", bastion.Proxy)
	}

	// The key Tabby kept in its vault is written out beside the state file and
	// added to the keychain, and the host that named it names its fingerprint.
	if len(state.identities) != 1 {
		t.Fatalf("identities = %+v", state.identities)
	}
	identity := state.identities[0]
	if filepath.Dir(identity.Path) != filepath.Join(filepath.Dir(options.StatePath), "keys") ||
		filepath.Base(identity.Path) != "tabby-key-1.key" {
		t.Fatalf("the key was written to %q", identity.Path)
	}
	if written, err := os.ReadFile(identity.Path); err != nil || !bytes.Equal(written, key) {
		t.Fatalf("the written key = %d bytes, %v", len(written), err)
	}
	if info, err := os.Stat(identity.Path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the key's mode = %v, %v", info.Mode().Perm(), err)
	}
	if identity.Fingerprint == "" || bastion.Identity != identity.Fingerprint {
		t.Fatalf("bastion identity %q, key fingerprint %q", bastion.Identity, identity.Fingerprint)
	}

	// The jump route the web session went through, and the tunnel it opened.
	if len(state.proxies) != 1 || state.proxies[0].TargetID != web.ID || state.proxies[0].JumpID != bastion.ID {
		t.Fatalf("proxies = %+v", state.proxies)
	}
	if len(state.forwards) != 1 {
		t.Fatalf("forwards = %+v", state.forwards)
	}
	forward := state.forwards[0]
	if forward.Mode != portforward.ModeLocal || forward.LocalPort != 8080 ||
		forward.DestinationHost != "db.internal" || forward.DestinationPort != 5432 ||
		forward.HostAddress != web.Address || forward.HostUser != web.User || forward.HostPort != web.Port {
		t.Fatalf("forward = %+v", forward)
	}

	// The secrets the passphrase unlocked are in Motty's own vault, under the
	// names a connection looks them up by.
	if value, found, err := state.secrets.vault.Get(hostSecretName(web.CatalogID, hostPasswordPart)); err != nil || !found || value != "hunter2" {
		t.Fatalf("the saved password = %q, %v, %v", value, found, err)
	}
	if value, found, err := state.secrets.vault.Get(passphraseSecretName(identity.Path)); err != nil || !found || value != "key-secret" {
		t.Fatalf("the key passphrase = %q, %v, %v", value, found, err)
	}

	// Tabby's keys and passwords are imported rather than reported, so the lines
	// that say otherwise for the other clients must not appear.
	warnings := strings.Join(state.hostImportResult.warnings[:state.hostImportResult.visible], "\n")
	if strings.Contains(warnings, "Saved passwords are not imported") || strings.Contains(warnings, "Keys are not imported") {
		t.Fatalf("warnings = %q", warnings)
	}
	if !strings.Contains(warnings, "not imported") {
		t.Fatalf("the skipped local shell is not reported: %q", warnings)
	}

	// Everything is kept.
	restarted := newTestState(t, options)
	if len(restarted.hosts) != 2 || len(restarted.identities) != 1 || len(restarted.proxies) != 1 || len(restarted.forwards) != 1 {
		t.Fatalf("restored %d hosts, %d keys, %d routes, %d tunnels",
			len(restarted.hosts), len(restarted.identities), len(restarted.proxies), len(restarted.forwards))
	}
}

func TestDismissingTheTabbyPassphraseImportsWhatCanBeReadAndSaysWhatCouldNot(t *testing.T) {
	path, _ := writeTabbyConfig(t, "correct horse")
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	options.Keystore = &memKeys{}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)

	state.Click(hitImportSessions)
	state.Click(hitImportTabby)
	state.Click(hitTabbyPassphraseCancel)
	if state.modal != modalHostImportResult {
		t.Fatalf("modal = %d", state.modal)
	}
	// The sessions are in the file, so they are added; the secrets are not.
	if len(state.hosts) != 2 || len(state.forwards) != 1 || len(state.proxies) != 1 {
		t.Fatalf("hosts %d, tunnels %d, routes %d", len(state.hosts), len(state.forwards), len(state.proxies))
	}
	if len(state.identities) != 0 {
		t.Fatalf("a key was written out of a vault that was never opened: %+v", state.identities)
	}
	warnings := strings.Join(state.hostImportResult.warnings[:state.hostImportResult.visible], "\n")
	if !strings.Contains(warnings, "left locked") {
		t.Fatalf("warnings = %q", warnings)
	}
	if _, found, _ := state.secrets.vault.Get(hostSecretName(state.hosts[1].CatalogID, hostPasswordPart)); found {
		t.Fatal("a password was saved from a vault that was never opened")
	}
}

func TestImportingTabbyWhileMottysOwnVaultIsLockedSaysWhereToUnlockIt(t *testing.T) {
	path, _ := writeTabbyConfig(t, "correct horse")
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	options.Keystore = &memKeys{}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)
	// The user's own vault is behind a master password this run has not typed.
	if err := state.secrets.vault.SetPassword("long enough one"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.DropKeyForTests()

	state.Click(hitImportSessions)
	state.Click(hitImportTabby)
	typeInto(state, hitTabbyPassphraseField, "correct horse")
	state.Click(hitTabbyPassphraseSubmit)
	// The hosts are still worth adding, so they are, and the secrets are told
	// about rather than silently dropped.
	if state.modal != modalHostImportResult || len(state.hosts) != 2 {
		t.Fatalf("modal %d, hosts %d", state.modal, len(state.hosts))
	}
	warnings := strings.Join(state.hostImportResult.warnings[:state.hostImportResult.visible], "\n")
	if !strings.Contains(warnings, "Settings") {
		t.Fatalf("warnings = %q", warnings)
	}
}

func TestATabbyFileThatIsNotAConfigurationIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("host: example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)

	state.Click(hitImportSessions)
	state.Click(hitImportTabby)
	if state.modal != modalNone || len(state.hosts) != 0 {
		t.Fatalf("modal %d, hosts %d", state.modal, len(state.hosts))
	}
	if want := fmt.Sprintf(state.messages.ActionFeedback.ImportSessionsFailedFmt, "Tabby"); state.status != want {
		t.Fatalf("status = %q", state.status)
	}
}
