//go:build !windows

package workbench

// restrictToOwner has nothing to add off Windows: the file's 0600 mode, set by
// the caller, already keeps it to its owner.
func restrictToOwner(string) error {
	return nil
}
