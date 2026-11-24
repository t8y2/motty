package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"motty/internal/configstore"
	"motty/internal/protection"
)

// The standalone agent retains an in-memory host catalog. Keep it outside a
// profile migration even between the SQLite transactions it makes for requests.
// Closing the handle (including process exit) releases the operating system lock.
func profileLease(config string, exclusive bool) (*os.File, error) {
	file, err := os.OpenFile(profileLeasePath(config), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockProfileFile(file, exclusive); err != nil {
		file.Close()
		return nil, fmt.Errorf("%w: %v", errProfileInUse, err)
	}
	return file, nil
}

func profileLeasePath(config string) string {
	return filepath.Join(filepath.Dir(config), ".motty-profile.lock")
}

// A migration can have a readable ordinary SQLite file while its journal
// still requires authentication. Consult the same disk state as a cold GUI.
// Opening the gate here is read-only and never installs or clears a key.
func requireOrdinaryProfile(config string) error {
	gate, err := protection.Open(protection.Options{ConfigPath: config})
	if err != nil {
		return err
	}
	if gate.Enabled() || gate.PendingMigration() {
		return configstore.ErrLocked
	}
	return nil
}

var errProfileInUse = errors.New("another Motty process is using this profile")
