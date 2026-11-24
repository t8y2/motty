package protection

import (
	"encoding/json"
	"errors"
	"os"

	"motty/internal/vault"
)

const deviceService = "Motty device unlock enrollment"
const deviceFile = "protection-device.json"

// The local marker avoids an OS keychain lookup for profiles that have never
// enrolled. Production stores only an opaque native envelope in the OS store.
// Copying the directory to another device therefore cannot advertise enrollment.
func (g *Gate) loadDeviceStore() error {
	if g.envelope == nil {
		return nil
	}
	data, err := os.ReadFile(g.path(deviceFile))
	if err != nil || len(data) > 1<<20 {
		return nil
	}
	var saved device
	if json.Unmarshal(data, &saved) != nil || saved.Version != 1 || saved.Profile != g.envelope.Profile {
		return nil
	}
	if store := g.options.DeviceStore; store != nil {
		value, err := store.Load(deviceService, saved.Profile)
		if err == nil && len(value) <= 1<<20 {
			g.device = value
		}
	} else {
		g.device = saved.Blob
	}
	return nil
}

// Called with g.mu held. The password slot stays independent even if the
// system store becomes unavailable. The marker and native envelope contain
// no password or plaintext data key.
func (g *Gate) persistDevice(blob []byte) error {
	if len(blob) > 1<<20 {
		return errors.New("device enrollment exceeds supported size")
	}
	path := g.path(deviceFile)
	if len(blob) == 0 {
		if g.options.DeviceStore != nil && len(g.device) > 0 {
			err := g.options.DeviceStore.Delete(deviceService, g.envelope.Profile)
			if err != nil && !errors.Is(err, vault.ErrKeystoreNotFound) {
				return err
			}
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		g.device = nil
		return nil
	}
	profile := g.envelope.Profile
	entry := device{Version: 1, Profile: profile}
	store := g.options.DeviceStore
	if store == nil {
		entry.Blob = blob
	} else if err := store.Store(deviceService, profile, blob); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err == nil {
		err = writeAtomic(path, data)
	}
	if err != nil {
		if store != nil {
			_ = store.Delete(deviceService, profile)
		}
		return err
	}
	g.device = append([]byte(nil), blob...)
	return nil
}
