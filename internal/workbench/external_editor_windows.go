//go:build windows

package workbench

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// openExternalURI hands a URL to the shell, which routes it to the application
// that registered the scheme. VS Code claims vscode:// only once it has been
// started, so a fresh install answers ERROR_NOT_SUPPORTED until then.
func openExternalURI(link string) error {
	target, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, nil, target, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("open %s: %w", link, err)
	}
	return nil
}
