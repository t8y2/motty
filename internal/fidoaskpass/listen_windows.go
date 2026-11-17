//go:build windows

package fidoaskpass

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/Microsoft/go-winio"
)

func listen(string) (ListenCloser, string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, "", err
	}
	address := `\\.\pipe\motty-fido-` + hex.EncodeToString(random[:])
	listener, err := winio.ListenPipe(address, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;OW)",
	})
	if err != nil {
		return nil, "", err
	}
	return listener, address, nil
}

func dial(address string) (Conn, error) {
	timeout := 5 * time.Second
	return winio.DialPipe(address, &timeout)
}
