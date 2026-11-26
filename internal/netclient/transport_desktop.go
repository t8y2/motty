//go:build !js

package netclient

import (
	"net/http"
	"net/url"

	"github.com/vivaui/viva/app/proxy"
)

var systemProxy = proxy.SystemProxyContext

// newTransport asks the desktop which proxy carries each request. The answer
// can differ by target: hosts the settings list as exceptions go direct.
func newTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = proxyForRequest
	return transport
}

func proxyForRequest(request *http.Request) (*url.URL, error) {
	return systemProxy(request.Context(), request.URL)
}

// newDirectTransport creates a transport that connects directly, bypassing
// desktop proxy lookup.
func newDirectTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return transport
}
