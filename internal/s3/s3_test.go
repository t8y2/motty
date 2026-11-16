package s3

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"motty/internal/cloudsync"
)

func TestApprovedRemoteHTTPUsesSignedRequests(t *testing.T) {
	server, _ := newFakeS3(t)
	config := Config{Endpoint: "http://backup.lan", Bucket: testBucket, Key: "backup.mottybak", AccessKey: testAccess, Secret: testSecret, PathStyle: true}
	if _, err := New(config, nil); !errors.Is(err, cloudsync.ErrInsecure) {
		t.Fatalf("unapproved remote HTTP: %v", err)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	t.Cleanup(transport.CloseIdleConnections)
	config.AllowInsecureHTTP = true
	client, err := New(config, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Put(context.Background(), []byte("encrypted backup"), ""); err != nil {
		t.Fatal(err)
	}
	data, _, err := client.Get(context.Background())
	if err != nil || string(data) != "encrypted backup" {
		t.Fatalf("download: %q, %v", data, err)
	}
}

const (
	testAccess = "AKIAMOTTYTEST"
	testSecret = "motty/secret+key/for-the-test"
	testBucket = "backups"
)

// fakeS3 behaves like the part of S3 Motty uses: one bucket, path-style
// addresses, Signature Version 4 checked on every request, and ETags that
// change with the content. Its verifier is written separately from the signer
// (it rebuilds the canonical request from what arrived on the wire), so the two
// agreeing is a check and not an echo.
type fakeS3 struct {
	mu                   sync.Mutex
	objects              map[string][]byte
	conditional          bool // supports If-Match; otherwise answers NotImplemented
	notImplementedStatus int  // 501 when zero; Aliyun OSS answers 400
	methods              []string
	badSigs              int
}

func (s *fakeS3) etag(key string) string {
	sum := md5.Sum(s.objects[key])
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func xmlError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message><RequestId>1</RequestId></Error>`, code, message)
}

// verify recomputes the signature the way the AWS documentation describes it.
func verify(r *http.Request, body []byte) bool {
	auth := r.Header.Get("Authorization")
	rest, ok := strings.CutPrefix(auth, "AWS4-HMAC-SHA256 ")
	if !ok {
		return false
	}
	fields := map[string]string{}
	for _, part := range strings.Split(rest, ", ") {
		name, value, _ := strings.Cut(part, "=")
		fields[name] = value
	}
	scope := strings.Split(fields["Credential"], "/") // key/date/region/service/aws4_request
	if len(scope) != 5 || scope[0] != testAccess || scope[3] != "s3" {
		return false
	}
	payload := sha256.Sum256(body)
	if r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(payload[:]) {
		return false
	}
	stamp := r.Header.Get("X-Amz-Date")
	if when, err := time.Parse("20060102T150405Z", stamp); err != nil || time.Since(when) > 5*time.Minute || time.Until(when) > 5*time.Minute {
		return false
	}
	names := strings.Split(fields["SignedHeaders"], ";")
	if !sort.StringsAreSorted(names) {
		return false
	}
	var lines []string
	for _, name := range names {
		value := r.Header.Get(name)
		if name == "host" {
			value = r.Host
		}
		lines = append(lines, name+":"+strings.Join(strings.Fields(value), " ")+"\n")
	}
	rawPath, rawQuery, _ := strings.Cut(r.RequestURI, "?")
	pairs := strings.Split(rawQuery, "&")
	sort.Strings(pairs)
	canonical := r.Method + "\n" + rawPath + "\n" + strings.TrimLeft(strings.Join(pairs, "&"), "&") + "\n" +
		strings.Join(lines, "") + "\n" + fields["SignedHeaders"] + "\n" + r.Header.Get("X-Amz-Content-Sha256")
	canonicalSum := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + strings.Join(scope[1:], "/") + "\n" + hex.EncodeToString(canonicalSum[:])
	key := []byte("AWS4" + testSecret)
	for _, step := range []string{scope[1], scope[2], scope[3], scope[4]} {
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(step))
		key = mac.Sum(nil)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(toSign))
	return hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(fields["Signature"]))
}

func (fake *fakeS3) unsupportedStatus() int {
	if fake.notImplementedStatus != 0 {
		return fake.notImplementedStatus
	}
	return 501
}

func newFakeS3(t *testing.T) (*httptest.Server, *fakeS3) {
	t.Helper()
	fake := &fakeS3{objects: map[string][]byte{}, conditional: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.methods = append(fake.methods, r.Method)
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(r.Header.Get("Authorization"), "Credential="+testAccess+"/") {
			xmlError(w, 403, "InvalidAccessKeyId", "The AWS Access Key Id you provided does not exist in our records.")
			return
		}
		if !verify(r, body) {
			fake.badSigs++
			xmlError(w, 403, "SignatureDoesNotMatch", "The request signature we calculated does not match the signature you provided.")
			return
		}
		bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if bucket != testBucket {
			xmlError(w, 404, "NoSuchBucket", "The specified bucket does not exist")
			return
		}
		_, exists := fake.objects[key]
		switch r.Method {
		case http.MethodPut:
			if r.Header.Get("If-None-Match") == "*" {
				if !fake.conditional {
					xmlError(w, fake.unsupportedStatus(), "NotImplemented", "A header you provided implies functionality that is not implemented")
					return
				}
				if exists {
					xmlError(w, 412, "PreconditionFailed", "At least one of the pre-conditions you specified did not hold")
					return
				}
			}
			if match := r.Header.Get("If-Match"); match != "" {
				if !fake.conditional {
					xmlError(w, fake.unsupportedStatus(), "NotImplemented", "A header you provided implies functionality that is not implemented")
					return
				}
				if !exists {
					xmlError(w, 404, "NoSuchKey", "The specified key does not exist.")
					return
				}
				if match != fake.etag(key) {
					xmlError(w, 412, "PreconditionFailed", "At least one of the pre-conditions you specified did not hold")
					return
				}
			}
			fake.objects[key] = body
			w.Header().Set("ETag", fake.etag(key))
		case http.MethodGet, http.MethodHead:
			if !exists {
				if r.Method == http.MethodHead {
					w.WriteHeader(404)
					return
				}
				xmlError(w, 404, "NoSuchKey", "The specified key does not exist.")
				return
			}
			w.Header().Set("ETag", fake.etag(key))
			if r.Method == http.MethodGet {
				w.Write(fake.objects[key])
			}
		}
	}))
	t.Cleanup(server.Close)
	return server, fake
}

func testClient(t *testing.T, server *httptest.Server, key string) *Client {
	t.Helper()
	client, err := New(Config{Endpoint: server.URL, Bucket: testBucket, Key: key, AccessKey: testAccess, Secret: testSecret, PathStyle: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestAnObjectRoundTripsUnderASignatureTheServerRecomputes(t *testing.T) {
	server, fake := newFakeS3(t)
	// A key with spaces, a non-ASCII letter and characters SigV4 encodes.
	client := testClient(t, server, "my backups/ü 1+2 (a)!.mottybak")
	ctx := context.Background()
	if err := client.Test(ctx); err != nil {
		t.Fatalf("test before the first upload: %v", err)
	}
	if _, _, err := client.Get(ctx); !errors.Is(err, cloudsync.ErrNotFound) {
		t.Fatalf("get of nothing: %v", err)
	}
	tag, err := client.Put(ctx, []byte("sealed one"), "")
	if err != nil || tag == "" {
		t.Fatalf("put: %q %v", tag, err)
	}
	data, got, err := client.Get(ctx)
	if err != nil || string(data) != "sealed one" || got != tag {
		t.Fatalf("get: %q %q %v (tag %q)", data, got, err, tag)
	}
	if err := client.Test(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.badSigs != 0 {
		t.Fatalf("%d requests failed the server's signature check", fake.badSigs)
	}
}

func TestAConditionalUploadNeverReplacesAVersionItHasNotSeen(t *testing.T) {
	server, fake := newFakeS3(t)
	client := testClient(t, server, DefaultKey)
	ctx := context.Background()
	first, _ := client.Put(ctx, []byte("one"), "")
	second, err := client.Put(ctx, []byte("two"), first)
	if err != nil || second == first {
		t.Fatalf("replacing the version it saw: %q %v", second, err)
	}
	if _, err := client.Put(ctx, []byte("three"), first); !errors.Is(err, cloudsync.ErrChanged) {
		t.Fatalf("a stale upload: %v", err)
	}
	if string(fake.objects[DefaultKey]) != "two" {
		t.Fatalf("the stale upload overwrote the object: %q", fake.objects[DefaultKey])
	}
	delete(fake.objects, DefaultKey)
	if _, err := client.Put(ctx, []byte("four"), second); !errors.Is(err, cloudsync.ErrChanged) {
		t.Fatalf("an upload over a deleted object: %v", err)
	}
}

func TestAServiceWithoutConditionalWritesIsCheckedWithAHeadFirst(t *testing.T) {
	for _, status := range []int{501, 400} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			checkHeadFallback(t, status)
		})
	}
}

func checkHeadFallback(t *testing.T, status int) {
	server, fake := newFakeS3(t)
	fake.conditional = false
	fake.notImplementedStatus = status
	client := testClient(t, server, DefaultKey)
	ctx := context.Background()
	first, _ := client.Put(ctx, []byte("one"), "")
	second, err := client.Put(ctx, []byte("two"), first)
	if err != nil || second == first {
		t.Fatalf("replacing the version it saw: %q %v", second, err)
	}
	if _, err := client.Put(ctx, []byte("three"), first); !errors.Is(err, cloudsync.ErrChanged) {
		t.Fatalf("a stale upload: %v", err)
	}
	if string(fake.objects[DefaultKey]) != "two" {
		t.Fatalf("the object is %q", fake.objects[DefaultKey])
	}
	heads := 0
	for _, method := range fake.methods {
		if method == http.MethodHead {
			heads++
		}
	}
	// One for each upload: creating, replacing, and the stale one.
	if heads != 3 {
		t.Fatalf("%d HEAD requests: %v", heads, fake.methods)
	}
}

func TestRefusalsAreOneReadableLine(t *testing.T) {
	server, _ := newFakeS3(t)
	ctx := context.Background()

	wrong, err := New(Config{Endpoint: server.URL, Bucket: testBucket, Key: "k", AccessKey: testAccess, Secret: "not the secret", PathStyle: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = wrong.Test(ctx)
	if !errors.Is(err, cloudsync.ErrUnauthorized) || !strings.Contains(err.Error(), "SignatureDoesNotMatch") || strings.ContainsAny(err.Error(), "<>\n") {
		t.Fatalf("a wrong secret: %v", err)
	}
	if _, err := wrong.Put(ctx, []byte("x"), ""); !errors.Is(err, cloudsync.ErrUnauthorized) {
		t.Fatalf("put with a wrong secret: %v", err)
	}

	missing, _ := New(Config{Endpoint: server.URL, Bucket: "nobucket", Key: "k", AccessKey: testAccess, Secret: testSecret, PathStyle: true}, nil)
	err = missing.Test(ctx)
	if err == nil || errors.Is(err, cloudsync.ErrNotFound) || !strings.Contains(err.Error(), "NoSuchBucket") || strings.ContainsAny(err.Error(), "<>\n") {
		t.Fatalf("a missing bucket must not look like an empty one: %v", err)
	}
	if _, _, err := missing.Get(ctx); errors.Is(err, cloudsync.ErrNotFound) {
		t.Fatal("a missing bucket read as no backup yet")
	}
}

func TestOnlyHTTPSOrLoopbackIsAccepted(t *testing.T) {
	base := Config{Bucket: "b", Key: "k", AccessKey: "a", Secret: "s"}
	for endpoint, wantErr := range map[string]error{
		"https://s3.example.com": nil, "http://127.0.0.1:9000": nil, "http://localhost:9000": nil,
		"http://s3.example.com": cloudsync.ErrInsecure,
	} {
		base.Endpoint = endpoint
		if _, err := New(base, nil); !errors.Is(err, wantErr) {
			t.Errorf("%s: %v", endpoint, err)
		}
	}
	base.Endpoint = "ftp://s3.example.com"
	if _, err := New(base, nil); err == nil {
		t.Error("ftp accepted")
	}
}

func TestAddressingStyle(t *testing.T) {
	config := Config{Endpoint: "https://s3.eu-west-1.amazonaws.com", Bucket: "b", Key: "/a b/c.bak", AccessKey: "a", Secret: "s"}
	virtual, _ := New(config, nil)
	if got := virtual.address().String(); got != "https://b.s3.eu-west-1.amazonaws.com/a%20b/c.bak" {
		t.Errorf("virtual-hosted: %s", got)
	}
	config.PathStyle = true
	path, _ := New(config, nil)
	if got := path.address().String(); got != "https://s3.eu-west-1.amazonaws.com/b/a%20b/c.bak" {
		t.Errorf("path-style: %s", got)
	}
}

func TestTooLargeIsRefusedBothWays(t *testing.T) {
	server, fake := newFakeS3(t)
	client := testClient(t, server, "k")
	client.MaxBytes = 10
	if _, err := client.Put(context.Background(), []byte(strings.Repeat("x", 11)), ""); !errors.Is(err, cloudsync.ErrTooLarge) {
		t.Fatalf("put: %v", err)
	}
	fake.objects["k"] = []byte(strings.Repeat("x", 11))
	if _, _, err := client.Get(context.Background()); !errors.Is(err, cloudsync.ErrTooLarge) {
		t.Fatalf("get: %v", err)
	}
}

func TestAFirstUploadDoesNotOverwriteAnObjectAnotherComputerCreatedFirst(t *testing.T) {
	for _, conditional := range []bool{true, false} {
		server, fake := newFakeS3(t)
		fake.conditional = conditional
		client := testClient(t, server, DefaultKey)
		ctx := context.Background()
		if _, err := client.Put(ctx, []byte("mine"), ""); err != nil {
			t.Fatalf("conditional=%v: creating: %v", conditional, err)
		}
		if _, err := client.Put(ctx, []byte("theirs, later"), ""); !errors.Is(err, cloudsync.ErrChanged) {
			t.Fatalf("conditional=%v: creating over an existing object: %v", conditional, err)
		}
		if string(fake.objects[DefaultKey]) != "mine" {
			t.Fatalf("conditional=%v: the object holds %q", conditional, fake.objects[DefaultKey])
		}
	}
}

func TestARedirectIsNotASuccess(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusFound, Status: "302 Found", Body: io.NopCloser(strings.NewReader(""))}
	if _, err := check(response); err == nil {
		t.Fatal("a redirect the client did not follow was reported as a working bucket")
	}
}
