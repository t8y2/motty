package x11fwd

import "sort"

// DefaultDisplay is the display to forward to when DISPLAY is unset, or ""
// when the platform gives no hint of one. An app opened from the Finder or the
// Start menu does not inherit the DISPLAY a terminal would have, so without
// this the user would have to set it by hand on every system but Linux.
//
//   - Windows: display 0 on localhost, where VcXsrv and X410 listen.
//   - macOS: the socket XQuartz's launchd job owns, which exists only while
//     XQuartz runs, so a server started after Motty is still found.
//   - elsewhere: display 0's Unix socket, when it exists.
func DefaultDisplay(goos string, glob func(pattern string) ([]string, error), exists func(path string) bool) string {
	switch goos {
	case "windows":
		return ":0"
	case "darwin":
		sockets, err := glob("/private/tmp/com.apple.launchd.*/org.xquartz:*")
		if err != nil || len(sockets) == 0 {
			return ""
		}
		sort.Strings(sockets)
		return sockets[0]
	}
	if exists("/tmp/.X11-unix/X0") {
		return ":0"
	}
	return ""
}
