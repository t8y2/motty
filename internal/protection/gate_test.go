package protection

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"motty/internal/configstore"
	"motty/internal/vault"
)

func TestMain(m *testing.M) {
	kdfCost = kdf{Name: "argon2id", Time: 1, MemoryKiB: 64, Threads: 1}
	os.Exit(m.Run())
}

func fixture(t *testing.T) (*Gate, string) {
	t.Helper()
	dir := t.TempDir()
	options := Options{Directory: dir, ConfigFormat: 28, Keystore: vault.FileKeystore{Path: filepath.Join(dir, "vault.key")}}
	store, err := configstore.Open(filepath.Join(dir, "config.db"), 28)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Commit(configstore.Change{Put: []configstore.Entry{{Kind: "host", ID: "private.example/admin", Payload: []byte(`{"d":{"address":"private.example","user":"admin","group":"Production"}}`)}}, Local: map[string][]byte{"state": []byte(`{"language":"zh-CN","sessions":[{"address":"private.example"}]}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	secrets, err := vault.Open(filepath.Join(dir, "vault.json"), options.Keystore)
	if err != nil {
		t.Fatal(err)
	}
	if err := secrets.Set("host:one:password", "secret-account-password"); err != nil {
		t.Fatal(err)
	}
	gate, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	return gate, dir
}

func TestProtectionRoundTripAndColdUnlock(t *testing.T) {
	g, dir := fixture(t)
	password := "master password one"
	if err := g.Enable(password); err != nil {
		t.Fatal("enable", err)
	}
	if !g.Enabled() || g.Locked() {
		t.Fatal("enabled profile did not stay open")
	}
	path := filepath.Join(dir, "config.db")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"private.example", "Production", "secret-account-password"} {
		if bytes.Contains(data, []byte(text)) {
			t.Fatalf("plaintext %q in database", text)
		}
	}
	if binary.BigEndian.Uint32(data[60:64]) != 2 {
		t.Fatal("old-client schema guard absent")
	}
	if _, err := os.Stat(filepath.Join(dir, "vault.key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ordinary file key survived: %v", err)
	}
	root, err := g.EnrollmentKey(password)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(root)
	g.Lock()
	if _, _, err := configstore.ReadOnly(path, 28); !errors.Is(err, configstore.ErrLocked) {
		t.Fatalf("locked read: %v", err)
	}
	cold, err := Open(g.options)
	if err != nil {
		t.Fatal(err)
	}
	if !cold.Locked() {
		t.Fatal("cold profile was opened automatically")
	}
	if err := cold.Unlock("incorrect"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	if err := cold.UnlockWithKey(root); err != nil {
		t.Fatal(err)
	}
	entries, local, err := configstore.ReadOnly(path, 28)
	if err != nil || len(entries) != 1 || !bytes.Contains(local["state"], []byte("private.example")) {
		t.Fatalf("unlocked records: %d %v", len(entries), err)
	}
	secret, err := vault.Open(filepath.Join(dir, "vault.json"), g.options.Keystore)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok, err := secret.Get("host:one:password"); err != nil || !ok || value != "secret-account-password" {
		t.Fatalf("saved secret: %v", err)
	}
	if err := cold.ChangePassword(password, "a different password"); err != nil {
		t.Fatal(err)
	}
	cold.Lock()
	if err := cold.Unlock(password); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("old password accepted: %v", err)
	}
	if err := cold.Unlock("a different password"); err != nil {
		t.Fatal(err)
	}
	if err := cold.Disable("a different password"); err != nil {
		t.Fatal("disable", err)
	}
	if cold.Enabled() {
		t.Fatal("disable left protection enabled")
	}
	plain, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(plain[60:64]) != 1 || !bytes.Contains(plain, []byte("private.example")) {
		t.Fatal("default storage was not restored")
	}
	reopened, err := vault.Open(filepath.Join(dir, "vault.json"), g.options.Keystore)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok, err := reopened.Get("host:one:password"); err != nil || !ok || value != "secret-account-password" {
		t.Fatalf("default secret: %v", err)
	}
}

func TestHistoricalBackupsSurvivePasswordChangeAndDisable(t *testing.T) {
	g, dir := fixture(t)
	original := []byte("previous private configuration")
	path := filepath.Join(dir, "config.db.bak")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	collision := filepath.Join(dir, "config.db.bak.protected")
	if err := os.WriteFile(collision, []byte("keep existing backup"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := g.Enable("initial password"); err != nil {
		t.Fatal(err)
	}
	if value, _ := os.ReadFile(collision); string(value) != "keep existing backup" {
		t.Fatal("existing recovery backup overwritten")
	}
	backup, err := os.ReadFile(path + ".protected.2")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(backup, original) {
		t.Fatal("recovery backup is plaintext")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("plaintext historical backup retained")
	}
	if err := g.ChangePassword("initial password", "replacement password"); err != nil {
		t.Fatal(err)
	}
	var archived recoveryBackup
	if err := json.Unmarshal(backup, &archived); err != nil {
		t.Fatal(err)
	}
	archiveKey, err := unwrap(archived.Protection, "initial password")
	if err != nil {
		t.Fatal(err)
	}
	defer clear(archiveKey)
	if checkKey(g.envelope, archiveKey) {
		t.Fatal("historical archive retained the active profile root")
	}
	if err := configstore.SetProtectionKey(g.options.ConfigPath, archiveKey); !errors.Is(err, configstore.ErrAuthentication) {
		t.Fatalf("archive key opened active configuration: %v", err)
	}
	currentImage, err := os.ReadFile(g.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, currentImage, 0600); err != nil {
		t.Fatal(err)
	}
	if err := g.Disable("replacement password"); err != nil {
		t.Fatal(err)
	}
	decoded, err := OpenRecoveryBackup(backup, "initial password")
	if err != nil || !bytes.Equal(decoded, original) {
		t.Fatalf("independent backup was not recoverable: %v", err)
	}
	newArchive, err := os.ReadFile(path + ".protected.3")
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := OpenRecoveryFile(newArchive, "replacement password")
	if err != nil {
		t.Fatal(err)
	}
	defer clear(recovery.ProfileKey)
	if !bytes.Equal(recovery.Data, currentImage) || len(recovery.ProfileKey) != 32 || len(recovery.ProtectionJSON) == 0 {
		t.Fatal("encrypted current backup lost its key metadata during disable")
	}
	if err := g.Enable("new activation password"); err != nil {
		t.Fatal(err)
	}
	if checkKey(g.envelope, recovery.ProfileKey) {
		t.Fatal("new activation reused an archived profile root")
	}
}

func TestAbandonedDisablePreparationCanBeScrubbedBeforeUnlock(t *testing.T) {
	g, dir := fixture(t)
	if err := g.Enable("master password"); err != nil {
		t.Fatal(err)
	}
	g.Lock()
	stage := filepath.Join(dir, ".motty-protection-stage-00112233445566778899aabbccddeeff")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	owner, _ := json.Marshal(struct {
		Profile string `json:"profile"`
	}{g.ProfileID()})
	if err := os.WriteFile(filepath.Join(stage, "owner.json"), owner, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "config.db.next"), []byte("unfinished plaintext migration"), 0600); err != nil {
		t.Fatal(err)
	}
	cold, err := Open(g.options)
	if err != nil {
		t.Fatal(err)
	}
	if !cold.PendingMigration() {
		t.Fatal("abandoned preparation ignored")
	}
	if err := cold.CleanupPreparation(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("plaintext staging remained")
	}
	if !cold.Locked() {
		t.Fatal("cleanup authenticated the profile")
	}
	if err := cold.Unlock("master password"); err != nil {
		t.Fatal(err)
	}
}

func TestEnableRejectsSymlinkBackupsWithoutChangingTheProfile(t *testing.T) {
	g, dir := fixture(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("untouched original"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.db.bak")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	before, err := os.ReadFile(g.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Enable("master password"); err == nil {
		t.Fatal("symlink backup accepted")
	}
	after, err := os.ReadFile(g.options.ConfigPath)
	if err != nil || !bytes.Equal(before, after) || g.Enabled() {
		t.Fatal("failed preparation changed the profile")
	}
	if original, _ := os.ReadFile(outside); string(original) != "untouched original" {
		t.Fatal("symlink target modified")
	}
}

func TestLostEnvelopeNeverCreatesAnUnprotectedProfile(t *testing.T) {
	g, dir := fixture(t)
	if err := g.Enable("master password"); err != nil {
		t.Fatal(err)
	}
	g.Lock()
	if err := os.Remove(filepath.Join(dir, "protection.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(g.options); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("lost metadata: %v", err)
	}
}

func TestUnsharedStoreMustCloseBeforeProfileMigration(t *testing.T) {
	g, _ := fixture(t)
	store, err := configstore.Open(g.options.ConfigPath, g.options.ConfigFormat)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Enable("master password"); !errors.Is(err, configstore.ErrBusy) {
		store.Close()
		t.Fatalf("migration ignored an ordinary open handle: %v", err)
	}
	if g.Enabled() {
		store.Close()
		t.Fatal("busy migration changed protection mode")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := g.Enable("master password"); err != nil {
		t.Fatalf("migration after Close: %v", err)
	}
}

func TestInterruptedCommitRecoversFromAnAlreadyMovedDatabase(t *testing.T) {
	g, dir := fixture(t)
	root := bytes.Repeat([]byte{7}, 32)
	password := "recovery password"
	env, err := makeEnvelope("00112233445566778899aabbccddeeff", password, root)
	if err != nil {
		t.Fatal(err)
	}
	m := &migration{Version: 1, Enable: true, Envelope: env, Stage: ".motty-protection-stage-00112233445566778899aabbccddeeff"}
	stage := filepath.Join(dir, m.Stage)
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := configstore.BuildCopy(g.options.ConfigPath, filepath.Join(stage, "config.next"), 28, root); err != nil {
		t.Fatal(err)
	}
	if err := vault.BuildProtectedCopy(g.options.VaultPath, filepath.Join(stage, "vault.next"), g.options.Keystore, root, env.Profile); err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(env)
	if err := os.WriteFile(filepath.Join(stage, "profile.next"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ target, staged string }{{"config.db", "config.next"}, {"vault.json", "vault.next"}, {"protection.json", "profile.next"}} {
		data, err := os.ReadFile(filepath.Join(stage, item.staged))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		m.Steps = append(m.Steps, migrationStep{Target: item.target, Staged: item.staged, Hash: hex.EncodeToString(sum[:])})
	}
	m.MAC = authenticate(root, m.signedBytes())
	journal, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "protection-migration.json"), journal, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(stage, "config.next"), g.options.ConfigPath); err != nil {
		t.Fatal(err)
	}
	cold, err := Open(g.options)
	if err != nil {
		t.Fatal(err)
	}
	if !cold.PendingMigration() {
		t.Fatal("interrupted commit ignored")
	}
	if err := cold.Unlock(password); err != nil {
		t.Fatal(err)
	}
	if cold.PendingMigration() || cold.Locked() {
		t.Fatal("recovery did not finish")
	}
	if _, err := os.Stat(filepath.Join(dir, "protection-migration.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal retained")
	}
}

func TestFailedRecoveryCanRollbackAndRetryWithoutLosingTheProfile(t *testing.T) {
	g, dir := fixture(t)
	before, err := os.ReadFile(g.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	root := bytes.Repeat([]byte{12}, 32)
	password := "recovery password"
	env, err := makeEnvelope("11223344556677889900aabbccddeeff", password, root)
	if err != nil {
		t.Fatal(err)
	}
	m := &migration{Version: 1, Enable: true, Envelope: env, Stage: ".motty-protection-stage-11223344556677889900aabbccddeeff"}
	stage := filepath.Join(dir, m.Stage)
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := configstore.BuildCopy(g.options.ConfigPath, filepath.Join(stage, "config.next"), 28, root); err != nil {
		t.Fatal(err)
	}
	if err := vault.BuildProtectedCopy(g.options.VaultPath, filepath.Join(stage, "vault.next"), g.options.Keystore, root, env.Profile); err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(env)
	if err := os.WriteFile(filepath.Join(stage, "profile.next"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ target, staged string }{{"config.db", "config.next"}, {"vault.json", "vault.next"}, {"protection.json", "profile.next"}} {
		data, err := os.ReadFile(filepath.Join(stage, item.staged))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		step := migrationStep{Target: item.target, Staged: item.staged, Hash: hex.EncodeToString(sum[:])}
		if original, err := os.ReadFile(filepath.Join(dir, item.target)); err == nil {
			step.Existed = true
			step.Previous = item.target + ".before"
			previous, err := seal(root, "migration-before/"+item.target, original)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stage, step.Previous), previous, 0600); err != nil {
				t.Fatal(err)
			}
		}
		m.Steps = append(m.Steps, step)
	}
	oldKey, err := g.options.Keystore.Load(vault.KeyService, vault.KeyAccount)
	if err != nil {
		t.Fatal(err)
	}
	m.OldKey, err = seal(root, "migration-old-key/"+m.Stage, oldKey)
	clear(oldKey)
	if err != nil {
		t.Fatal(err)
	}
	m.MAC = authenticate(root, m.signedBytes())
	journal, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "protection-migration.json"), journal, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(stage, "config.next"), g.options.ConfigPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(stage, "vault.next")); err != nil {
		t.Fatal(err)
	}
	cold, err := Open(g.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := cold.Unlock(password); !errors.Is(err, ErrMigrationRolledBack) || cold.PendingMigration() || cold.Enabled() {
		t.Fatalf("interrupted enable failed to restore ordinary mode: %v", err)
	}
	after, err := os.ReadFile(g.options.ConfigPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rollback lost the original database: %v", err)
	}
	if cold.Enabled() || cold.PendingMigration() {
		t.Fatal("rollback did not return to original mode")
	}
	if err := cold.Enable("new retry password"); err != nil {
		t.Fatal("retry", err)
	}
	secret, err := vault.Open(g.options.VaultPath, g.options.Keystore)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok, err := secret.Get("host:one:password"); err != nil || !ok || value != "secret-account-password" {
		t.Fatalf("retry lost a secret: %v", err)
	}
}
