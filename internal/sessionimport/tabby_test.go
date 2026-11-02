package sessionimport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The two files below were written by Tabby's own vault code (Node's crypto,
// PBKDF2-SHA512 with 100000 rounds into AES-256-CBC), so the test proves this
// program opens what Tabby made rather than what it makes itself.
const tabbyFixturePlain = `{
  "version": 8,
  "groups": [
    {
      "id": "g-root",
      "name": "Production"
    },
    {
      "id": "g-db",
      "name": "Databases",
      "parentGroupId": "g-root"
    }
  ],
  "profiles": [
    {
      "id": "ssh:web",
      "type": "ssh",
      "name": "web",
      "group": "g-db",
      "color": "#3b82f6",
      "options": {
        "host": "web.example.com",
        "port": 2222,
        "user": "deploy",
        "auth": "password",
        "privateKeys": [],
        "jumpHost": null,
        "x11": true,
        "agentForward": false,
        "forwardedPorts": [
          {
            "type": "Local",
            "host": "127.0.0.1",
            "port": 8080,
            "targetAddress": "db.internal",
            "targetPort": 5432
          },
          {
            "type": "Dynamic",
            "host": "127.0.0.1",
            "port": 1080,
            "targetAddress": "",
            "targetPort": 0
          }
        ]
      }
    },
    {
      "id": "ssh:jump",
      "type": "ssh",
      "name": "bastion",
      "options": {
        "host": "bastion.example.com",
        "port": 22,
        "user": "ops",
        "auth": "publicKey",
        "privateKeys": [
          "/home/u/.ssh/id_ed25519"
        ]
      }
    },
    {
      "id": "ssh:db",
      "type": "ssh",
      "name": "db",
      "group": "g-root",
      "options": {
        "host": "db.example.com",
        "port": 22,
        "user": "dba",
        "auth": "password",
        "jumpHost": "ssh:jump"
      }
    },
    {
      "id": "telnet:old",
      "type": "telnet",
      "name": "switch",
      "options": {
        "host": "switch.example.com",
        "port": 23
      }
    },
    {
      "id": "serial:tty",
      "type": "serial",
      "name": "console",
      "options": {
        "port": "/dev/ttyUSB0",
        "baudrate": 115200,
        "databits": 7,
        "stopbits": 2,
        "parity": "even",
        "rtscts": true
      }
    },
    {
      "id": "ssh:vaultkey",
      "type": "ssh",
      "name": "vaultkey",
      "options": {
        "host": "vault.example.com",
        "port": 22,
        "user": "vk",
        "auth": "publicKey",
        "privateKeys": [
          "vault://abc123"
        ]
      }
    },
    {
      "id": "local:1",
      "type": "local",
      "name": "PowerShell",
      "options": {}
    }
  ],
  "vault": {
    "version": 1,
    "contents": "3oSa7y51mtfOqFjkUoP0Z7RCJrACFxb7+9YQ0lAIGhnkyxMtqdwh1RWRYVqkOtD5pWVtLMtgy8g6khFysmFL281/nqLK6zhCxUWA2nAw9HpZocszHTgXokrnPhebMJ1/edt4Qh3k5ECXt+6nNz4BdsPQ89VjNzcYrT1GnP8YRDSxzxRlI/L6V91v4lVZPSCwq/HoA3nWGuDs4e+hfI/3WN7CkIdFCWoPgtAE+oO5GsWzASk3U1G2YDNdHj5hWYpwiyhNUNCnSWptMYESw9hJm3CoWBLeWAYJm8jRpiz1tzoVsGwi1iPtww+ykxLT5V0sh8NYfkEvnPicnk+JnFIY8AerYr2V0wcsibouyke50FO8XZKWdDqyUgQ4+PaFk93XcC3Z5mQKFsH1HbieFJq3hKuKYbSDLz5lhyyQHsJSob7bkeriNLTRLrhwbemuQ2Iz6C4e/aRc+3fzs7mcLztWW5mZIw+zzlqwmQN3AttbvDQ9dy9INP3HaAr4/HS2HLDjM9og3NRt5091nJyqLMs3wprpPoy1YjQkIjaNYhyuBnXn0DTSoJK9c19I502ZgwwVgHthqU+JZ0QUKNpsjo8hOtyK1DETnXRBO3izsM/fmPINSVTJ6sZSpjDxqaaF3ae1iA8bVKk3+9wv2b70fEEA9OE3DbxcPNHSWw4d9b1ChN8jvLZO5/FKr3JPE/m/UxigTBdiO/OORK1SyuBV5BzgxWGFo/XtY5q3yxiRJZ6p9Gsz07DmUAtmUwRYTzz3pn+H4TrwedpI4Kw80CCp/tDaUpPLqTpzeXbeuyKrJzdhnIj6uvohFVnA0UZv8upHY50D8vv7/Sc3U/nm+SHbE4ccrsQgqGhQPHjxFFQwcXLhNWvhW1E2bsF8DwoQaWYJluNuTb3AVWOW15McteN21hVJvxs4BotGTA6kh5Nj5Fo304ANJaSvtTtpsg4zPTk8hhRugGDou1KAIOKtlHZZvsw3IihxCtiA/jKH2Szq6CIBrg1ZFd6f7RnxGUUfZlZuRofXa8rulkk46hHCjoJXdBPNFnhANU+idoTURICxIHtzCFzvnV3oDY3bOqhaUD+kk9szMxZ8AjS4ptmRvRCJ+W5BZVunXBQGXFsmvnIdmO+pa0Hn2ndLqt+VTBRmvEDOiPIAPxi7mFgGCcEiyMfrxDtGozWNlqwb6mEUpCVqij2e11Ca0wCVVqXmWOCKMf7Wz6Yg2X157KAB1wp/Qr+U3cyLmQvgAZcJLK8RyZFeXT6+SHHI5J7om3wNAh97za+563dkeb6Fwoti+Kss5Dnq1zmSQiBleTQ09RUihie9A2mkCWnXfxL2u60jRi6DMTkISHn6d90JfefFWkvrSNPK4j1RvINiT2uJppjLhIIDKV+Ulfngb8bSCvCDCXzvtYDxuI1A2P7JA+/yuP5eaIW/szMjQ1Y/kjLY/kKOJSn8+4acARXKmAh5ytxzRrvYoLaA25kiDhSAWowbvoQYBphTczL78He0J6J3yZfT8Rjc0nykWis+AkMywaPOflynFQJdngdMJdJR3LYq96DXkd/dXPsFd+sro2CY/2REWF94Lw4TCCroe7xavVAj2lcnKfs5ETJEGqcoSH1bDPAEWySj/xLozD+RjV2f5kVBY1pzs2gLuOyFkMJe0Q9jnJrcPdXEUA6iIZNrQ3CKSPb7EtO+aNSGdGUBCoT7nB7+pBw/vc+aq8Vx/YeLm3bUSeJPCsuq3neHAjGzvY1pPZ9FHARzLXmEbfbWzuB1LVh2c0dfH0dJHRkmD4y29SIgu2fsBMKxWCRHDqcM9x3kFNNPUrFioVWuXLAmTkyXGb19AJ8Ui0XJ5haFYVh9QPRpyiDlTs9gCQnNwh8PwNobj9gGeKlxqtxJ5rhIWxrLBY8eMIHeBJ366ctbQlpjfv17LxU7orO07NS28Im4PcDl8ZpWF441VJKmLIKzpxElKTuLkVYMj2FuWVi+GIDKxjPshZQ8i0ZDhpIhVi+3BJn8q9iZYYvjBgwoEbzUbfG9EeTvXOfcVhXxLEWrFWli+Br1kfAlr31h0yivsUopCV7nYw+1CC4Xj4X46aABecj7GVXYhMKE9JD8ZyrUZI8Jvshioccl0Yu1J/wD8PLIgChjOojg+0w/w7xVL6xx8OsYIMcccCvo6nVVwVThJhYN+Ea9pXYtlAejpXvicnW3Y8dImcE2AEdTCJQHLU9x7O5o92ow5LCNNZWDlA+9tvjIy2FibFjymwj/ogc7vWRwiVKAhx42VZZ/0Lt9iGRJBl4PAYO+cePqFjSCYHahyFfv/Xn1PmGm05eCxfJcRFJk7SB+BWAY5ogJyb4N553PvC2io9XtLJgoa62/5A1i65CPE+/m7Ii/6Ss28bYzKSsZLJqrOorHMrNcvYPlAOXPktOEb28+x7eVc3JKWIakK7yqlPGBiEgq2ECDpU86qHGJZ3PaJn8SNRfZSoVNPFoPUYbMfWtyfkAWuBVR/favQDVJm7CypZHyYfl/nbvZpXdnA+d59ih1imqL7xZyrUQgZohRt3TgHNHozupJlDHAHFTcyZTg8aobUv3ZTKTtQkZ5NZepaGhA+KpYvWwzsAJAD6Dub+VFjvg1nC9Pq4VawB2mFCsjMkeGP4RcsyzK5ciHwAKq7+7T1DPZl5tUEj1beSma3YBABNYfN2vW3pMHXGYdqBkOlzEaLB041BxAWhkmWHphNH6Itd7Z04SbuN2HSdIE0kZCyF8pFbi3dLPqbCzRb15ee8I21t33Sras",
    "keySalt": "8a83636bf584efd9",
    "iv": "2e57c383a371bb2ef9ce7e628becdd7e"
  }
}`

const tabbyFixtureEncrypted = `{
  "vault": {
    "version": 1,
    "contents": "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",
    "keySalt": "97a56c90b269f8ec",
    "iv": "41af3cc2eee4edb318791c91b880b1f7"
  },
  "encrypted": true
}`

const tabbyFixtureKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQ==
-----END OPENSSH PRIVATE KEY-----
`

// writeTabbyFixture writes one of the fixture files, with the key path the
// bastion profile names replaced by a file this test made.
func writeTabbyFixture(t *testing.T, content, keyPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content = strings.ReplaceAll(content, "/home/u/.ssh/id_ed25519", keyPath)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTabbyKey(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, []byte(tabbyFixtureKey), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestATabbyFileGivesItsSessionsBeforeAnyPassphrase(t *testing.T) {
	keyPath := writeTabbyKey(t)
	path := writeTabbyFixture(t, tabbyFixturePlain, keyPath)
	tabby, err := ParseTabby(path)
	if err != nil {
		t.Fatal(err)
	}
	if !tabby.NeedsPassphrase() || tabby.Encrypted() {
		t.Fatalf("needs passphrase %v, encrypted %v", tabby.NeedsPassphrase(), tabby.Encrypted())
	}
	result := tabby.Result()
	// ssh web, ssh bastion, ssh db, ssh vaultkey, telnet switch, serial console;
	// the local shell is not a host.
	if len(result.Entries) != 6 || result.Skipped != 1 {
		t.Fatalf("%d entries, %d skipped", len(result.Entries), result.Skipped)
	}
	web := result.Entries[0]
	if web.Name != "web" || web.Host != "web.example.com" || web.Port != 2222 || web.User != "deploy" ||
		web.Auth != AuthPassword || !web.X11 || web.AgentForward || web.Color != "#3b82f6" {
		t.Fatalf("web = %+v", web)
	}
	// A group names the group above it, so the path reads from the outside in.
	if web.Group != "Production/Databases" {
		t.Fatalf("web group = %q", web.Group)
	}
	if result.Entries[1].Group != "" || result.Entries[2].Group != "Production" {
		t.Fatalf("groups = %q, %q", result.Entries[1].Group, result.Entries[2].Group)
	}
	if result.Entries[1].Auth != AuthPrivateKey || result.Entries[1].KeyPath != keyPath {
		t.Fatalf("bastion = %+v", result.Entries[1])
	}
	if result.Entries[5].KeyID != "abc123" || len(result.Entries[5].KeyData) != 0 {
		t.Fatalf("a key kept in the vault is read before it is opened: %+v", result.Entries[5])
	}
	if result.Entries[3].Protocol != Telnet || result.Entries[3].Port != 23 {
		t.Fatalf("telnet = %+v", result.Entries[3])
	}
	console := result.Entries[4]
	if console.Protocol != Serial || console.Host != "/dev/ttyUSB0" || console.Line != "115200 7E2" || console.Flow != "rtscts" {
		t.Fatalf("serial = %+v", console)
	}
	if result.Keys != 2 {
		t.Fatalf("keys = %d", result.Keys)
	}
	// The jump route and the two tunnels of the web session.
	if len(result.Jumps) != 1 || result.Jumps[0].Target != 2 || result.Jumps[0].Via != 1 {
		t.Fatalf("jumps = %+v", result.Jumps)
	}
	if len(result.Forwards) != 2 {
		t.Fatalf("forwards = %+v", result.Forwards)
	}
	if forward := result.Forwards[0]; forward.Mode != ForwardLocal || forward.LocalPort != 8080 ||
		forward.DestinationHost != "db.internal" || forward.DestinationPort != 5432 {
		t.Fatalf("local forward = %+v", forward)
	}
	if forward := result.Forwards[1]; forward.Mode != ForwardSOCKS || forward.LocalPort != 1080 {
		t.Fatalf("dynamic forward = %+v", forward)
	}
	for _, entry := range result.Entries {
		if entry.Password != "" || entry.KeyPassphrase != "" {
			t.Fatalf("a secret was read without the passphrase: %+v", entry)
		}
	}
}

func TestATabbyVaultHandsBackTheSecretsItsPassphraseOpens(t *testing.T) {
	keyPath := writeTabbyKey(t)
	path := writeTabbyFixture(t, tabbyFixturePlain, keyPath)
	tabby, err := ParseTabby(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tabby.Unlock("not the passphrase"); !errors.Is(err, ErrTabbyVault) {
		t.Fatalf("Unlock with a wrong passphrase = %v", err)
	}
	if !tabby.NeedsPassphrase() {
		t.Fatal("a wrong passphrase opened the vault")
	}
	if err := tabby.Unlock("correct horse"); err != nil {
		t.Fatal(err)
	}
	if tabby.NeedsPassphrase() {
		t.Fatal("the vault still says it needs a passphrase after it was opened")
	}
	result := tabby.Result()
	// A password names its connection by user, host and port.
	if got := result.Entries[0].Password; got != "web-secret" {
		t.Fatalf("web password = %q", got)
	}
	if got := result.Entries[2].Password; got != "db-secret" {
		t.Fatalf("db password = %q", got)
	}
	// A passphrase names the key by the hash of its bytes, so both the key file
	// the profile points at and the key kept in the vault are matched.
	if got := result.Entries[1].KeyPassphrase; got != "key-secret" {
		t.Fatalf("bastion passphrase = %q", got)
	}
	if got := string(result.Entries[5].KeyData); got != tabbyFixtureKey {
		t.Fatalf("the key kept in the vault was not read: %q", got)
	}
	if got := result.Entries[5].KeyPassphrase; got != "key-secret" {
		t.Fatalf("vault key passphrase = %q", got)
	}
	if result.Entries[3].Password != "" || result.Entries[4].Password != "" {
		t.Fatal("a secret was handed to a session it does not belong to")
	}
}

func TestATabbyFileWithAnEncryptedConfigurationGivesNothingBeforeThePassphrase(t *testing.T) {
	path := writeTabbyFixture(t, tabbyFixtureEncrypted, "/home/u/.ssh/id_ed25519")
	tabby, err := ParseTabby(path)
	if err != nil {
		t.Fatal(err)
	}
	if !tabby.Encrypted() || !tabby.NeedsPassphrase() {
		t.Fatalf("encrypted %v, needs passphrase %v", tabby.Encrypted(), tabby.NeedsPassphrase())
	}
	if result := tabby.Result(); len(result.Entries) != 0 {
		t.Fatalf("%d sessions came out of a file that holds none", len(result.Entries))
	}
	if err := tabby.Unlock("not the passphrase"); !errors.Is(err, ErrTabbyVault) {
		t.Fatalf("Unlock with a wrong passphrase = %v", err)
	}
	if result := tabby.Result(); len(result.Entries) != 0 {
		t.Fatalf("a wrong passphrase read %d sessions", len(result.Entries))
	}
	if err := tabby.Unlock("correct horse"); err != nil {
		t.Fatal(err)
	}
	result := tabby.Result()
	if len(result.Entries) != 6 || result.Skipped != 1 {
		t.Fatalf("%d entries, %d skipped", len(result.Entries), result.Skipped)
	}
	if result.Entries[0].Group != "Production/Databases" || result.Entries[0].Password != "web-secret" {
		t.Fatalf("web = %+v", result.Entries[0])
	}
	if len(result.Jumps) != 1 || len(result.Forwards) != 2 {
		t.Fatalf("jumps %+v, forwards %+v", result.Jumps, result.Forwards)
	}
	if string(result.Entries[5].KeyData) != tabbyFixtureKey {
		t.Fatal("the key kept in the vault was not read from the encrypted configuration")
	}
}

func TestAFileThatIsNotATabbyConfigIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("host: example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseTabby(path); err == nil {
		t.Fatal("a YAML file that is not a Tabby config was read")
	}
	broken := filepath.Join(t.TempDir(), "broken.yaml")
	if err := os.WriteFile(broken, []byte("profiles: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseTabby(broken); err == nil {
		t.Fatal("a broken file was read")
	}
}

func TestATabbyVaultWhoseVersionIsUnknownIsNotGuessedAt(t *testing.T) {
	content := strings.Replace(tabbyFixturePlain, `"version": 1,`, `"version": 2,`, 1)
	path := writeTabbyFixture(t, content, "/home/u/.ssh/id_ed25519")
	tabby, err := ParseTabby(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tabby.Unlock("correct horse"); err == nil || errors.Is(err, ErrTabbyVault) {
		t.Fatalf("an unknown vault version = %v", err)
	}
}

func TestATabbyFileWithoutAVaultSaysWhereItsPasswordsWent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "version: 8\nprofiles:\n  - id: ssh:a\n    type: ssh\n    name: a\n    options:\n      host: a.example.com\n      port: 22\n      user: u\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	tabby, err := ParseTabby(path)
	if err != nil {
		t.Fatal(err)
	}
	if tabby.NeedsPassphrase() {
		t.Fatal("a file with no vault asks for a passphrase")
	}
	if result := tabby.Result(); len(result.Entries) != 1 || result.Entries[0].Host != "a.example.com" {
		t.Fatalf("entries = %+v", result.Entries)
	}
	found := false
	for _, warning := range tabby.Result().Warnings {
		if strings.Contains(warning, "keychain") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no warning says where the passwords went: %v", tabby.Result().Warnings)
	}
}
