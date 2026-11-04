//go:build !windows && !darwin && !linux

package workbench

import "errors"

func revealPathInFileManager(string) error {
	return errors.New("reveal path: unsupported platform")
}
