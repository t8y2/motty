// Package datadir decides where the application keeps what it saves.
//
// There are three answers, in this order: the directory an environment variable
// names (a test run, a second profile), the folder beside the program when it is
// a portable copy, and the platform's usual place. A copy is portable when a
// writable folder named PortableName sits next to its executable; it then keeps
// everything there and touches nothing else on the machine.
package datadir

import (
	"os"
	"path/filepath"
	"sync"

	"motty/internal/buildinfo"
)

// Variable names the environment variable that moves everything the application
// keeps to another directory.
const Variable = "MOTTY_DATA_DIR"

// PortableName is the folder beside the executable that makes a copy portable.
// The name is specific on purpose: a program unpacked into Downloads or Desktop
// must not take over an unrelated "data" folder that happens to be there.
const PortableName = buildinfo.DataName + "-data"

// Location is where the data lives and why.
type Location struct {
	// Dir is the data directory. Empty means the platform's usual place.
	Dir string
	// Portable is true when Dir is the folder beside the executable. A portable
	// copy must not leave anything behind on the machine, so it keeps no key in
	// the system's secret store.
	Portable bool
}

// Resolve picks the data directory. explicit is the value of Variable and
// executable is the path of the running program; either may be empty.
func Resolve(explicit, executable string) Location {
	if explicit != "" {
		return Location{Dir: explicit}
	}
	if executable == "" {
		return Location{}
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	dir := filepath.Join(filepath.Dir(executable), PortableName)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() || !writable(dir) {
		// A folder that cannot be written is a read-only medium or a protected
		// location: the copy then behaves as an ordinary one rather than failing
		// to save anything.
		return Location{}
	}
	return Location{Dir: dir, Portable: true}
}

var (
	detectOnce sync.Once
	detected   Location
)

// Current resolves for the running process. The variable is read on every call;
// whether the copy is portable is looked up once, since it cannot change while
// the program runs.
func Current() Location {
	if explicit := os.Getenv(Variable); explicit != "" {
		return Location{Dir: explicit}
	}
	detectOnce.Do(func() {
		executable, _ := os.Executable()
		detected = Resolve("", executable)
	})
	return detected
}

func writable(dir string) bool {
	file, err := os.CreateTemp(dir, ".write-*")
	if err != nil {
		return false
	}
	name := file.Name()
	file.Close()
	return os.Remove(name) == nil
}
