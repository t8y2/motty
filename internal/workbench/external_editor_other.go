//go:build !darwin && !linux && !windows

package workbench

import "errors"

// openExternalURI has nowhere to hand a URL to on this platform.
func openExternalURI(link string) error {
	return errors.New("opening a URL is not supported on this platform")
}
