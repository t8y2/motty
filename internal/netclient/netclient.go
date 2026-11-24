// Package netclient builds the HTTP clients the application's own requests
// use. Where a request goes is the desktop's decision: an explicit proxy in
// the environment when there is one, otherwise the system's proxy settings.
// A desktop whose policy cannot be read or evaluated fails the request instead
// of sending it direct, which would bypass that policy. Callers may offer
// an explicit direct retry through DirectClient when appropriate.
package netclient

import (
	"errors"
	"net/http"
	"time"

	"github.com/vivaui/viva/app/proxy"
)

// transport is shared by every client so update checks and downloads reuse
// connections instead of each opening a pool of its own.
var transport = newTransport()

// directTransport is shared by clients explicitly requested to connect
// directly, bypassing desktop proxy settings.
var directTransport = newDirectTransport()

// Client returns a client whose requests follow the desktop's proxy settings.
// A zero timeout leaves each request bounded by the caller's context.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Transport: transport, Timeout: timeout}
}

// DirectClient returns a client that connects directly, bypassing the
// desktop's proxy settings. It is used when the user explicitly chooses
// to retry a failed request directly.
func DirectClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: directTransport, Timeout: timeout}
}

// IsProxyUnavailable reports whether err was caused by failure to read or
// evaluate the desktop's proxy settings.
func IsProxyUnavailable(err error) bool {
	return errors.Is(err, proxy.ErrUnavailable)
}
