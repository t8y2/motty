//go:build js

package netclient

import "net/http"

// newTransport is the default transport on the web build, which has no
// desktop proxy settings to ask.
func newTransport() *http.Transport {
	return http.DefaultTransport.(*http.Transport).Clone()
}
