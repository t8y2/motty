package protection

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"motty/internal/configstore"
	"motty/internal/vault"
)

type migrationStep struct {
	Target   string `json:"target"`
	Staged   string `json:"staged,omitempty"`
	Hash     string `json:"hash,omitempty"`
	Previous string `json:"previous,omitempty"`
	Existed  bool   `json:"existed"`
	Remove   bool   `json:"remove,omitempty"`
}
type migration struct {
	Version  int             `json:"version"`
	Enable   bool            `json:"enable"`
	Envelope envelope        `json:"envelope"`
	Stage    string          `json:"stage"`
	Steps    []migrationStep `json:"steps"`
	OldKey   []byte          `json:"old_key,omitempty"`
	NewKey   []byte          `json:"new_key,omitempty"`
	MAC      []byte          `json:"mac"`
}

func (m *migration) signedBytes() []byte {
	clone := *m
	clone.MAC = nil
	data, _ := json.Marshal(clone)
	return data
}

func (g *Gate) migrate(password string, enable bool) error {
	if err := configstore.EnsureClosed(g.options.ConfigPath); err != nil {
		return err
	}
	var root []byte
	var next envelope
	var err error
	if enable {
		root = make([]byte, 32)
		if _, err = rand.Read(root); err != nil {
			return err
		}
		defer clear(root)
		id, idErr := identifier()
		if idErr != nil {
			return idErr
		}
		next, err = makeEnvelope(id, password, root)
		if err != nil {
			return err
		}
	} else {
		root, err = unwrap(*g.envelope, password)
		if err != nil {
			return err
		}
		defer clear(root)
		next = *g.envelope
		if err := configstore.SetProtectionKey(g.options.ConfigPath, root); err != nil {
			return err
		}
		vault.SetProtectionKey(g.options.VaultPath, root)
	}
	stageID, err := identifier()
	if err != nil {
		return err
	}
	stageName := ".motty-protection-stage-" + stageID
	stage := g.path(stageName)
	if err := os.Mkdir(stage, 0700); err != nil {
		return err
	}
	prepared := false
	defer func() {
		if !prepared {
			_ = os.RemoveAll(stage)
		}
	}()
	owner, _ := json.Marshal(struct {
		Profile string `json:"profile"`
		Enable  bool   `json:"enable"`
	}{next.Profile, enable})
	if err := writeAtomic(filepath.Join(stage, "owner.json"), owner); err != nil {
		return err
	}
	m := &migration{Version: 1, Enable: enable, Envelope: next, Stage: stageName}
	if oldKey, err := g.options.Keystore.Load(vault.KeyService, vault.KeyAccount); err == nil {
		m.OldKey, err = seal(root, "migration-old-key/"+stageName, oldKey)
		clear(oldKey)
		if err != nil {
			return err
		}
	} else if !errors.Is(err, vault.ErrKeystoreNotFound) && !errors.Is(err, vault.ErrKeystoreUnavailable) {
		return err
	}
	configTarget := filepath.Base(g.options.ConfigPath)
	vaultTarget := filepath.Base(g.options.VaultPath)
	for _, target := range []string{configTarget, vaultTarget, "protection.json", "protection-device.json"} {
		if info, err := os.Lstat(g.path(target)); err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("protection: refusing non-regular profile file %s", target)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		step := migrationStep{Target: target, Staged: target + ".next"}
		if old, readErr := os.ReadFile(g.path(target)); readErr == nil {
			encrypted, sealErr := seal(root, "migration-before/"+target, old)
			clear(old)
			if sealErr != nil {
				return sealErr
			}
			step.Existed = true
			step.Previous = target + ".before"
			if err := writeAtomic(filepath.Join(stage, step.Previous), encrypted); err != nil {
				return err
			}
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		if target == "protection-device.json" || target == "protection.json" && !enable {
			step.Remove = true
			step.Staged = ""
		}
		m.Steps = append(m.Steps, step)
	}
	var key []byte
	if enable {
		key = root
	}
	if err := configstore.BuildCopy(g.options.ConfigPath, filepath.Join(stage, configTarget+".next"), g.options.ConfigFormat, key); err != nil {
		return err
	}
	if enable {
		if err := vault.BuildProtectedCopy(g.options.VaultPath, filepath.Join(stage, vaultTarget+".next"), g.options.Keystore, root, next.Profile); err != nil {
			return err
		}
	} else {
		targetKey, keyErr := g.normalVaultKey()
		if keyErr != nil {
			return keyErr
		}
		defer clear(targetKey)
		newKey, err := vault.BuildUnprotectedCopy(g.options.VaultPath, filepath.Join(stage, vaultTarget+".next"), g.options.Keystore, targetKey)
		if err != nil {
			return err
		}
		m.NewKey, err = seal(root, "migration-new-key/"+stageName, newKey)
		clear(newKey)
		if err != nil {
			return err
		}
	}
	if enable {
		data, _ := json.Marshal(next)
		if err := writeAtomic(filepath.Join(stage, "protection.json.next"), data); err != nil {
			return err
		}
	}
	if err := g.prepareBackups(root, password, m, stage); err != nil {
		return err
	}
	for index := range m.Steps {
		step := &m.Steps[index]
		if step.Remove {
			continue
		}
		data, err := os.ReadFile(filepath.Join(stage, step.Staged))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		clear(data)
		step.Hash = hex.EncodeToString(sum[:])
	}
	m.MAC = authenticate(root, m.signedBytes())
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := writeAtomic(g.path("protection-migration.json"), data); err != nil {
		return err
	}
	prepared = true
	g.pending = m
	g.envelope = &m.Envelope
	if err := g.finishMigration(root); err != nil {
		if rollbackErr := g.rollbackMigration(root); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("%w: %v", ErrMigrationPending, rollbackErr))
		}
		return err
	}
	if enable {
		return g.unlock(root)
	}
	return nil
}

func (g *Gate) validateMigration(root []byte) error {
	m := g.pending
	if m == nil {
		return nil
	}
	if m.Version != 1 || !strings.HasPrefix(m.Stage, ".motty-protection-stage-") || filepath.Base(m.Stage) != m.Stage || !hmac.Equal(m.MAC, authenticate(root, m.signedBytes())) {
		return fmt.Errorf("%w: migration signature or stage name", ErrCorrupt)
	}
	allowed := map[string]bool{filepath.Base(g.options.ConfigPath): true, filepath.Base(g.options.VaultPath): true, "protection.json": true, "protection-device.json": true}
	for _, step := range m.Steps {
		if (!allowed[step.Target] && !g.backupTarget(step.Target)) || filepath.Base(step.Target) != step.Target || step.Staged != "" && filepath.Base(step.Staged) != step.Staged || step.Previous != "" && filepath.Base(step.Previous) != step.Previous {
			return fmt.Errorf("%w: invalid migration target %s", ErrCorrupt, step.Target)
		}
		if info, err := os.Lstat(g.path(step.Target)); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symbolic link migration target %s", ErrCorrupt, step.Target)
		}
	}
	return nil
}

func (g *Gate) finishMigration(root []byte) error {
	if err := g.validateMigration(root); err != nil {
		return err
	}
	if err := configstore.EnsureClosed(g.options.ConfigPath); err != nil {
		return err
	}
	m := g.pending
	if m == nil {
		return nil
	}
	stage := g.path(m.Stage)
	if info, err := os.Lstat(stage); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: missing or unsafe migration stage: %v", ErrCorrupt, err)
	}
	if !m.Enable {
		key, err := open(root, "migration-new-key/"+m.Stage, m.NewKey)
		if err != nil {
			return err
		}
		err = g.storeNormalVaultKey(key)
		clear(key)
		if err != nil {
			return err
		}
	}
	for _, step := range m.Steps {
		target := g.path(step.Target)
		if step.Remove {
			if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if current, err := os.ReadFile(target); err == nil {
			sum := sha256.Sum256(current)
			clear(current)
			if hex.EncodeToString(sum[:]) == step.Hash {
				continue
			}
		}
		staged := filepath.Join(stage, step.Staged)
		data, err := os.ReadFile(staged)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		clear(data)
		if hex.EncodeToString(sum[:]) != step.Hash {
			return fmt.Errorf("%w: staged file checksum %s", ErrCorrupt, step.Target)
		}
		if err := os.Rename(staged, target); err != nil {
			return err
		}
		syncDirectory(g.options.Directory)
	}
	if m.Enable {
		// The new vault uses a different key: an old copied vault.key cannot
		// decrypt it. The shared OS entry may belong to other data directories.
		if err := os.Remove(g.path("vault.key")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := g.persistDevice(nil); err != nil {
		return err
	}
	if err := os.Remove(g.path("protection-migration.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.RemoveAll(stage)
	syncDirectory(g.options.Directory)
	g.pending = nil
	clear(g.device)
	g.device = nil
	vault.RetireProtection(g.options.VaultPath)
	vault.Forget(g.options.VaultPath)
	if m.Enable {
		copyEnvelope := m.Envelope
		g.envelope = &copyEnvelope
	} else {
		g.envelope = nil
		configstore.LockProtection(g.options.ConfigPath)
		vault.LockProtection(g.options.VaultPath)
		clear(g.key)
		g.key = nil
	}
	return nil
}

func (g *Gate) rollbackMigration(root []byte) error {
	if err := g.validateMigration(root); err != nil {
		return err
	}
	m := g.pending
	if m == nil {
		return nil
	}
	stage := g.path(m.Stage)
	for _, step := range m.Steps {
		if !step.Existed {
			if err := os.Remove(g.path(step.Target)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		sealed, err := os.ReadFile(filepath.Join(stage, step.Previous))
		if err != nil {
			return err
		}
		plain, err := open(root, "migration-before/"+step.Target, sealed)
		if err != nil {
			return err
		}
		err = writeAtomic(g.path(step.Target), plain)
		clear(plain)
		if err != nil {
			return err
		}
	}
	if len(m.OldKey) > 0 {
		key, err := open(root, "migration-old-key/"+m.Stage, m.OldKey)
		if err != nil {
			return err
		}
		err = g.storeNormalVaultKey(key)
		clear(key)
		if err != nil {
			return err
		}
	}
	if err := os.Remove(g.path("protection-migration.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.RemoveAll(stage)
	g.pending = nil
	vault.RetireProtection(g.options.VaultPath)
	vault.Forget(g.options.VaultPath)
	if m.Enable {
		g.envelope = nil
		clear(g.key)
		g.key = nil
		configstore.LockProtection(g.options.ConfigPath)
		vault.LockProtection(g.options.VaultPath)
	} else {
		restored := m.Envelope
		g.envelope = &restored
	}
	return nil
}

// Rollback restores the encrypted recovery snapshot of an interrupted migration.
// Callers hold the same exclusive profile lease required by Enable and Disable.
func (g *Gate) Rollback(password string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pending == nil {
		return nil
	}
	root, err := unwrap(g.pending.Envelope, password)
	if err != nil {
		return err
	}
	defer clear(root)
	return g.rollbackMigration(root)
}
