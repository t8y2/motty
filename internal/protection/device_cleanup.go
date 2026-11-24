package protection

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"motty/internal/vault"
)

const cleanupDirectory = ".motty-device-cleanup"

// Only the authorization tag goes on disk. The OS entry holds its independent
// random verification key and the native reference until native revocation succeeds.
func (g *Gate) deviceDirectory() string {
	directory := g.options.Directory
	if runtime.GOOS == "windows" {
		directory = strings.ToLower(directory)
	}
	return directory
}

func (g *Gate) deviceAccount(profile string, version int) string {
	if version < 2 {
		return profile
	}
	id := sha256.Sum256([]byte(g.deviceDirectory()))
	return profile + "." + hex.EncodeToString(id[:])
}

func (g *Gate) cleanupTag(key []byte, profile string, version int) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("Motty device cleanup/v1\x00" + g.deviceDirectory() + "\x00" + g.deviceAccount(profile, version)))
	return mac.Sum(nil)
}

func (g *Gate) HasDeviceCleanup() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.hasDeviceCleanup
}

func (g *Gate) cleanupPath(profile string) string {
	return filepath.Join(g.path(cleanupDirectory), profile)
}

// prepareDeviceCleanup is part of the signed disable transaction. The original
// marker may already have been removed by a previous build's interrupted commit.
func (g *Gate) prepareDeviceCleanup(m *migration, root []byte) error {
	if m.Enable {
		return nil
	}
	for _, step := range m.Steps {
		if step.Target != deviceFile || !step.Existed {
			continue
		}
		before, err := os.ReadFile(filepath.Join(g.path(m.Stage), step.Previous))
		if err != nil {
			return err
		}
		plain, err := open(root, "migration-before/"+deviceFile, before)
		if err != nil {
			return err
		}
		defer clear(plain)
		var saved device
		if json.Unmarshal(plain, &saved) != nil || (saved.Version != 1 && saved.Version != 2) || saved.Profile != m.Envelope.Profile {
			return ErrCorrupt
		}
		intent := []byte{byte(saved.Version)}
		if g.options.DeviceStore != nil {
			key, err := g.cleanupKeyForDisable(saved, root)
			if err != nil {
				return err
			}
			defer clear(key)
			if len(key) > 0 {
				intent = append(intent, g.cleanupTag(key, saved.Profile, saved.Version)...)
			}
		}
		dir := g.path(cleanupDirectory)
		if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrCorrupt
		}
		if err := writeAtomic(g.cleanupPath(saved.Profile), intent); err != nil {
			return err
		}
		syncDirectory(g.options.Directory)
		g.hasDeviceCleanup = true
		return nil
	}
	return nil
}

func (g *Gate) cleanupKeyForDisable(saved device, root []byte) ([]byte, error) {
	if len(saved.CleanupKey) > 0 {
		key, err := open(root, "device-cleanup/"+saved.Profile, saved.CleanupKey)
		if err != nil || len(key) != sha256.Size {
			clear(key)
			return nil, ErrCorrupt
		}
		return key, nil
	}
	// Upgrade an older enrollment while the disable is authenticated. A retry
	// continues to understand the upgraded OS slot even after a rollback.
	store := g.options.DeviceStore
	entry, err := store.Load(deviceService, saved.Profile)
	if errors.Is(err, vault.ErrKeystoreNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer clear(entry)
	blob, key, err := unpackDeviceEntry(entry)
	if err != nil {
		return nil, err
	}
	if len(key) > 0 {
		return append([]byte(nil), key...), nil
	}
	key = make([]byte, sha256.Size)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	wrapped := packDeviceEntry(key, blob)
	defer clear(wrapped)
	if err := store.Store(deviceService, saved.Profile, wrapped); err != nil {
		clear(key)
		return nil, err
	}
	return key, nil
}

func (g *Gate) cancelDeviceCleanup(profile string) error {
	dir := g.path(cleanupDirectory)
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrCorrupt
	}
	if err := os.Remove(g.cleanupPath(profile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	syncDirectory(dir)
	return nil
}

// RetryDeviceCleanup is called under the profile's exclusive lease. Recheck the
// current disk generation so a restarted worker cannot revoke a restored slot.
func (g *Gate) RetryDeviceCleanup() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.retryDeviceCleanup()
}

func (g *Gate) retryDeviceCleanup() error {
	dir := g.path(cleanupDirectory)
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		g.hasDeviceCleanup = false
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrCorrupt
	}
	if _, err := os.Lstat(g.path("protection-migration.json")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var active string
	data, err := os.ReadFile(g.path("protection.json"))
	if err == nil {
		var current envelope
		if json.Unmarshal(data, &current) != nil || !validEnvelope(&current) {
			return ErrCorrupt
		}
		active = current.Profile
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if protectedMarker(g.options.ConfigPath, g.options.VaultPath) {
		return ErrCorrupt
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var result error
	for _, entry := range entries {
		profile := entry.Name()
		id, err := hex.DecodeString(profile)
		if err != nil || len(id) != 16 || profile != strings.ToLower(profile) || !entry.Type().IsRegular() {
			result = errors.Join(result, ErrCorrupt)
			continue
		}
		if profile != active {
			result = errors.Join(result, g.revokeRetiredDevice(profile))
		}
	}
	remaining, err := os.ReadDir(dir)
	if err != nil {
		return errors.Join(result, err)
	}
	g.hasDeviceCleanup = len(remaining) > 0
	if !g.hasDeviceCleanup {
		result = errors.Join(result, os.Remove(dir))
		syncDirectory(g.options.Directory)
	}
	return result
}

func (g *Gate) revokeRetiredDevice(profile string) error {
	path := g.cleanupPath(profile)
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || (info.Size() != 1 && info.Size() != 1+sha256.Size) {
		return ErrCorrupt
	}
	intent, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(intent) < 1 || (intent[0] != 1 && intent[0] != 2) {
		return ErrCorrupt
	}
	tag := intent[1:]
	if store := g.options.DeviceStore; store != nil {
		account := g.deviceAccount(profile, int(intent[0]))
		entry, err := store.Load(deviceService, account)
		if err != nil && !errors.Is(err, vault.ErrKeystoreNotFound) {
			return err
		}
		if err == nil {
			defer clear(entry)
			blob, key, err := unpackDeviceEntry(entry)
			if err != nil {
				return err
			}
			if len(key) != sha256.Size || !hmac.Equal(tag, g.cleanupTag(key, profile, int(intent[0]))) {
				return ErrCorrupt
			}
			if revoke := g.options.RevokeDevice; revoke != nil {
				if err := revoke(profile, blob); err != nil {
					return err
				}
			}
			if err := store.Delete(deviceService, account); err != nil && !errors.Is(err, vault.ErrKeystoreNotFound) {
				return err
			}
		}
	}
	// Other generations may already have enrolled in this directory.
	marker := g.path(deviceFile)
	data, err := os.ReadFile(marker)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var saved device
	if err == nil && json.Unmarshal(data, &saved) == nil && saved.Profile == profile {
		if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	syncDirectory(filepath.Dir(path))
	return nil
}
