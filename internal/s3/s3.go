// Package s3 keeps one object in an S3-compatible bucket (AWS S3, Cloudflare R2,
// MinIO, Backblaze B2, Aliyun OSS in S3 mode): test the login, upload, and
// download. Requests are signed with AWS Signature Version 4 using only the
// standard library. It never lists or deletes.
package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"motty/internal/cloudsync"
)

// Config says where the object is and who may touch it.
type Config struct {
	// Endpoint is the service's address, such as https://s3.eu-west-1.amazonaws.com
	// or https://<account>.r2.cloudflarestorage.com. https:// only, except for a
	// loopback address or an explicit AllowInsecureHTTP opt-in.
	Endpoint string
	// Region the requests are signed for; empty means us-east-1. R2 uses "auto".
	Region string
	Bucket string
	// Key is the object, such as motty/backup.mottybak.
	Key       string
	AccessKey string
	Secret    string
	// PathStyle puts the bucket in the path (endpoint/bucket/key), as MinIO and
	// most self-hosted services want; otherwise it is the host (bucket.endpoint).
	PathStyle bool
	// AllowInsecureHTTP is the user's consent to HTTP outside loopback.
	AllowInsecureHTTP bool
}

// DefaultKey is the object name used when the user gives none.
const DefaultKey = "motty/backup.mottybak"

// Client talks to one object.
type Client struct {
	config Config
	base   *url.URL
	http   *http.Client
	// MaxBytes limits a download and an upload.
	MaxBytes int64
	// Now is the clock the signatures use; nil is time.Now.
	Now func() time.Time
}

var _ cloudsync.Store = (*Client)(nil)

const defaultMaxBytes = 64 << 20

// New checks the configuration. A nil http client does not follow redirects to
// another host. The caller controls the request deadline through its context.
func New(config Config, client *http.Client) (*Client, error) {
	base, err := url.Parse(strings.TrimSpace(config.Endpoint))
	if err != nil || base.Host == "" || base.User != nil {
		return nil, fmt.Errorf("s3: %q is not a server address", config.Endpoint)
	}
	if err := cloudsync.CheckAddress(base, config.AllowInsecureHTTP); err != nil {
		if errors.Is(err, cloudsync.ErrInsecure) {
			return nil, err
		}
		return nil, fmt.Errorf("s3: %q is not an http or https address", config.Endpoint)
	}
	config.Bucket = strings.TrimSpace(config.Bucket)
	if config.Bucket == "" || strings.ContainsAny(config.Bucket, "/\\ \x00\r\n") {
		return nil, errors.New("s3: a bucket name is required")
	}
	clean := path.Clean("/" + strings.TrimSpace(config.Key))
	if clean == "/" || strings.ContainsAny(config.Key, "\x00\r\n") {
		return nil, errors.New("s3: an object key is required")
	}
	config.Key = strings.TrimPrefix(clean, "/")
	if strings.TrimSpace(config.Region) == "" {
		config.Region = "us-east-1"
	}
	config.Region = strings.TrimSpace(config.Region)
	if config.AccessKey == "" || config.Secret == "" {
		return nil, errors.New("s3: an access key and its secret are required")
	}
	return &Client{config: config, base: base, http: cloudsync.HTTPClient(client, config.AllowInsecureHTTP), MaxBytes: defaultMaxBytes}, nil
}

// address is the object's URL. The path is encoded the way it is signed, so the
// request line and the signature agree.
func (client *Client) address() *url.URL {
	target := *client.base
	var segments []string
	if prefix := strings.Trim(client.base.Path, "/"); prefix != "" {
		segments = append(segments, strings.Split(prefix, "/")...)
	}
	if client.config.PathStyle {
		segments = append(segments, client.config.Bucket)
	} else {
		target.Host = client.config.Bucket + "." + client.base.Host
	}
	segments = append(segments, strings.Split(client.config.Key, "/")...)
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		escaped[i] = awsEscape(segment)
	}
	target.Path = "/" + strings.Join(segments, "/")
	target.RawPath = "/" + strings.Join(escaped, "/")
	target.RawQuery, target.Fragment = "", ""
	return &target
}

func (client *Client) do(ctx context.Context, method string, body []byte, header map[string]string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.address().String(), reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "motty")
	for key, value := range header {
		request.Header.Set(key, value)
	}
	payload := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(payload[:])
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	now := time.Now
	if client.Now != nil {
		now = client.Now
	}
	signV4(request, payloadHash, client.config.AccessKey, client.config.Secret, client.config.Region, "s3", now())
	return client.http.Do(request)
}

// serviceError is the XML body S3 sends with an error.
type serviceError struct {
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

// failure turns an error response into one line: "<status>: <Code>: <Message>".
// The body is read to a small limit and need not be XML.
func failure(response *http.Response) (code string, err error) {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	var parsed serviceError
	if xml.Unmarshal(data, &parsed) == nil && parsed.Code != "" {
		line := parsed.Code
		if message := strings.Join(strings.Fields(parsed.Message), " "); message != "" {
			line += ": " + message
		}
		return parsed.Code, fmt.Errorf("s3: the server answered %d %s", response.StatusCode, line)
	}
	return "", fmt.Errorf("s3: the server answered %s", response.Status)
}

// check maps the responses every request shares: a refused login (S3 answers
// 403 both for a wrong key and for a key that may not do this) is
// ErrUnauthorized, with the service's own words after it.
func check(response *http.Response) (code string, err error) {
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		code, detail := failure(response)
		return code, fmt.Errorf("%w (%s)", cloudsync.ErrUnauthorized, strings.TrimPrefix(detail.Error(), "s3: the server answered "))
	case response.StatusCode >= 300 && response.StatusCode < 400:
		// A redirect the client did not follow (another host, or too many hops)
		// is not a success: nothing was verified.
		return "", fmt.Errorf("s3: the server answered %s", response.Status)
	case response.StatusCode >= 400:
		return failure(response)
	}
	return "", nil
}

// Test checks that the bucket answers and the key is accepted. A missing object
// is fine: the first upload makes it; a missing bucket is not.
func (client *Client) Test(ctx context.Context) error {
	response, err := client.do(ctx, http.MethodGet, nil, map[string]string{"Range": "bytes=0-0"})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	code, err := check(response)
	switch {
	case response.StatusCode == http.StatusNotFound && (code == "NoSuchKey" || code == ""):
		return nil
	case response.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		return nil // an empty object
	}
	return err
}

// Get downloads the object. The returned tag is its ETag.
func (client *Client) Get(ctx context.Context) (data []byte, tag string, err error) {
	response, err := client.do(ctx, http.MethodGet, nil, nil)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	code, err := check(response)
	if response.StatusCode == http.StatusNotFound && (code == "NoSuchKey" || code == "") {
		return nil, "", cloudsync.ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("s3: the server answered %s", response.Status)
	}
	limit := client.MaxBytes
	data, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > limit {
		return nil, "", cloudsync.ErrTooLarge
	}
	return data, response.Header.Get("ETag"), nil
}

// Put uploads the object. The upload only replaces the version previous names, or
// creates the object when previous is empty (cloudsync.ErrChanged otherwise). It
// returns the ETag of what it stored.
//
// The condition is If-Match (or If-None-Match: * to create) on the PUT, which S3
// has supported since late 2024 and MinIO and R2 support too. A service that does
// not know the header answers
// NotImplemented (501, or 400 from Aliyun OSS) or ignores it; then the version is read with a HEAD and
// compared before an unconditional PUT. That fallback leaves a window of a
// round trip in which another computer can slip in; it is the best an old
// service allows.
func (client *Client) Put(ctx context.Context, data []byte, previous string) (tag string, err error) {
	if int64(len(data)) > client.MaxBytes {
		return "", cloudsync.ErrTooLarge
	}
	header := map[string]string{"Content-Type": "application/octet-stream"}
	if previous != "" {
		header["If-Match"] = previous
	} else {
		header["If-None-Match"] = "*"
	}
	response, err := client.do(ctx, http.MethodPut, data, header)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if unsupported(response) {
		return client.putAfterHead(ctx, data, previous)
	}
	switch {
	case response.StatusCode == http.StatusPreconditionFailed:
		return "", cloudsync.ErrChanged
	case previous != "" && response.StatusCode == http.StatusNotFound:
		if code, _ := failure(response); code == "NoSuchKey" {
			return "", cloudsync.ErrChanged // deleted since it was last read
		}
		return "", errors.New("s3: the bucket was not found")
	}
	if _, err := check(response); err != nil {
		return "", err
	}
	return response.Header.Get("ETag"), nil
}

// unsupported reports whether the service refused the conditional header itself.
// S3-compatible services say so differently: 501 NotImplemented from MinIO-era
// and older gateways, 400 NotImplemented from Aliyun OSS (error 0017-00000245),
// so the error code decides, not the status. The body is left readable.
func unsupported(response *http.Response) bool {
	if response.StatusCode == http.StatusNotImplemented {
		return true
	}
	if response.StatusCode != http.StatusBadRequest {
		return false
	}
	data, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	response.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(data), response.Body), response.Body}
	var parsed serviceError
	return xml.Unmarshal(data, &parsed) == nil && parsed.Code == "NotImplemented"
}

// putAfterHead is the fallback for a service without conditional writes.
func (client *Client) putAfterHead(ctx context.Context, data []byte, previous string) (string, error) {
	head, err := client.do(ctx, http.MethodHead, nil, nil)
	if err != nil {
		return "", err
	}
	head.Body.Close()
	switch {
	case head.StatusCode == http.StatusNotFound && previous == "":
		// Nothing there, which is what creating wants.
	case head.StatusCode == http.StatusNotFound:
		return "", cloudsync.ErrChanged
	case previous == "" && head.StatusCode == http.StatusOK:
		return "", cloudsync.ErrChanged
	case head.StatusCode == http.StatusForbidden || head.StatusCode == http.StatusUnauthorized:
		return "", cloudsync.ErrUnauthorized
	case head.StatusCode != http.StatusOK:
		return "", fmt.Errorf("s3: the server answered %s", head.Status)
	case head.StatusCode == http.StatusOK && head.Header.Get("ETag") != previous:
		return "", cloudsync.ErrChanged
	}
	response, err := client.do(ctx, http.MethodPut, data, map[string]string{"Content-Type": "application/octet-stream"})
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if _, err := check(response); err != nil {
		return "", err
	}
	return response.Header.Get("ETag"), nil
}
