//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package linkwatch

import (
	"golang.org/x/sys/unix"
)

// subscribe opens the system's route socket and signals after each message
// about interfaces or their addresses, until done is closed. The socket's
// messages are not parsed: they only say that something moved, and Read says
// what. The returned channel closes if the socket fails.
func subscribe(done <-chan struct{}) (<-chan struct{}, error) {
	socket, err := openRouteSocket()
	if err != nil {
		return nil, err
	}
	if err := unix.SetNonblock(socket, true); err != nil {
		unix.Close(socket)
		return nil, err
	}
	// The read end of a pipe wakes the poll when the watcher stops: closing a
	// socket another goroutine is blocked on does not reliably wake it.
	var wake [2]int
	if err := unix.Pipe(wake[:]); err != nil {
		unix.Close(socket)
		return nil, err
	}
	unix.CloseOnExec(wake[0])
	unix.CloseOnExec(wake[1])
	signals := make(chan struct{}, 1)
	go func() {
		<-done
		unix.Write(wake[1], []byte{0})
	}()
	go func() {
		defer close(signals)
		defer unix.Close(wake[1])
		defer unix.Close(wake[0])
		defer unix.Close(socket)
		buffer := make([]byte, 16<<10)
		fds := []unix.PollFd{{Fd: int32(socket), Events: unix.POLLIN}, {Fd: int32(wake[0]), Events: unix.POLLIN}}
		for {
			fds[0].Revents, fds[1].Revents = 0, 0
			if _, err := unix.Poll(fds, -1); err != nil {
				if err == unix.EINTR {
					continue
				}
				return
			}
			if fds[1].Revents != 0 {
				return
			}
			if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
				return
			}
			if fds[0].Revents&unix.POLLIN == 0 {
				continue
			}
			// Drain what is queued. A full queue (ENOBUFS on Linux) means
			// messages were dropped, which is still news: the read below looks.
			for {
				if _, err := unix.Read(socket, buffer); err != nil {
					break
				}
			}
			select {
			case signals <- struct{}{}:
			default:
			}
		}
	}()
	return signals, nil
}
