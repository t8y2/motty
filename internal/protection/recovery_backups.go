package protection

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type recoveryBackup struct {
	Version         int      `json:"version"`
	Name            string   `json:"name"`
	Protection      envelope `json:"protection"`
	Data            []byte   `json:"data"`
	VaultKey        []byte   `json:"vault_key,omitempty"`
	ProfileKey      []byte   `json:"profile_key,omitempty"`
	ProfileMetadata []byte   `json:"profile_metadata,omitempty"`
}

// RecoveryFile includes the original bytes and the key metadata needed to
// reopen an encrypted historical database/vault. Secret byte slices must be
// cleared by the recovery caller after use. No key is written to disk here.
type RecoveryFile struct {
	Name           string
	Data           []byte
	ProtectionJSON []byte
	ProfileKey     []byte
	VaultKey       []byte
}

func OpenRecoveryFile(data []byte, password string) (RecoveryFile, error) {
	var backup recoveryBackup
	if len(data) > 256<<20 || json.Unmarshal(data, &backup) != nil || backup.Version != 1 || filepath.Base(backup.Name) != backup.Name || !validEnvelope(&backup.Protection) {
		return RecoveryFile{}, ErrCorrupt
	}
	root, err := unwrap(backup.Protection, password)
	if err != nil {
		return RecoveryFile{}, err
	}
	defer clear(root)
	plain, err := open(root, "recovery-file/"+backup.Name, backup.Data)
	if err != nil {
		return RecoveryFile{}, err
	}
	result := RecoveryFile{Name: backup.Name, Data: plain}
	if len(backup.ProfileKey) > 0 {
		result.ProfileKey, err = open(root, "recovery-profile-key/"+backup.Name, backup.ProfileKey)
		if err != nil {
			clear(result.Data)
			return RecoveryFile{}, err
		}
		result.ProtectionJSON, err = open(root, "recovery-profile-metadata/"+backup.Name, backup.ProfileMetadata)
		if err != nil {
			clear(result.Data)
			clear(result.ProfileKey)
			return RecoveryFile{}, err
		}
	}
	if len(backup.VaultKey) > 0 {
		result.VaultKey, err = open(root, "recovery-vault-key/"+backup.Name, backup.VaultKey)
		if err != nil {
			clear(result.Data)
			clear(result.ProfileKey)
			return RecoveryFile{}, err
		}
	}
	return result, nil
}

// OpenRecoveryBackup opens a historical file kept during protection migration.
// Its own password envelope keeps it recoverable after a password change or
// after protection is disabled. The old password remains necessary for it.
func OpenRecoveryBackup(data []byte, password string) ([]byte, error) {
	file, err := OpenRecoveryFile(data, password)
	clear(file.ProfileKey)
	clear(file.VaultKey)
	return file.Data, err
}

func (g *Gate) backupName(name string) bool {
	configBase, vaultBase := filepath.Base(g.options.ConfigPath), filepath.Base(g.options.VaultPath)
	return name == configBase+".bak" || strings.HasPrefix(name, configBase+".before-restore") || strings.HasPrefix(name, configBase+".corrupt") || strings.HasPrefix(name, vaultBase+".bad-") || name == vaultBase+".bak"
}

func (g *Gate) backupTarget(name string) bool {
	if g.backupName(name) {
		return true
	}
	at := strings.Index(name, ".protected")
	return at > 0 && g.backupName(name[:at])
}

func (g *Gate) prepareBackups(root []byte, password string, m *migration, stage string) error {
	entries, err := os.ReadDir(g.options.Directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !g.backupName(name) || strings.Contains(name, ".protected") {
			continue
		}
		info, err := os.Lstat(g.path(name))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("protection: refusing non-regular backup %s", name)
		}
		if info.Size() > 128<<20 {
			return errors.New("protection: historical backup is too large")
		}
		plain, err := os.ReadFile(g.path(name))
		if err != nil {
			return err
		}
		previous, err := seal(root, "migration-before/"+name, plain)
		if err != nil {
			clear(plain)
			return err
		}
		target := name + ".protected"
		for number := 2; ; number++ {
			if _, err := os.Lstat(g.path(target)); errors.Is(err, os.ErrNotExist) {
				break
			} else if err != nil {
				return err
			}
			target = fmt.Sprintf("%s.protected.%d", name, number)
		}
		var oldVaultKey []byte
		if strings.HasPrefix(name, filepath.Base(g.options.VaultPath)) && len(m.OldKey) > 0 {
			oldVaultKey, err = open(root, "migration-old-key/"+m.Stage, m.OldKey)
			if err != nil {
				clear(plain)
				return err
			}
		}
		protected := len(plain) >= 100 && string(plain[:16]) == "SQLite format 3\x00" && binary.BigEndian.Uint32(plain[60:64]) == 2
		if !protected {
			var header struct {
				Version int `json:"version"`
			}
			protected = json.Unmarshal(plain, &header) == nil && header.Version == 2
		}
		var oldRoot, metadata []byte
		if !m.Enable && protected {
			oldRoot = root
			metadata, _ = json.Marshal(m.Envelope)
		}
		data, err := makeRecoveryBackup(name, plain, password, oldVaultKey, oldRoot, metadata)
		clear(plain)
		clear(oldVaultKey)
		if err != nil {
			return err
		}
		staged := fmt.Sprintf("backup-%d.next", len(m.Steps))
		before := fmt.Sprintf("backup-%d.before", len(m.Steps))
		if err := writeAtomic(filepath.Join(stage, staged), data); err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(stage, before), previous); err != nil {
			return err
		}
		m.Steps = append(m.Steps, migrationStep{Target: target, Staged: staged}, migrationStep{Target: name, Existed: true, Previous: before, Remove: true})
	}
	return nil
}

func makeRecoveryBackup(name string, plain []byte, password string, vaultKey, profileKey, metadata []byte) ([]byte, error) {
	archiveKey := make([]byte, 32)
	if _, err := rand.Read(archiveKey); err != nil {
		return nil, err
	}
	defer clear(archiveKey)
	id, err := identifier()
	if err != nil {
		return nil, err
	}
	protection, err := makeEnvelope(id, password, archiveKey)
	if err != nil {
		return nil, err
	}
	backup := recoveryBackup{Version: 1, Name: name, Protection: protection}
	backup.Data, err = seal(archiveKey, "recovery-file/"+name, plain)
	if err != nil {
		return nil, err
	}
	if len(vaultKey) > 0 {
		backup.VaultKey, err = seal(archiveKey, "recovery-vault-key/"+name, vaultKey)
		if err != nil {
			return nil, err
		}
	}
	if len(profileKey) > 0 {
		backup.ProfileKey, err = seal(archiveKey, "recovery-profile-key/"+name, profileKey)
		if err != nil {
			return nil, err
		}
		backup.ProfileMetadata, err = seal(archiveKey, "recovery-profile-metadata/"+name, metadata)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(backup)
}

func (g *Gate) removeOrphanStages() error {
	for _, name := range g.orphanStages {
		path := g.path(name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrCorrupt
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	g.orphanStages = nil
	return nil
}

// CleanupPreparation removes only abandoned private staging directories. No
// password or active data is touched. Call before showing the cold lock screen
// while holding an exclusive profile lease, so an interrupted disable cannot
// leave an uncommitted plaintext database in its preparation directory.
func (g *Gate) CleanupPreparation() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.removeOrphanStages()
}

func (g *Gate) discoverOrphanStages() error {
	if g.envelope == nil {
		return nil
	}
	entries, err := os.ReadDir(g.options.Directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !strings.HasPrefix(name, ".motty-protection-stage-") || g.pending != nil && name == g.pending.Stage {
			continue
		}
		if info, err := os.Lstat(g.path(name)); err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(g.path(name), "owner.json"))
		if err != nil || len(data) > 1024 {
			continue
		}
		var owner struct {
			Profile string `json:"profile"`
			Enable  bool   `json:"enable"`
		}
		if json.Unmarshal(data, &owner) == nil && owner.Profile == g.envelope.Profile {
			g.orphanStages = append(g.orphanStages, name)
		}
	}
	return nil
}
