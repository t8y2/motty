//go:build !windows

package fidoaskpass

import (
	"net"
	"os"
	"path/filepath"
	"time"
)

func listen(dir string) (ListenCloser, string, error) {
	path := filepath.Join(dir, "askpass.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, "", err
	}
	return listener, path, nil
}

func dial(address string) (Conn, error) {
	return net.DialTimeout("unix", address, 5*time.Second)
}
