package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// The standalone agent retains an in-memory host catalog. Keep it outside a
// profile migration even between the SQLite transactions it makes for requests.
// Closing the handle (including process exit) releases the operating system lock.
func profileLease(config string, exclusive bool) (*os.File, error) {
	file, err := os.OpenFile(filepath.Join(filepath.Dir(config), ".motty-profile.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockProfileFile(file, exclusive); err != nil {
		file.Close()
		return nil, fmt.Errorf("%w: %v", errProfileInUse, err)
	}
	return file, nil
}

var errProfileInUse = errors.New("another Motty process is using this profile")
