// Package netclient builds the HTTP clients the application's own requests
// use. Where a request goes is the desktop's decision: an explicit proxy in
// the environment when there is one, otherwise the system's proxy settings. A
// desktop whose policy cannot be read or evaluated fails the request instead
// of sending it direct, which would bypass that policy. PAC evaluation follows
// the request's cancellation and deadline.
package netclient

import (
	"net/http"
	"time"
)

// transport is shared by every client so update checks and downloads reuse
// connections instead of each opening a pool of its own.
var transport = newTransport()

// Client returns a client whose requests follow the desktop's proxy settings.
// A zero timeout leaves each request bounded by the caller's context.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Transport: transport, Timeout: timeout}
}
