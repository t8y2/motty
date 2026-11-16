// Package cloudsync is what every place that can keep the encrypted backup has
// in common: one file, read, replaced only when it is still the version the
// caller saw, and a handful of errors the caller acts on. WebDAV, S3-compatible
// object storage and GitHub Gist implement Store; the application does not care
// which one it holds.
package cloudsync

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
)

// Store is one remote file.
type Store interface {
	// Test checks that the service answers and accepts the login. A file that is
	// not there yet is fine.
	Test(ctx context.Context) error
	// Get downloads the file. The tag identifies this version of it. A file that
	// is not there is ErrNotFound.
	Get(ctx context.Context) (data []byte, tag string, err error)
	// Put uploads the file. Only the version previous names is replaced, and
	// another one is ErrChanged; an empty previous means there is none yet, so a
	// file that exists is ErrChanged too. It returns the tag of what it stored, or
	// "" when the service does not say.
	Put(ctx context.Context, data []byte, previous string) (tag string, err error)
}

// Errors callers act on; test with errors.Is. The backends wrap them with
// detail, so print the error and do not compare it.
var (
	// ErrUnauthorized means the service refused the login, or the login may not
	// do this.
	ErrUnauthorized = errors.New("the service refused the login")
	// ErrNotFound means the file is not there.
	ErrNotFound = errors.New("nothing is stored there yet")
	// ErrChanged means the file is not the one the caller last saw.
	ErrChanged = errors.New("the file changed since it was last read")
	// ErrInsecure means the address would send a secret in the clear.
	ErrInsecure = errors.New("use an https:// address")
	// ErrTooLarge means the file is bigger than the service or the limit allows.
	ErrTooLarge = errors.New("the file is too large")
)

// CheckAddress requires HTTPS away from loopback unless the user explicitly
// allowed unencrypted HTTP for this server.
func CheckAddress(address *url.URL, allowInsecureHTTP bool) error {
	switch address.Scheme {
	case "https":
		return nil
	case "http":
		if allowInsecureHTTP {
			return nil
		}
		host := address.Hostname()
		if ip := net.ParseIP(host); host == "localhost" || ip != nil && ip.IsLoopback() {
			return nil
		}
		return ErrInsecure
	}
	return errors.New("not an http or https address")
}

// HTTPClient returns a copy of client (nil: a default client) that does not
// follow a redirect to another host, so a login is not handed to a server the
// user did not name. The caller's context bounds the whole operation, including
// reading the response body; an HTTP client timeout must not cut it short.
func HTTPClient(client *http.Client, allowInsecureHTTP bool) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	copied := *client
	copied.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Host != via[0].URL.Host || len(via) > 5 {
			return http.ErrUseLastResponse
		}
		if err := CheckAddress(request.URL, allowInsecureHTTP); err != nil {
			return err
		}
		return nil
	}
	return &copied
}
