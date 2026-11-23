package sessionimport

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestUnlockTabbyVaultMatchesPasswordsAndEncryptedProfiles(t *testing.T) {
	path := filepath.Join("testdata", "tabby-vault.yaml")
	source, err := OpenTabbyConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Unlock([]byte("wrong")); !errors.Is(err, ErrTabbyVaultUnlock) {
		t.Fatal("wrong master password was not rejected")
	}
	result, err := source.Unlock([]byte("fixture-vault-password"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 4 || result.Entries[0].Group != "Lab" || result.PlainSecrets != 0 {
		t.Fatal("encrypted profiles were not restored")
	}
	for index, want := range []string{"fixture-first", "fixture-fallback", "", ""} {
		if result.Entries[index].Password != want {
			t.Fatalf("incorrect password match for profile %d", index)
		}
	}

	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	outerProfiles := "profiles:\n  - type: ssh\n    options: {host: primary.example.test, user: ops}\n"
	path = write(t, t.TempDir(), "config.yaml",
		[]byte(strings.Replace(string(file), "encrypted: true", outerProfiles, 1)))
	plain, err := ParseTabby(path)
	if err != nil || len(plain.Entries) != 1 || plain.Entries[0].Password != "" {
		t.Fatal("ordinary Tabby import should read profiles without unlocking the Vault")
	}
	source, err = OpenTabbyConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	unlocked, err := source.Unlock([]byte("fixture-vault-password"))
	if err != nil || len(unlocked.Entries) != 1 || unlocked.Entries[0].Password != "fixture-first" {
		t.Fatal("configured outer profile did not receive the saved password")
	}
}

func TestTabbyProfileDefaultsGroupsAndPrivateKeyReferences(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "test key")
	data := `profileDefaults:
  ssh:
    options:
      user: deploy
      auth: password
      port: 2222
groups:
  - id: work
    name: Work
  - id: build
    name: Build
    parentGroupId: work
    defaults:
      ssh:
        options:
          user: builder
profiles:
  - type: ssh
    name: Runner
    group: build
    options:
      host: build.example.test
      auth: publicKey
      privateKeys:
        - 'file://` + key + `'
        - '` + key + `'
        - vault://test-key-id
  - type: ssh
    options:
      host: second.example.test
      auth: null
      password: "fixture password "
  - type: ssh
    options:
      host: routed.example.test
      jumpHost: jump-profile
  - type: ssh
    options:
      host: invalid.example.test
      port: invalid
  - type: ssh
    options:
      host: ask.example.test
      user: null
  - type: local
    options:
      command: bash
`
	result, err := Parse(Tabby, []string{write(t, dir, "config.yaml", []byte(data))})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 2 || result.Skipped != 4 || result.Keys != 1 || result.PlainSecrets != 1 {
		t.Fatalf("unexpected totals: %d hosts, %d skipped, %d keys, %d plaintext entries",
			len(result.Entries), result.Skipped, result.Keys, result.PlainSecrets)
	}
	runner := result.Entries[0]
	if runner.Name != "Runner" || runner.Group != "Work/Build" || runner.User != "builder" || runner.Port != 2222 ||
		runner.SSHSettings.Auth != "publicKey" || !slices.Equal(runner.SSHSettings.IdentityFiles, []string{key}) {
		t.Fatal("Tabby SSH settings were not preserved")
	}
	second := result.Entries[1]
	if second.Name != second.Host || second.User != "deploy" || second.SSHSettings.Auth != "" ||
		second.Password != "fixture password " {
		t.Fatal("default settings, explicit null or password was not preserved")
	}
}

func TestTabbyInvalidConfigErrorsDoNotEchoScalars(t *testing.T) {
	dir := t.TempDir()
	for _, test := range []struct {
		data string
		want string
	}{
		{"encrypted: true\nvault: {contents: fixture-value}\n", "configuration is encrypted"},
		{"profiles: fixture-value\n", "invalid Tabby YAML"},
		{"other: fixture-value\n", "no profiles list"},
	} {
		_, err := ParseTabby(write(t, dir, "config.yaml", []byte(test.data)))
		if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), "fixture-value") {
			t.Fatal("Tabby returned an unexpected or unsanitized parse error")
		}
	}
}
