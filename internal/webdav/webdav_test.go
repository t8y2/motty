package webdav

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestApprovedRemoteHTTPTransfersTheEncryptedFile(t *testing.T) {
	server, _ := newFakeServer(t)
	config := Config{URL: "http://backup.lan/dav", User: "alice", Password: "s3cret", Path: "motty/backup.mottybak"}
	if _, err := New(config, nil); !errors.Is(err, ErrInsecure) {
		t.Fatalf("unapproved remote HTTP: %v", err)
	}
	// Keep real HTTP/authentication on the wire, routed only to our local fixture.
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

// fakeServer is a WebDAV folder in memory: collections, one file store and
// basic auth, with an ETag that changes on every write.
type fakeServer struct {
	mu          sync.Mutex
	collections map[string]bool
	files       map[string][]byte
	version     int
	requests    []string
}

func newFakeServer(t *testing.T) (*httptest.Server, *fakeServer) {
	t.Helper()
	state := &fakeServer{collections: map[string]bool{"/dav": true}, files: map[string][]byte{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()
		state.requests = append(state.requests, r.Method+" "+r.URL.Path)
		if user, password, ok := r.BasicAuth(); !ok || user != "alice" || password != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case "PROPFIND":
			if !state.collections[strings.TrimSuffix(r.URL.Path, "/")] {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusMultiStatus)
		case "MKCOL":
			if state.collections[r.URL.Path] {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			parent := r.URL.Path[:strings.LastIndex(r.URL.Path, "/")]
			if !state.collections[parent] {
				w.WriteHeader(http.StatusConflict)
				return
			}
			state.collections[r.URL.Path] = true
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			parent := r.URL.Path[:strings.LastIndex(r.URL.Path, "/")]
			if !state.collections[parent] {
				w.WriteHeader(http.StatusConflict)
				return
			}
			if r.Header.Get("If-None-Match") == "*" && state.files[r.URL.Path] != nil {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			current := `"v` + strconv.Itoa(state.version) + `"`
			if match := r.Header.Get("If-Match"); match != "" && (state.files[r.URL.Path] == nil || match != current) {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			body, _ := io.ReadAll(r.Body)
			state.files[r.URL.Path] = body
			state.version++
			w.Header().Set("ETag", `"v`+strconv.Itoa(state.version)+`"`)
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			body, ok := state.files[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("ETag", `"v`+strconv.Itoa(state.version)+`"`)
			w.Write(body)
		}
	}))
	t.Cleanup(server.Close)
	return server, state
}

func client(t *testing.T, server *httptest.Server, mutate func(*Config)) *Client {
	t.Helper()
	config := Config{URL: server.URL + "/dav", User: "alice", Password: "s3cret", Path: "motty/backup.mottybak"}
	if mutate != nil {
		mutate(&config)
	}
	created, err := New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func TestUploadMakesTheFoldersAndDownloadReturnsTheBytes(t *testing.T) {
	server, state := newFakeServer(t)
	c := client(t, server, nil)
	ctx := context.Background()
	if _, _, err := c.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before any upload: %v", err)
	}
	tag, err := c.Put(ctx, []byte("sealed bytes"), "")
	if err != nil || tag == "" {
		t.Fatalf("put: %q, %v", tag, err)
	}
	if !state.collections["/dav/motty"] {
		t.Fatal("the folder was not created")
	}
	got, gotTag, err := c.Get(ctx)
	if err != nil || string(got) != "sealed bytes" || gotTag != tag {
		t.Fatalf("get: %q %q, %v", got, gotTag, err)
	}
}

func TestAnUploadDoesNotOverwriteWhatAnotherComputerStored(t *testing.T) {
	server, _ := newFakeServer(t)
	c := client(t, server, nil)
	ctx := context.Background()
	first, _ := c.Put(ctx, []byte("one"), "")
	if _, err := c.Put(ctx, []byte("two"), first); err != nil {
		t.Fatalf("replacing the version we hold: %v", err)
	}
	// first is no longer the current version.
	if _, err := c.Put(ctx, []byte("three"), first); !errors.Is(err, ErrChanged) {
		t.Fatalf("a stale upload gave %v", err)
	}
	if data, _, _ := c.Get(ctx); string(data) != "two" {
		t.Fatalf("the server holds %q", data)
	}
}

func TestTestAcceptsAMissingFolderAndRefusesBadCredentials(t *testing.T) {
	server, _ := newFakeServer(t)
	if err := client(t, server, nil).Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	missing := client(t, server, func(c *Config) { c.URL = server.URL + "/dav/not-yet" })
	if err := missing.Test(context.Background()); err != nil {
		t.Fatalf("a folder the first upload makes: %v", err)
	}
	wrong := client(t, server, func(c *Config) { c.Password = "nope" })
	if err := wrong.Test(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("bad password: %v", err)
	}
	if _, err := wrong.Put(context.Background(), []byte("x"), ""); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("bad password on upload: %v", err)
	}
}

// A server like Jianguoyun only has the top-level folders its owner made in its
// own app; MKCOL below /dav/ answers 409 and so does a PUT into the missing one.
func TestAnUploadIntoATopLevelFolderTheServerWillNotMakeNamesIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/dav/existing/") && r.Method == http.MethodPut {
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusConflict)
	}))
	t.Cleanup(server.Close)
	_, err := client(t, server, nil).Put(context.Background(), []byte("x"), "")
	if err == nil || !strings.Contains(err.Error(), `"motty"`) || !strings.Contains(err.Error(), "409") {
		t.Fatalf("the error does not name the missing folder: %v", err)
	}
	existing := client(t, server, func(c *Config) { c.Path = "existing/backup.mottybak" })
	if _, err := existing.Put(context.Background(), []byte("x"), ""); err != nil {
		t.Fatalf("a folder that is there: %v", err)
	}
}

func TestOnlySecureOrLoopbackAddressesAreAccepted(t *testing.T) {
	for address, ok := range map[string]bool{
		"https://dav.example.com/remote.php": true, "http://127.0.0.1:8080/dav": true, "http://localhost/dav": true, "http://[::1]/dav": true,
		"http://dav.example.com/dav": false, "ftp://dav.example.com": false, "dav.example.com": false, "https://user:pw@dav.example.com/": false, "": false,
	} {
		_, err := New(Config{URL: address, User: "u", Path: "a/b"}, nil)
		if (err == nil) != ok {
			t.Errorf("%q: %v", address, err)
		}
	}
	if _, err := New(Config{URL: "http://dav.example.com", Path: "a"}, nil); !errors.Is(err, ErrInsecure) {
		t.Fatalf("plain http to a remote host: %v", err)
	}
	for _, name := range []string{"", "/", "..", "a/../.."} {
		if c, err := New(Config{URL: "https://dav.example.com", Path: name}, nil); err == nil && c.config.Path != "" && strings.Contains(c.config.Path, "..") {
			t.Errorf("path %q kept a parent reference: %q", name, c.config.Path)
		}
	}
}

func TestADownloadLargerThanTheLimitIsRefused(t *testing.T) {
	server, _ := newFakeServer(t)
	c := client(t, server, nil)
	c.Put(context.Background(), []byte(strings.Repeat("x", 100)), "")
	c.MaxBytes = 50
	if _, _, err := c.Get(context.Background()); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.Put(context.Background(), []byte(strings.Repeat("x", 100)), ""); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("upload err = %v", err)
	}
}

func TestAFirstUploadDoesNotOverwriteAFileAnotherComputerCreatedFirst(t *testing.T) {
	server, _ := newFakeServer(t)
	c := client(t, server, nil)
	ctx := context.Background()
	if _, err := c.Put(ctx, []byte("mine"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Put(ctx, []byte("theirs, later"), ""); !errors.Is(err, ErrChanged) {
		t.Fatalf("creating over an existing file: %v", err)
	}
	if data, _, _ := c.Get(ctx); string(data) != "mine" {
		t.Fatalf("the server holds %q", data)
	}
}
