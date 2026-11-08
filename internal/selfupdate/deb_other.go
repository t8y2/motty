//go:build !linux

package selfupdate

import "errors"

const DebCommand = "install-deb"

func debInstalled(string) bool  { return false }
func polkitAvailable() bool     { return false }
func (job Job) applyDeb() error { return errors.New("DEB installation is only supported on Linux") }
func RunPrivilegedDeb(string, string, string) error {
	return errors.New("DEB installation is only supported on Linux")
}
