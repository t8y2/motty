//go:build linux

package workbench

// openExternalURI uses xdg-open; exec.Command handles PATH lookup and errors.
func openExternalURI(link string) error {
	return runOpener("xdg-open", link)
}
