package protection

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"

	"motty/internal/vault"
)

const deviceService = "Motty device unlock enrollment"
const deviceFile = "protection-device.json"
const maxDeviceBlob = 1 << 20

// The marker avoids consulting the OS store on a profile that has never
// enrolled. A directory copied to another device can still use its password.
func (g *Gate) loadDeviceStore() error {
	g.device, _ = g.readDevice()
	return nil
}

func (g *Gate) readDevice() ([]byte, error) {
	if g.envelope == nil {
		return nil, nil
	}
	data, err := os.ReadFile(g.path(deviceFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var saved device
	if len(data) > maxDeviceBlob || json.Unmarshal(data, &saved) != nil || (saved.Version != 1 && saved.Version != 2) {
		return nil, ErrCorrupt
	}
	if saved.Profile != g.envelope.Profile {
		return nil, nil
	}
	g.deviceVersion = saved.Version
	return g.loadDeviceBlob(saved)
}

func (g *Gate) loadDeviceBlob(saved device) ([]byte, error) {
	if store := g.options.DeviceStore; store != nil {
		blob, err := store.Load(deviceService, g.deviceAccount(saved.Profile, saved.Version))
		if errors.Is(err, vault.ErrKeystoreNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		defer clear(blob)
		value, _, err := unpackDeviceEntry(blob)
		return append([]byte(nil), value...), err
	}
	return saved.Blob, nil
}

// DeviceForRemoval also retries an enrollment lookup that failed at startup.
// The password fallback remains available while the system store is offline.
func (g *Gate) DeviceForRemoval() ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.device) != 0 {
		return append([]byte(nil), g.device...), nil
	}
	// Older journals could remove the marker before a disable was committed.
	// Its authenticated profile still names the opaque OS slot.
	if g.options.DeviceStore != nil && g.pending != nil && !g.pending.Enable {
		for _, step := range g.pending.Steps {
			if step.Target == deviceFile && step.Existed {
				return g.loadDeviceBlob(device{Profile: g.envelope.Profile})
			}
		}
	}
	return g.readDevice()
}

// A random per-enrollment secret in the OS slot authenticates future cleanup
// requests. Older entries contain just the original native reference.
const deviceEntryPrefix = "Motty device slot v1\x00"

func packDeviceEntry(key, blob []byte) []byte {
	entry := make([]byte, 0, len(deviceEntryPrefix)+sha256.Size+len(blob))
	entry = append(entry, deviceEntryPrefix...)
	entry = append(entry, key...)
	return append(entry, blob...)
}

func unpackDeviceEntry(entry []byte) (blob, key []byte, err error) {
	if bytes.HasPrefix(entry, []byte(deviceEntryPrefix)) {
		entry = entry[len(deviceEntryPrefix):]
		if len(entry) <= sha256.Size {
			return nil, nil, ErrCorrupt
		}
		key, entry = entry[:sha256.Size], entry[sha256.Size:]
	}
	if len(entry) == 0 || len(entry) > maxDeviceBlob {
		return nil, nil, ErrCorrupt
	}
	return entry, key, nil
}

func (g *Gate) removeDeviceEnrollment(profile string) error {
	data, err := os.ReadFile(g.path(deviceFile))
	version := g.deviceVersion
	if err == nil {
		var saved device
		if json.Unmarshal(data, &saved) != nil || saved.Profile != profile || (saved.Version != 1 && saved.Version != 2) {
			return ErrCorrupt
		}
		version = saved.Version
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if version == 0 {
		return nil
	}
	if store := g.options.DeviceStore; store != nil {
		err := store.Delete(deviceService, g.deviceAccount(profile, version))
		if err != nil && !errors.Is(err, vault.ErrKeystoreNotFound) {
			return err
		}
	}
	if err := os.Remove(g.path(deviceFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	clear(g.device)
	g.device = nil
	return nil
}

// Called with g.mu held. Persisting the marker before the OS slot ensures a
// failed marker write can never remove or replace an existing enrollment.
func (g *Gate) persistDevice(blob []byte) error {
	if len(blob) > maxDeviceBlob {
		return errors.New("device enrollment exceeds supported size")
	}
	path := g.path(deviceFile)
	store := g.options.DeviceStore
	if len(blob) == 0 {
		_, err := os.Lstat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if errors.Is(err, os.ErrNotExist) && len(g.device) == 0 {
			return nil
		}
		return g.removeDeviceEnrollment(g.envelope.Profile)
	}
	existing, err := g.readDevice()
	if err != nil {
		return err
	}
	if len(existing) != 0 {
		// The system store may have come back since startup. Make the
		// original enrollment available to the next UI status refresh.
		g.device = append([]byte(nil), existing...)
	}
	if len(g.device) != 0 {
		return errors.New("protection: device unlock is already enrolled")
	}
	profile := g.envelope.Profile
	entry := device{Version: 2, Profile: profile}
	var osEntry []byte
	if store == nil {
		entry.Blob = blob
	} else {
		key := make([]byte, sha256.Size)
		defer clear(key)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		sealed, err := seal(g.key, "device-cleanup/"+profile, key)
		if err != nil {
			return err
		}
		entry.CleanupKey = sealed
		osEntry = packDeviceEntry(key, blob)
		defer clear(osEntry)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := writeAtomic(path, data); err != nil {
		return err
	}
	if store != nil {
		// With no OS slot, this marker represents an unenrolled device.
		// Keep it on store failure so a retry can check the actual result.
		if err := store.Store(deviceService, g.deviceAccount(profile, entry.Version), osEntry); err != nil {
			return err
		}
	}
	g.deviceVersion = entry.Version
	g.device = append([]byte(nil), blob...)
	return nil
}
