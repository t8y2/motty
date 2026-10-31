//go:build darwin

package workbench

// openExternalURI hands a URL to the system's own handler; the application is
// named in full so no PATH lookup can choose another one.
func openExternalURI(link string) error {
	return runOpener("/usr/bin/open", link)
}
