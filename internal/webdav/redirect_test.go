package webdav

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadFollowsRedirectsWithoutSharingCredentials(test *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		test.Run(strconv.Itoa(status), func(test *testing.T) {
			destination := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				for _, header := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Referer"} {
					if value := request.Header.Get(header); value != "" {
						test.Errorf("download target received %s: %q", header, value)
					}
				}
				if request.Method != http.MethodGet || request.URL.RawQuery != "token=signed" {
					test.Errorf("download request = %s %s", request.Method, request.URL)
				}
				response.Header().Set("ETag", `"storage-version"`)
				io.WriteString(response, "sealed backup")
			}))
			test.Cleanup(destination.Close)
			source := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				user, password, ok := request.BasicAuth()
				if !ok || user != "alice" || password != "s3cret" {
					test.Errorf("WebDAV credentials = %q, %q, %v", user, password, ok)
				}
				if cookie, err := request.Cookie("session"); err != nil || cookie.Value != "private" {
					test.Errorf("WebDAV cookie = %v, %v", cookie, err)
				}
				response.Header().Set("ETag", `"dav-version"`)
				http.Redirect(response, request, destination.URL+"/download?token=signed", status)
			}))
			test.Cleanup(source.Close)
			jar, err := cookiejar.New(nil)
			if err != nil {
				test.Fatal(err)
			}
			address, err := url.Parse(source.URL)
			if err != nil {
				test.Fatal(err)
			}
			jar.SetCookies(address, []*http.Cookie{{Name: "session", Value: "private"}})
			transport := source.Client()
			transport.Jar, transport.Timeout = jar, time.Second
			dav, err := New(Config{URL: source.URL + "?access_token=private", User: "alice", Password: "s3cret", Path: "backup.mottybak"}, transport)
			if err != nil {
				test.Fatal(err)
			}
			data, tag, err := dav.Get(context.Background())
			if err != nil || string(data) != "sealed backup" || tag != `"dav-version"` {
				test.Fatalf("download = %q, %q, %v", data, tag, err)
			}
			if transport.Jar != jar || dav.http.Jar != jar || transport.Timeout != time.Second {
				test.Fatal("the download changed the shared HTTP client")
			}
		})
	}
}

func TestDownloadKeepsCredentialsForSameOriginRedirects(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if user, password, ok := request.BasicAuth(); !ok || user != "alice" || password != "s3cret" {
			test.Errorf("same-origin credentials = %q, %q, %v", user, password, ok)
		}
		if request.URL.Path != "/download" {
			response.Header().Set("ETag", `"redirect-version"`)
			http.Redirect(response, request, "/download", http.StatusFound)
			return
		}
		response.Header().Set("ETag", `"dav-version"`)
		io.WriteString(response, "sealed backup")
	}))
	test.Cleanup(server.Close)
	data, tag, err := client(test, server, nil).Get(context.Background())
	if err != nil || string(data) != "sealed backup" || tag != `"dav-version"` {
		test.Fatalf("same-origin download = %q, %q, %v", data, tag, err)
	}
}

func TestDownloadNeverRestoresCredentialsAfterLeavingTheOrigin(test *testing.T) {
	var sourceURL string
	destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
			test.Error("the intermediate target received credentials")
		}
		http.Redirect(response, request, sourceURL+"/returned", http.StatusFound)
	}))
	test.Cleanup(destination.Close)
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/returned" {
			if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
				test.Error("the redirect chain restored credentials on returning to the origin")
			}
			response.Header().Set("ETag", `"unrelated-version"`)
			io.WriteString(response, "sealed backup")
			return
		}
		response.Header().Set("ETag", `"dav-version"`)
		http.Redirect(response, request, destination.URL, http.StatusFound)
	}))
	test.Cleanup(source.Close)
	sourceURL = source.URL
	data, tag, err := client(test, source, nil).Get(context.Background())
	if err != nil || string(data) != "sealed backup" || tag != `"dav-version"` {
		test.Fatalf("returning download = %q, %q, %v", data, tag, err)
	}
}

func TestDownloadRequiresAWebDAVVersionInsteadOfTheStorageETag(test *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("ETag", `"storage-version"`)
		io.WriteString(response, "sealed backup")
	}))
	test.Cleanup(destination.Close)
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, destination.URL, http.StatusFound)
	}))
	test.Cleanup(source.Close)
	data, tag, err := client(test, source, nil).Get(context.Background())
	if err == nil || !strings.Contains(err.Error(), "native proxy") || data != nil || tag != "" {
		test.Fatalf("download without a WebDAV version = %q, %q, %v", data, tag, err)
	}
}

func TestRedirectedDownloadUsesWebDAVPropertiesForConditionalWrites(test *testing.T) {
	var version, probes, downloads atomic.Int32
	version.Store(1)
	destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		downloads.Add(1)
		response.Header().Set("ETag", `"storage-version"`)
		io.WriteString(response, "sealed backup")
	}))
	test.Cleanup(destination.Close)
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			http.Redirect(response, request, destination.URL, http.StatusFound)
		case "PROPFIND":
			probes.Add(1)
			if request.Header.Get("Depth") != "0" || request.URL.Path != "/dav/motty/backup.mottybak" {
				test.Errorf("version probe = %s, depth %q", request.URL.Path, request.Header.Get("Depth"))
			}
			if user, password, ok := request.BasicAuth(); !ok || user != "alice" || password != "s3cret" {
				test.Error("the WebDAV version probe lost its credentials")
			}
			response.WriteHeader(http.StatusMultiStatus)
			fmt.Fprintf(response, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/motty/backup.mottybak</d:href><d:propstat><d:prop><d:getetag>"v%d"</d:getetag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`, version.Load())
		case "MKCOL":
			response.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodPut:
			if request.Header.Get("If-Match") != fmt.Sprintf(`"v%d"`, version.Load()) {
				response.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			response.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version.Add(1)))
			response.WriteHeader(http.StatusCreated)
		}
	}))
	test.Cleanup(source.Close)
	dav := client(test, source, nil)
	data, tag, err := dav.Get(context.Background())
	if err != nil || string(data) != "sealed backup" || tag != `"v1"` {
		test.Fatalf("download with WebDAV properties = %q, %q, %v", data, tag, err)
	}
	if probes.Load() != 2 || downloads.Load() != 1 {
		test.Fatalf("version probes = %d, downloads = %d", probes.Load(), downloads.Load())
	}
	if updated, err := dav.Put(context.Background(), []byte("merged backup"), tag); err != nil || updated != `"v2"` {
		test.Fatalf("conditional upload = %q, %v", updated, err)
	}
	if _, err := dav.Put(context.Background(), []byte("stale backup"), tag); !errors.Is(err, ErrChanged) || version.Load() != 2 {
		test.Fatalf("stale upload = %v, version = %d", err, version.Load())
	}
}

func TestRedirectedDownloadRejectsAFileChangedDuringTheDownload(test *testing.T) {
	var version atomic.Int32
	version.Store(1)
	destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		version.Store(2)
		io.WriteString(response, "older sealed backup")
	}))
	test.Cleanup(destination.Close)
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == "PROPFIND" {
			response.WriteHeader(http.StatusMultiStatus)
			fmt.Fprintf(response, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/motty/backup.mottybak</d:href><d:propstat><d:prop><d:getetag>"v%d"</d:getetag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`, version.Load())
			return
		}
		http.Redirect(response, request, destination.URL, http.StatusFound)
	}))
	test.Cleanup(source.Close)
	data, tag, err := client(test, source, nil).Get(context.Background())
	if !errors.Is(err, ErrChanged) || data != nil || tag != "" {
		test.Fatalf("changed download = %q, %q, %v", data, tag, err)
	}
}

func TestRedirectedDownloadRejectsUnusableWebDAVProperties(test *testing.T) {
	valid := `<d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/motty/backup.mottybak</d:href><d:propstat><d:prop><d:getetag>"v1"</d:getetag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`
	for _, fixture := range []struct {
		name, body string
	}{
		{"malformed XML", "<not-xml"},
		{"failed property", strings.Replace(valid, "200 OK", "404 Not Found", 1)},
		{"different file", strings.Replace(valid, "backup.mottybak", "other.mottybak", 1)},
		{"different origin", strings.Replace(valid, "/dav/motty/backup.mottybak", "https://other.invalid/dav/motty/backup.mottybak", 1)},
		{"wrong namespace", strings.Replace(valid, "DAV:", "other:", 1)},
		{"missing version", strings.Replace(valid, `"v1"`, "", 1)},
		{"oversized properties", strings.Repeat("x", 1<<17)},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			var downloads atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				downloads.Add(1)
				io.WriteString(response, "sealed backup")
			}))
			test.Cleanup(destination.Close)
			source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Method == "PROPFIND" {
					response.WriteHeader(http.StatusMultiStatus)
					io.WriteString(response, fixture.body)
					return
				}
				http.Redirect(response, request, destination.URL, http.StatusFound)
			}))
			test.Cleanup(source.Close)
			data, tag, err := client(test, source, nil).Get(context.Background())
			if err == nil || data != nil || tag != "" || downloads.Load() != 0 {
				test.Fatalf("unusable properties accepted: data=%q, tag=%q, error=%v, downloads=%d", data, tag, err, downloads.Load())
			}
			if fixture.name == "oversized properties" && !errors.Is(err, ErrTooLarge) {
				test.Fatalf("oversized properties = %v, want ErrTooLarge", err)
			}
		})
	}
}

func TestRedirectedDownloadAcceptsCanonicalWebDAVPropertyPaths(test *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		io.WriteString(response, "sealed backup")
	}))
	test.Cleanup(destination.Close)
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/canonical/backup.mottybak" {
			http.Redirect(response, request, "/canonical/backup.mottybak", http.StatusTemporaryRedirect)
			return
		}
		if request.Method == "PROPFIND" {
			response.WriteHeader(http.StatusMultiStatus)
			io.WriteString(response, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>/canonical/backup.mottybak</d:href><d:propstat><d:prop><d:getetag>"dav-version"</d:getetag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`)
			return
		}
		http.Redirect(response, request, destination.URL, http.StatusFound)
	}))
	test.Cleanup(source.Close)
	data, tag, err := client(test, source, nil).Get(context.Background())
	if err != nil || string(data) != "sealed backup" || tag != `"dav-version"` {
		test.Fatalf("canonical download = %q, %q, %v", data, tag, err)
	}
}

func TestRedirectedDownloadPreservesPropertyRequestErrors(test *testing.T) {
	for _, fixture := range []struct {
		name   string
		status int
		after  bool
	}{
		{"missing before download", http.StatusNotFound, false},
		{"missing after download", http.StatusNotFound, true},
		{"server error before download", http.StatusServiceUnavailable, false},
		{"server error after download", http.StatusServiceUnavailable, true},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			var probes, downloads atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				downloads.Add(1)
				io.WriteString(response, "sealed backup")
			}))
			test.Cleanup(destination.Close)
			source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Method != "PROPFIND" {
					http.Redirect(response, request, destination.URL, http.StatusFound)
					return
				}
				if probes.Add(1) == 1 && fixture.after {
					response.WriteHeader(http.StatusMultiStatus)
					io.WriteString(response, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/motty/backup.mottybak</d:href><d:propstat><d:prop><d:getetag>"dav-version"</d:getetag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`)
					return
				}
				response.WriteHeader(fixture.status)
			}))
			test.Cleanup(source.Close)
			data, tag, err := client(test, source, nil).Get(context.Background())
			if err == nil || data != nil || tag != "" {
				test.Fatalf("failed property request accepted: %q, %q, %v", data, tag, err)
			}
			if fixture.status == http.StatusNotFound && !errors.Is(err, ErrNotFound) {
				test.Fatalf("missing backup = %v, want ErrNotFound", err)
			}
			if fixture.status == http.StatusServiceUnavailable && (!strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "native proxy")) {
				test.Fatalf("server error lost its status or gave configuration advice: %v", err)
			}
			if fixture.after && downloads.Load() != 1 || !fixture.after && downloads.Load() != 0 {
				test.Fatalf("downloads = %d for after=%v", downloads.Load(), fixture.after)
			}
		})
	}
}

func TestDownloadBoundsRedirectLoops(test *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		http.Redirect(response, request, "/loop", http.StatusFound)
	}))
	test.Cleanup(server.Close)
	if _, _, err := client(test, server, nil).Get(context.Background()); err == nil {
		test.Fatal("the redirect loop succeeded")
	}
	if count := requests.Load(); count != 6 {
		test.Fatalf("redirect loop made %d requests, want 6", count)
	}
}

func TestDownloadRefusesUnsafeRedirectAddresses(test *testing.T) {
	var requests atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		io.WriteString(response, "sealed backup")
	}))
	test.Cleanup(destination.Close)
	for _, fixture := range []struct {
		name, target string
		insecure     bool
	}{
		{"unapproved HTTP", "http://backup.invalid/download", true},
		{"unsupported scheme", "ftp://backup.invalid/download", false},
		{"URL credentials", strings.Replace(destination.URL, "://", "://other:secret@", 1), false},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				http.Redirect(response, request, fixture.target, http.StatusFound)
			}))
			test.Cleanup(server.Close)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, _, err := client(test, server, nil).Get(ctx)
			if err == nil {
				test.Fatal("an unsafe redirect succeeded")
			}
			if fixture.insecure && !errors.Is(err, ErrInsecure) {
				test.Fatalf("unapproved HTTP redirect = %v, want ErrInsecure", err)
			}
		})
	}
	if requests.Load() != 0 {
		test.Fatal("the redirect target with URL credentials was contacted")
	}
}

func TestDownloadRefusesHTTPSDowngradesEvenWithHTTPConsent(test *testing.T) {
	var requests atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		io.WriteString(response, "sealed backup")
	}))
	test.Cleanup(destination.Close)
	source := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, destination.URL, http.StatusFound)
	}))
	test.Cleanup(source.Close)
	dav, err := New(Config{URL: source.URL, Path: "backup.mottybak", AllowInsecureHTTP: true}, source.Client())
	if err != nil {
		test.Fatal(err)
	}
	if _, _, err := dav.Get(context.Background()); !errors.Is(err, ErrInsecure) {
		test.Fatalf("HTTPS downgrade = %v, want ErrInsecure", err)
	}
	if requests.Load() != 0 {
		test.Fatal("the insecure redirect target was contacted")
	}
}

func TestWriteAndConnectionChecksStillRefuseCrossOriginRedirects(test *testing.T) {
	for _, operation := range []string{"test", "prepare", "put"} {
		test.Run(operation, func(test *testing.T) {
			var requests atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				response.WriteHeader(http.StatusOK)
			}))
			test.Cleanup(destination.Close)
			source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				http.Redirect(response, request, destination.URL, http.StatusTemporaryRedirect)
			}))
			test.Cleanup(source.Close)
			dav := client(test, source, nil)
			var err error
			switch operation {
			case "test":
				err = dav.Test(context.Background())
			case "prepare":
				err = dav.Prepare(context.Background())
			case "put":
				_, err = dav.Put(context.Background(), []byte("sealed backup"), "")
			}
			if err == nil || requests.Load() != 0 {
				test.Fatalf("%s followed the redirect: error=%v, target requests=%d", operation, err, requests.Load())
			}
		})
	}
}

func TestRedirectedDownloadsKeepStatusAndSizeChecks(test *testing.T) {
	for _, fixture := range []struct {
		name   string
		status int
		want   error
	}{
		{"unauthorized", http.StatusUnauthorized, ErrUnauthorized},
		{"forbidden", http.StatusForbidden, ErrUnauthorized},
		{"missing", http.StatusNotFound, ErrNotFound},
		{"too large", http.StatusOK, ErrTooLarge},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.WriteHeader(fixture.status)
				io.WriteString(response, strings.Repeat("x", 100))
			}))
			test.Cleanup(destination.Close)
			source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("ETag", `"dav-version"`)
				http.Redirect(response, request, destination.URL, http.StatusFound)
			}))
			test.Cleanup(source.Close)
			dav := client(test, source, nil)
			dav.MaxBytes = 50
			if _, _, err := dav.Get(context.Background()); !errors.Is(err, fixture.want) {
				test.Fatalf("download error = %v, want %v", err, fixture.want)
			}
		})
	}
}

func TestRedirectedDownloadBodyObeysCancellation(test *testing.T) {
	started := make(chan struct{})
	destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusOK)
		response.(http.Flusher).Flush()
		close(started)
		<-request.Context().Done()
	}))
	test.Cleanup(destination.Close)
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("ETag", `"dav-version"`)
		http.Redirect(response, request, destination.URL, http.StatusFound)
	}))
	test.Cleanup(source.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dav := client(test, source, nil)
	finished := make(chan error, 1)
	go func() {
		_, _, err := dav.Get(ctx)
		finished <- err
	}()
	select {
	case <-started:
		cancel()
	case err := <-finished:
		test.Fatalf("download ended before reaching its redirected body: %v", err)
	case <-ctx.Done():
		test.Fatal("the redirected download never started")
	}
	if err := <-finished; !errors.Is(err, context.Canceled) {
		test.Fatalf("canceled body read = %v", err)
	}
}
