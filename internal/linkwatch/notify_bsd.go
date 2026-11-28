//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package linkwatch

import "golang.org/x/sys/unix"

// openRouteSocket opens a route socket, which carries every interface and
// address message of the system.
func openRouteSocket() (int, error) {
	socket, err := unix.Socket(unix.AF_ROUTE, unix.SOCK_RAW, unix.AF_UNSPEC)
	if err != nil {
		return -1, err
	}
	unix.CloseOnExec(socket)
	return socket, nil
}
