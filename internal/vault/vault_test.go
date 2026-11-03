package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// memoryKeystore is a system store in memory, which a test can take away.
type memoryKeystore struct {
	mu          sync.Mutex
	items       map[[2]string][]byte
	unavailable bool
	denied      bool
	loads       int
}

func (store *memoryKeystore) Store(service, account string, secret []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.unavailable {
		return ErrKeystoreUnavailable
	}
	if store.denied {
		return ErrKeystoreDenied
	}
	if store.items == nil {
		store.items = make(map[[2]string][]byte)
	}
	store.items[[2]string{service, account}] = append([]byte(nil), secret...)
	return nil
}

func (store *memoryKeystore) Load(service, account string) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.loads++
	switch {
	case store.unavailable:
		return nil, ErrKeystoreUnavailable
	case store.denied:
		return nil, ErrKeystoreDenied
	}
	secret, ok := store.items[[2]string{service, account}]
	if !ok {
		return nil, ErrKeystoreNotFound
	}
	return append([]byte(nil), secret...), nil
}

func (store *memoryKeystore) Delete(service, account string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.items, [2]string{service, account})
	return nil
}

func (store *memoryKeystore) has() bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	_, ok := store.items[[2]string{KeyService, KeyAccount}]
	return ok
}

func openVault(t *testing.T, keys Keystore) (*Vault, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.json")
	vault, err := Open(path, keys)
	if err != nil {
		t.Fatal(err)
	}
	return vault, path
}

func TestASavedSecretComesBackAfterTheFileIsOpenedAgain(t *testing.T) {
	keys := &memoryKeystore{}
	vault, path := openVault(t, keys)
	if got, found, err := vault.Get("password:tester@host:22"); err != nil || found || got != "" {
		t.Fatalf("an empty vault gave %q, %v, %v", got, found, err)
	}
	if err := vault.Set("password:tester@host:22", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if !keys.has() {
		t.Fatal("the data key was not put in the system's store")
	}
	info, err := os.Stat(path)
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("vault file mode = %v, %v; want 0600", info, err)
	}
	data, _ := os.ReadFile(path)
	for _, leak := range []string{"hunter2", "tester", "host", "password:"} {
		if strings.Contains(string(data), leak) {
			t.Fatalf("the file shows %q in clear", leak)
		}
	}

	again, err := Open(path, keys)
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := again.Get("password:tester@host:22")
	if err != nil || !found || got != "hunter2" {
		t.Fatalf("Get = %q, %v, %v", got, found, err)
	}
	if _, found, _ := again.Get("password:other@host:22"); found {
		t.Fatal("found a secret that was never saved")
	}
	if again.Len() != 1 || again.NeedsConversion() {
		t.Fatalf("Len %d, needs conversion %v", again.Len(), again.NeedsConversion())
	}
}

func TestApplyWithCommitFailureRestoresCiphertextWithoutTombstones(t *testing.T) {
	vault, path := openVault(t, &memoryKeystore{})
	stamp := 0
	vault.SetStamper(func() (string, error) { stamp++; return fmt.Sprintf("%04d", stamp), nil })
	if err := vault.Set("remote", "untouched"); err != nil {
		t.Fatal(err)
	}
	before, err := vault.Export()
	if err != nil {
		t.Fatal(err)
	}
	failed := errors.New("config commit failed")
	if err := vault.ApplyWithCommit([]Change{{Name: "new", Value: "secret"}}, func() error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("commit returned %v", err)
	}
	after, err := vault.Export()
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("vault rollback changed synced records: before=%v after=%v err=%v", before, after, err)
	}
	reopened, err := Open(path, vault.keys)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := reopened.Get("new"); err != nil || found {
		t.Fatalf("new secret remained after rollback: found=%v err=%v", found, err)
	}
}

func TestApplyMissingWithCommitKeepsConcurrentWinner(t *testing.T) {
	vault, _ := openVault(t, &memoryKeystore{})
	if _, found, err := vault.Get("host"); err != nil || found {
		t.Fatal("host must start without a secret")
	}
	if err := vault.Set("host", "other-window"); err != nil {
		t.Fatal(err)
	}
	applied, err := vault.ApplyMissingWithCommit([]Change{{Name: "host", Value: "imported"}}, func(count int) error {
		if count != 0 {
			t.Fatalf("applied %d despite a concurrent winner", count)
		}
		return nil
	})
	if err != nil || applied != 0 {
		t.Fatalf("applied=%d err=%v", applied, err)
	}
	if value, found, err := vault.Get("host"); err != nil || !found || value != "other-window" {
		t.Fatal("concurrent secret was overwritten")
	}
}

func TestSetReplacesAndDeleteForgets(t *testing.T) {
	vault, _ := openVault(t, &memoryKeystore{})
	for _, value := range []string{"one", "two"} {
		if err := vault.Set("a", value); err != nil {
			t.Fatal(err)
		}
	}
	if err := vault.Set("b", "bee"); err != nil {
		t.Fatal(err)
	}
	if vault.Len() != 2 {
		t.Fatalf("Len = %d, want one record per name", vault.Len())
	}
	if got, _, _ := vault.Get("a"); got != "two" {
		t.Fatalf("a = %q", got)
	}
	names, err := vault.Names()
	if err != nil || strings.Join(names, ",") != "a,b" {
		t.Fatalf("Names = %v, %v", names, err)
	}
	if err := vault.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Delete("a"); err != nil {
		t.Fatalf("deleting what is gone: %v", err)
	}
	if _, found, _ := vault.Get("a"); found || vault.Len() != 1 {
		t.Fatal("the secret survived Delete")
	}
	if err := vault.Set("", "x"); err == nil {
		t.Fatal("a nameless secret was saved")
	}
}

func TestApplyChangesAllSecretsOrNone(t *testing.T) {
	vault, _ := openVault(t, &memoryKeystore{})
	if err := vault.Apply([]Change{{Name: "a", Value: "old-a"}, {Name: "b", Value: "old-b"}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	vault.SetStamper(func() (string, error) {
		calls++
		if calls == 2 {
			return "", errors.New("stamp failed")
		}
		return fmt.Sprintf("s%04d", calls), nil
	})
	if err := vault.Apply([]Change{{Name: "a", Value: "new-a"}, {Name: "b", Delete: true}}); err == nil {
		t.Fatal("a batch with a failed second change succeeded")
	}
	if got, found, err := vault.Get("a"); err != nil || !found || got != "old-a" {
		t.Fatalf("a after failed batch = %q, %t, %v", got, found, err)
	}
	if got, found, err := vault.Get("b"); err != nil || !found || got != "old-b" {
		t.Fatalf("b after failed batch = %q, %t, %v", got, found, err)
	}

	stamps := &counter{calls: 10}
	vault.SetStamper(stamps.next)
	if err := vault.Apply([]Change{{Name: "a", Value: "new-a"}, {Name: "b", Delete: true}, {Name: "c", Value: "see"}}); err != nil {
		t.Fatal(err)
	}
	if got, found, _ := vault.Get("a"); !found || got != "new-a" {
		t.Fatalf("a after batch = %q, %t", got, found)
	}
	if _, found, _ := vault.Get("b"); found {
		t.Fatal("b survived batch deletion")
	}
	if got, found, _ := vault.Get("c"); !found || got != "see" {
		t.Fatalf("c after batch = %q, %t", got, found)
	}
}

func TestALostKeyIsReportedAndClearStartsOver(t *testing.T) {
	keys := &memoryKeystore{}
	vault, path := openVault(t, keys)
	if err := vault.Set("a", "alpha"); err != nil {
		t.Fatal(err)
	}
	keys.Delete(KeyService, KeyAccount)
	lost, _ := Open(path, keys)
	if _, _, err := lost.Get("a"); !errors.Is(err, ErrKeyLost) {
		t.Fatalf("Get with the key gone = %v", err)
	}
	if err := lost.Set("b", "bee"); !errors.Is(err, ErrKeyLost) {
		t.Fatalf("Set with the key gone = %v (the old records must not be overwritten silently)", err)
	}
	if err := lost.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := lost.Set("b", "bee"); err != nil {
		t.Fatal(err)
	}
	if got, found, _ := lost.Get("b"); !found || got != "bee" || lost.Len() != 1 {
		t.Fatalf("after Clear: %q, %v, Len %d", got, found, lost.Len())
	}
}

func TestADeniedStoreIsNotMistakenForALostKey(t *testing.T) {
	keys := &memoryKeystore{}
	vault, path := openVault(t, keys)
	if err := vault.Set("a", "alpha"); err != nil {
		t.Fatal(err)
	}
	keys.denied = true
	again, _ := Open(path, keys)
	if _, _, err := again.Get("a"); !errors.Is(err, ErrKeystoreDenied) {
		t.Fatalf("Get with the store refusing = %v", err)
	}
	keys.denied = false
	if got, _, err := again.Get("a"); err != nil || got != "alpha" {
		t.Fatalf("the vault did not recover once the store answered: %q, %v", got, err)
	}
}

func TestAChangedRecordIsRefusedAndNeverReturnedAsAnotherSecret(t *testing.T) {
	keys := &memoryKeystore{}
	vault, path := openVault(t, keys)
	if err := vault.Set("a", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Set("b", "bee"); err != nil {
		t.Fatal(err)
	}
	reopened, _ := Open(path, keys)
	// Swap the sealed parts of the two records: each is bound to its own id.
	reopened.file.Records[0].Data, reopened.file.Records[1].Data = reopened.file.Records[1].Data, reopened.file.Records[0].Data
	for _, name := range []string{"a", "b"} {
		if got, found, err := reopened.Get(name); !errors.Is(err, ErrCorrupt) || found || got != "" {
			t.Fatalf("Get(%q) on swapped records = %q, %v, %v", name, got, found, err)
		}
	}
	other, _ := Open(path, keys)
	other.file.Records[0].Data[len(other.file.Records[0].Data)-1] ^= 1
	if _, err := other.Names(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("a flipped bit = %v", err)
	}
}

func TestFilesThatAreNotVaultsAreRefused(t *testing.T) {
	dir := t.TempDir()
	for name, contents := range map[string]string{
		"garbage":     "not json",
		"version":     `{"version": 9, "records": []}`,
		"half":        `{"version": 1, "kdf": {"name":"argon2id","time":1,"memory_kib":64,"threads":1,"salt":"AAAAAAAAAAA="}, "records": []}`,
		"weak kdf":    `{"version": 1, "kdf": {"name":"md5","time":1,"memory_kib":64,"threads":1,"salt":"AAAAAAAAAAA="}, "wrapped_key": "AAAA", "records": []}`,
		"huge memory": `{"version": 1, "kdf": {"name":"argon2id","time":1,"memory_kib":4294967295,"threads":1,"salt":"AAAAAAAAAAA="}, "wrapped_key": "AAAA", "records": []}`,
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path, nil); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: Open = %v, want ErrCorrupt", name, err)
		}
	}
}

// counter is a stamper whose stamps order like time, and counts how often it is asked.
type counter struct{ calls int }

func (c *counter) next() (string, error) {
	c.calls++
	return fmt.Sprintf("s%04d", c.calls), nil
}

func stampedVault(t *testing.T) (*Vault, string, *counter) {
	t.Helper()
	vault, path := openVault(t, &memoryKeystore{})
	stamps := &counter{}
	vault.SetStamper(stamps.next)
	return vault, path, stamps
}

func mustExport(t *testing.T, vault *Vault) []Secret {
	t.Helper()
	secrets, err := vault.Export()
	if err != nil {
		t.Fatal(err)
	}
	return secrets
}

func TestDeleteWithAStamperLeavesATombstone(t *testing.T) {
	vault, _, _ := stampedVault(t)
	vault.Set("a", "alpha")
	vault.Set("b", "bee")
	if err := vault.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Delete("a"); err != nil {
		t.Fatalf("deleting a tombstone: %v", err)
	}
	if err := vault.Delete("never"); err != nil {
		t.Fatalf("deleting what never was: %v", err)
	}
	if _, found, err := vault.Get("a"); found || err != nil {
		t.Fatalf("Get of a deleted secret = %v, %v", found, err)
	}
	names, _ := vault.Names()
	if strings.Join(names, ",") != "b" || vault.Len() != 1 {
		t.Fatalf("Names %v, Len %d", names, vault.Len())
	}
	secrets := mustExport(t, vault)
	if len(secrets) != 2 || secrets[0].Name != "a" || !secrets[0].Deleted || secrets[0].Value != "" || secrets[0].Stamp != "s0003" {
		t.Fatalf("Export = %+v", secrets)
	}
	if secrets[1].Name != "b" || secrets[1].Deleted || secrets[1].Value != "bee" || secrets[1].Stamp != "s0002" {
		t.Fatalf("Export = %+v", secrets)
	}
}

func TestDeleteWithoutAStamperRemovesTheRecord(t *testing.T) {
	vault, _ := openVault(t, &memoryKeystore{})
	vault.Set("a", "alpha")
	if err := vault.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if secrets := mustExport(t, vault); len(secrets) != 0 || len(vault.file.Records) != 0 {
		t.Fatalf("a record is left: %+v", secrets)
	}
}

func TestSettingTheSameValueChangesNothingAndANewOneIsStampedLater(t *testing.T) {
	vault, path, stamps := stampedVault(t)
	vault.Set("a", "alpha")
	before, _ := os.ReadFile(path)
	if err := vault.Set("a", "alpha"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || stamps.calls != 1 {
		t.Fatalf("an unchanged Set rewrote the file or asked for a stamp (%d calls)", stamps.calls)
	}
	vault.Set("a", "alpha two")
	if secrets := mustExport(t, vault); secrets[0].Stamp != "s0002" || secrets[0].Value != "alpha two" {
		t.Fatalf("after a change: %+v", secrets)
	}
	vault.Delete("a")
	if err := vault.Set("a", "alpha two"); err != nil {
		t.Fatal(err)
	}
	got, found, _ := vault.Get("a")
	secrets := mustExport(t, vault)
	if !found || got != "alpha two" || secrets[0].Deleted || secrets[0].Stamp != "s0004" {
		t.Fatalf("a revived secret: %q, %v, %+v", got, found, secrets)
	}
}

func TestMergeTakesTheLaterStampAndIgnoresTheRest(t *testing.T) {
	vault, path, _ := stampedVault(t)
	vault.Set("live", "old")        // s0001
	vault.Set("dead", "old")        // s0002
	vault.Set("same", "mine")       // s0003
	vault.Set("newer-here", "mine") // s0004
	vault.Delete("dead")            // s0005
	vault.Set("revived", "old")     // s0006
	applied, err := vault.Merge([]Secret{
		{Name: "live", Value: "new", Stamp: "s0100"},
		{Name: "live2", Deleted: true, Stamp: "s0100"}, // a tombstone for a name never seen
		{Name: "dead", Value: "back", Stamp: "s0200"},  // live beats older tombstone
		{Name: "same", Value: "theirs", Stamp: "s0003"},
		{Name: "newer-here", Value: "theirs", Stamp: "s0001"},
		{Name: "revived", Stamp: "s0300", Deleted: true, Value: "ignored"}, // tombstone beats older live
		{Name: "", Value: "x", Stamp: "s0999"},
		{Name: "unstamped", Value: "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Secret{
		{Name: "live", Value: "new", Stamp: "s0100"},
		{Name: "live2", Deleted: true, Stamp: "s0100"},
		{Name: "dead", Value: "back", Stamp: "s0200"},
		{Name: "revived", Deleted: true, Stamp: "s0300"},
	}
	if !reflect.DeepEqual(applied, want) {
		t.Fatalf("applied = %+v", applied)
	}
	for name, value := range map[string]string{"live": "new", "dead": "back", "same": "mine", "newer-here": "mine"} {
		if got, found, _ := vault.Get(name); !found || got != value {
			t.Errorf("%s = %q, %v; want %q", name, got, found, value)
		}
	}
	if _, found, _ := vault.Get("revived"); found {
		t.Error("a tombstone did not beat an older live secret")
	}
	if _, found, _ := vault.Get("unstamped"); found {
		t.Error("an unstamped entry was taken")
	}

	unchanged, _ := os.ReadFile(path)
	if applied, err := vault.Merge([]Secret{{Name: "live", Value: "stale", Stamp: "s0050"}}); err != nil || applied != nil {
		t.Fatalf("a stale merge = %+v, %v", applied, err)
	}
	if again, _ := os.ReadFile(path); !bytes.Equal(unchanged, again) {
		t.Fatal("a merge that accepted nothing rewrote the file")
	}
}

func TestTwoVaultsThatExchangeTheirSecretsConverge(t *testing.T) {
	one, _ := openVault(t, &memoryKeystore{})
	two, _ := openVault(t, &memoryKeystore{})
	clock := &counter{}
	one.SetStamper(clock.next)
	two.SetStamper(clock.next) // one shared clock stands in for a hybrid logical clock
	one.Set("a", "one-a")
	one.Set("b", "one-b")
	two.Set("b", "two-b")
	two.Set("c", "two-c")
	one.Delete("a")
	two.Delete("c")
	two.Set("c", "two-c again")

	fromOne, fromTwo := mustExport(t, one), mustExport(t, two)
	if _, err := one.Merge(fromTwo); err != nil {
		t.Fatal(err)
	}
	if _, err := two.Merge(fromOne); err != nil {
		t.Fatal(err)
	}
	if left, right := mustExport(t, one), mustExport(t, two); !reflect.DeepEqual(left, right) {
		t.Fatalf("the vaults differ:\n%+v\n%+v", left, right)
	}
	if got, _, _ := one.Get("b"); got != "two-b" {
		t.Fatalf("b = %q, want the later write", got)
	}
	if _, found, _ := two.Get("a"); found {
		t.Fatal("the deletion of a did not reach the other vault")
	}
}

func TestExportStampsSecretsSavedBeforeStampsExisted(t *testing.T) {
	vault, path := openVault(t, &memoryKeystore{})
	vault.Set("b", "bee")
	vault.Set("a", "alpha")
	if secrets := mustExport(t, vault); secrets[0].Stamp != "" {
		t.Fatalf("a vault with no stamper stamped %+v", secrets)
	}
	stamps := &counter{}
	vault.SetStamper(stamps.next)
	secrets := mustExport(t, vault)
	if len(secrets) != 2 || secrets[0].Stamp == "" || secrets[1].Stamp == "" || secrets[0].Stamp == secrets[1].Stamp {
		t.Fatalf("Export = %+v", secrets)
	}
	reopened, _ := Open(path, vault.keys)
	reopened.SetStamper(stamps.next)
	if again := mustExport(t, reopened); !reflect.DeepEqual(again, secrets) || stamps.calls != 2 {
		t.Fatalf("the stamps were not kept: %+v after %d calls", again, stamps.calls)
	}
}

func TestPruneDropsOnlyOldTombstones(t *testing.T) {
	vault, _, _ := stampedVault(t)
	vault.Set("a", "alpha") // s0001
	vault.Set("b", "bee")   // s0002
	vault.Set("c", "sea")   // s0003
	vault.Delete("a")       // s0004
	vault.Delete("b")       // s0005
	if pruned, err := vault.Prune("s0001"); err != nil || pruned != 0 {
		t.Fatalf("Prune before any tombstone = %d, %v", pruned, err)
	}
	if pruned, err := vault.Prune("s0005"); err != nil || pruned != 1 {
		t.Fatalf("Prune = %d, %v", pruned, err)
	}
	secrets := mustExport(t, vault)
	if len(secrets) != 2 || secrets[0].Name != "b" || !secrets[0].Deleted || secrets[1].Name != "c" || secrets[1].Deleted {
		t.Fatalf("after Prune: %+v", secrets)
	}
}

func FuzzOpenNeverPanicsOnAnyFile(f *testing.F) {
	f.Add([]byte(`{"version":1,"records":[]}`))
	f.Add([]byte(`{"version":1,"kdf":{"name":"argon2id","time":1,"memory_kib":64,"threads":1,"salt":"AAAAAAAAAAA="},"wrapped_key":"AAAA","records":[{"id":"AA==","data":"AA=="}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "vault.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		vault, err := Open(path, &memoryKeystore{})
		if err != nil {
			return
		}
		_ = vault.ConvertLegacy("password")
		vault.Names()
		vault.Get("a")
	})
}

// legacyVault writes a vault the way a version with a master password did: the
// data key wrapped by a key derived from the password, and no key in any store.
func legacyVault(t *testing.T, password string, secrets map[string]string) string {
	t.Helper()
	key := bytes.Repeat([]byte{7}, keySize)
	params := kdfParams{Name: "argon2id", Time: 1, MemoryKiB: 64, Threads: 1, Salt: bytes.Repeat([]byte{3}, 16)}
	kek := deriveKey(password, &params)
	gcm, err := newGCM(kek)
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{9}, gcm.NonceSize())
	file := fileData{Version: fileVersion, KDF: &params, WrappedKey: gcm.Seal(nonce, nonce, key, []byte(wrapAssociated))}
	for name, value := range secrets {
		sealed, err := seal(key, plainRecord{Name: name, Value: value, Stamp: "s1"})
		if err != nil {
			t.Fatal(err)
		}
		file.Records = append(file.Records, sealed)
	}
	path := filepath.Join(t.TempDir(), "vault.json")
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAVaultKeptByAMasterPasswordIsConvertedOnceAndThenNeedsNoPassword(t *testing.T) {
	path := legacyVault(t, "correct horse", map[string]string{"password:ops@host:22": "hunter2", "aikey:x": "sk-1"})
	keys := &memoryKeystore{}
	vault, err := Open(path, keys)
	if err != nil {
		t.Fatal(err)
	}
	if !vault.NeedsConversion() {
		t.Fatal("a vault kept by a password does not say so")
	}
	if _, _, err := vault.Get("aikey:x"); !errors.Is(err, ErrNeedsConversion) {
		t.Fatalf("Get before converting = %v", err)
	}
	if err := vault.Set("b", "x"); !errors.Is(err, ErrNeedsConversion) {
		t.Fatalf("Set before converting = %v", err)
	}
	if err := vault.ConvertLegacy("wrong horse"); !errors.Is(err, ErrWrongPassword) || !vault.NeedsConversion() || keys.has() {
		t.Fatalf("a wrong password converted the vault: %v", err)
	}

	if err := vault.ConvertLegacy("correct horse"); err != nil {
		t.Fatal(err)
	}
	if got, found, err := vault.Get("password:ops@host:22"); err != nil || !found || got != "hunter2" {
		t.Fatalf("after converting = %q, %v, %v", got, found, err)
	}
	reopened, err := Open(path, keys)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.NeedsConversion() {
		t.Fatal("the file is still kept by the password")
	}
	if got, found, err := reopened.Get("aikey:x"); err != nil || !found || got != "sk-1" {
		t.Fatalf("a fresh start reads %q, %v, %v", got, found, err)
	}
}

func TestAConversionThatCannotKeepTheKeyLeavesTheVaultAsItWas(t *testing.T) {
	path := legacyVault(t, "correct horse", map[string]string{"a": "alpha"})
	before, _ := os.ReadFile(path)
	vault, _ := Open(path, &memoryKeystore{denied: true})
	if err := vault.ConvertLegacy("correct horse"); !errors.Is(err, ErrKeystoreDenied) {
		t.Fatalf("ConvertLegacy with the store refusing = %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || !vault.NeedsConversion() {
		t.Fatal("a failed conversion changed the vault")
	}
}

func TestAKeyFileHoldsTheKeyForASystemWithoutAStore(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "vault.key")
	vaultPath := filepath.Join(dir, "vault.json")
	keys := FallbackKeystore{System: &memoryKeystore{unavailable: true}, File: FileKeystore{Path: keyPath}}
	vault, err := Open(vaultPath, keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Set("a", "alpha"); err != nil {
		t.Fatalf("Set without a system store = %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(keyPath); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("key file = %v, %v", info, err)
		}
	}
	again, err := Open(vaultPath, FallbackKeystore{System: &memoryKeystore{unavailable: true}, File: FileKeystore{Path: keyPath}})
	if err != nil {
		t.Fatal(err)
	}
	if got, found, err := again.Get("a"); err != nil || !found || got != "alpha" {
		t.Fatalf("after a restart = %q, %v, %v", got, found, err)
	}

	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	lost, _ := Open(vaultPath, FallbackKeystore{System: &memoryKeystore{unavailable: true}, File: FileKeystore{Path: keyPath}})
	if _, _, err := lost.Get("a"); !errors.Is(err, ErrKeyLost) {
		t.Fatalf("with the key file gone = %v", err)
	}
}

func TestTheSystemStoreComesFirstAndARefusalIsNeverAnsweredWithAFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "vault.key")
	system := &memoryKeystore{}
	keys := FallbackKeystore{System: system, File: FileKeystore{Path: keyPath}}
	vault, _ := Open(filepath.Join(dir, "vault.json"), keys)
	if err := vault.Set("a", "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keyPath); !errors.Is(err, os.ErrNotExist) || !system.has() {
		t.Fatal("a key was written to a file although the system has a store")
	}

	system.denied = true
	other, _ := Open(filepath.Join(dir, "other.json"), keys)
	if err := other.Set("a", "alpha"); !errors.Is(err, ErrKeystoreDenied) {
		t.Fatalf("Set with the store refusing = %v", err)
	}
	if _, err := os.Stat(keyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refusal was answered by a key file")
	}
}

func TestAPortableKeyFileTravelsWithTheVault(t *testing.T) {
	dir := t.TempDir()
	vault, _ := Open(filepath.Join(dir, "vault.json"), FileKeystore{Path: filepath.Join(dir, "vault.key")})
	if err := vault.Set("a", "alpha"); err != nil {
		t.Fatal(err)
	}
	moved := t.TempDir()
	for _, name := range []string{"vault.json", "vault.key"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(moved, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	elsewhere, err := Open(filepath.Join(moved, "vault.json"), FileKeystore{Path: filepath.Join(moved, "vault.key")})
	if err != nil {
		t.Fatal(err)
	}
	if got, found, err := elsewhere.Get("a"); err != nil || !found || got != "alpha" {
		t.Fatalf("on another computer = %q, %v, %v", got, found, err)
	}
}
