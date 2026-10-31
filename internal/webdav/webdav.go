// Package webdav is the small part of WebDAV needed to keep one file on a
// server: test the connection, upload, and download. It never lists or deletes.
package webdav

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"motty/internal/cloudsync"
)

// Config says where the file is and who may touch it.
type Config struct {
	// URL is the folder's address on the server. Remote HTTP requires explicit
	// consent because the password travels with every request.
	URL      string
	User     string
	Password string
	// Path is the file under URL, such as motty/backup.mottybak. Folders on the
	// way are created on upload.
	Path string
	// AllowInsecureHTTP is the user's consent to HTTP outside loopback.
	AllowInsecureHTTP bool
}

// Errors callers act on; test with errors.Is. They are the ones every cloud
// sync backend shares (package cloudsync).
var (
	ErrUnauthorized = cloudsync.ErrUnauthorized
	ErrNotFound     = cloudsync.ErrNotFound
	ErrChanged      = cloudsync.ErrChanged
	ErrInsecure     = cloudsync.ErrInsecure
	ErrTooLarge     = cloudsync.ErrTooLarge
)

var _ cloudsync.Store = (*Client)(nil)

// Client talks to one server.
type Client struct {
	config Config
	base   *url.URL
	http   *http.Client
	// MaxBytes limits a download.
	MaxBytes int64
}

const defaultMaxBytes = 64 << 20

// New checks the configuration. A nil http client uses one with a 60 second
// timeout that does not follow redirects to another host, so the password is
// not handed to a server the user did not name.
func New(config Config, client *http.Client) (*Client, error) {
	base, err := url.Parse(strings.TrimSpace(config.URL))
	if err != nil || base.Host == "" || base.User != nil {
		return nil, fmt.Errorf("webdav: %q is not a server address", config.URL)
	}
	if err := cloudsync.CheckAddress(base, config.AllowInsecureHTTP); err != nil {
		if errors.Is(err, ErrInsecure) {
			return nil, err
		}
		return nil, fmt.Errorf("webdav: %q is not an http or https address", config.URL)
	}
	clean := path.Clean("/" + strings.TrimSpace(config.Path))
	if clean == "/" || strings.ContainsAny(config.Path, "\x00\r\n") {
		return nil, errors.New("webdav: a file name is required")
	}
	config.Path = strings.TrimPrefix(clean, "/")
	return &Client{config: config, base: base, http: cloudsync.HTTPClient(client, config.AllowInsecureHTTP), MaxBytes: defaultMaxBytes}, nil
}

func (client *Client) address(parts ...string) string {
	target := *client.base
	target.Path = strings.TrimSuffix(target.Path, "/") + "/" + strings.Join(parts, "/")
	return target.String()
}

func (client *Client) do(ctx context.Context, method, address string, body []byte, header map[string]string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth(client.config.User, client.config.Password)
	request.Header.Set("User-Agent", "motty")
	for key, value := range header {
		request.Header.Set(key, value)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		response.Body.Close()
		return nil, ErrUnauthorized
	}
	return response, nil
}

// Test checks that the server answers and accepts the user. A folder that does
// not exist yet is fine: the first upload makes it.
func (client *Client) Test(ctx context.Context) error {
	response, err := client.do(ctx, "PROPFIND", client.base.String(), nil, map[string]string{"Depth": "0"})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusMultiStatus, response.StatusCode == http.StatusOK, response.StatusCode == http.StatusNotFound:
		return nil
	}
	return fmt.Errorf("webdav: the server answered %s", response.Status)
}

// Get downloads the file. The returned tag identifies this version of it.
func (client *Client) Get(ctx context.Context) (data []byte, tag string, err error) {
	response, err := client.do(ctx, http.MethodGet, client.address(strings.Split(client.config.Path, "/")...), nil, nil)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound:
		return nil, "", ErrNotFound
	case response.StatusCode != http.StatusOK:
		return nil, "", fmt.Errorf("webdav: the server answered %s", response.Status)
	}
	limit := client.MaxBytes
	data, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > limit {
		return nil, "", ErrTooLarge
	}
	return data, response.Header.Get("ETag"), nil
}

// Put uploads the file, making the folders above it first. The upload only
// replaces the version previous names, or creates the file when previous is empty,
// so a file another computer uploaded in the meantime is not overwritten without
// the caller knowing (ErrChanged). It returns the tag of what it stored, when the
// server says.
func (client *Client) Put(ctx context.Context, data []byte, previous string) (tag string, err error) {
	if int64(len(data)) > client.MaxBytes {
		return "", ErrTooLarge
	}
	parts := strings.Split(client.config.Path, "/")
	// refused remembers the first folder the server would neither make nor
	// report as existing, so a PUT into it can say what is missing.
	var refused, refusal string
	for depth := 1; depth < len(parts); depth++ {
		response, err := client.do(ctx, "MKCOL", client.address(parts[:depth]...), nil, nil)
		if err != nil {
			return "", err
		}
		response.Body.Close()
		// 201 made it, 405 is "already there"; anything else surfaces at the PUT.
		if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusMethodNotAllowed && refused == "" {
			refused, refusal = strings.Join(parts[:depth], "/"), response.Status
		}
	}
	header := map[string]string{"Content-Type": "application/octet-stream"}
	if previous != "" {
		header["If-Match"] = previous
	} else {
		header["If-None-Match"] = "*"
	}
	response, err := client.do(ctx, http.MethodPut, client.address(parts...), data, header)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusPreconditionFailed:
		return "", ErrChanged
	case response.StatusCode == http.StatusConflict && refused != "":
		return "", fmt.Errorf("webdav: the folder %q does not exist and the server refused to create it (%s); create it on the server first", refused, refusal)
	case response.StatusCode < 200 || response.StatusCode > 299:
		return "", fmt.Errorf("webdav: the server answered %s", response.Status)
	}
	return response.Header.Get("ETag"), nil
}
