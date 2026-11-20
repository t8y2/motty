//go:build !js

package netclient

import (
	"net/http"
	"net/url"

	"github.com/vivaui/viva/app/proxy"
)

// newTransport asks the desktop which proxy carries each request. The answer
// can differ by target: hosts the settings list as exceptions go direct.
func newTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(request *http.Request) (*url.URL, error) {
		return proxy.SystemProxy(request.URL)
	}
	return transport
}
