package completion

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewerComparesNumericVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.9.0", "v1.8.1", true},
		{"v1.8.1", "v1.8.1", false},
		{"v1.8.0", "v1.10.0", false},
		{"v2", "v1.99.99", true},
		{"v1.9.0-rc1", "v1.8.1", false}, // a prerelease suffix is not installed
		{"nightly", "v1.8.1", false},
		{"", "v1.8.1", false},
	}
	for _, test := range cases {
		if got := Newer(test.a, test.b); got != test.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", test.a, test.b, got, test.want)
		}
	}
}

func TestCatalogPathSitsBesideTheConfigurationStore(t *testing.T) {
	path := CatalogPath(filepath.Join("/data", "config.db"))
	if want := filepath.Join("/data", "completion", FileName); path != want {
		t.Fatalf("CatalogPath = %q, want %q", path, want)
	}
	if CatalogPath("") != "" {
		t.Fatal("an empty store path must have no installed catalog")
	}
}

func TestInstallCatalogReplacesAtomicallyAndLoadsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "completion", FileName)
	catalog := testCatalog(t)
	if err := InstallCatalog(path, catalog); err != nil {
		t.Fatalf("install: %v", err)
	}
	loaded, err := LoadInstalled(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !bytes.Equal(loaded.Encode(), catalog.Encode()) {
		t.Fatal("the loaded catalog differs from the installed one")
	}
	// A damaged file is refused rather than half read; the caller then keeps
	// the built-in snapshot.
	if err := os.WriteFile(path, []byte("motty-completion\t1\ntruncated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadInstalled(path); err == nil {
		t.Fatal("a damaged catalog was accepted")
	}
}

func TestInstallCatalogFailuresPreserveExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	catalog := testCatalog(t)
	if err := InstallCatalog(path, catalog); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := installCatalog(ctx, path, catalog); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled installation = %v", err)
	}
	if err := InstallCatalog(filepath.Join(path, FileName), catalog); err == nil {
		t.Fatal("an invalid parent directory was accepted")
	}
	loaded, err := LoadInstalled(path)
	if err != nil || !bytes.Equal(loaded.Encode(), catalog.Encode()) {
		t.Fatalf("failed installation changed the file: %v", err)
	}
	if err := InstallCatalog("", catalog); err == nil {
		t.Fatal("an empty catalog path was accepted")
	}
	// A directory at the destination makes the final replacement fail after
	// the temporary file has been fully written; that file must be removed.
	dir := t.TempDir()
	if err := InstallCatalog(dir, catalog); err == nil {
		t.Fatal("an installation replaced a directory")
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "catalog-") && strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary catalog was left behind: %s", entry.Name())
		}
	}
}

func TestInstallCatalogUsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits to check")
	}
	dir := filepath.Join(t.TempDir(), "completion")
	path := filepath.Join(dir, FileName)
	if err := InstallCatalog(path, testCatalog(t)); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mode := directory.Mode().Perm(); mode != 0o700 {
		t.Fatalf("catalog directory mode = %o, want 700", mode)
	}
	file, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := file.Mode().Perm(); mode != 0o600 {
		t.Fatalf("catalog file mode = %o, want 600", mode)
	}
}

func TestLatestCommitResolvesAnAnnotatedTagToACommit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/carapace-sh/carapace-bin/releases/latest":
			io.WriteString(writer, `{"tag_name":"v1.9.0","draft":false,"prerelease":false}`)
		case "/repos/carapace-sh/carapace-bin/git/ref/tags/v1.9.0":
			io.WriteString(writer, `{"object":{"type":"tag","sha":"tag-object"}}`)
		case "/repos/carapace-sh/carapace-bin/git/tags/tag-object":
			io.WriteString(writer, `{"object":{"type":"commit","sha":"commit-sha"}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client := &http.Client{Transport: rewriteTransport{target: server.URL}}
	release, err := LatestCommit(context.Background(), client)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if release.Tag != "v1.9.0" || release.Commit != "commit-sha" {
		t.Fatalf("release = %+v", release)
	}
}

func TestFetchCatalogImportsThePinnedArchive(t *testing.T) {
	body := releaseArchive(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/tar.gz/commit-sha") {
			http.NotFound(writer, request)
			return
		}
		writer.Write(body)
	}))
	defer server.Close()
	client := &http.Client{Transport: rewriteTransport{target: server.URL}}
	catalog, stats, err := FetchCatalog(context.Background(), client, Release{Tag: "v1.9.0", Commit: "commit-sha"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if catalog.Release != "v1.9.0" || catalog.Commit != "commit-sha" {
		t.Fatalf("metadata = %q %q", catalog.Release, catalog.Commit)
	}
	if catalog.RootCount() != 1 || catalog.Root("tool") == nil {
		t.Fatalf("roots = %d", catalog.RootCount())
	}
	if stats.RepairedFiles != 0 || stats.SkippedSources != 0 {
		t.Fatalf("clean archive reported %+v", stats)
	}
}

func TestFetchCatalogRepairsAnUpstreamSourceAndReportsIt(t *testing.T) {
	body := repairableReleaseArchive(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/tar.gz/commit-sha") {
			http.NotFound(writer, request)
			return
		}
		writer.Write(body)
	}))
	defer server.Close()
	client := &http.Client{Transport: rewriteTransport{target: server.URL}}
	catalog, stats, err := FetchCatalog(context.Background(), client, Release{Tag: "v1.9.0", Commit: "commit-sha"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if stats.RepairedFiles != 2 || stats.SkippedSources != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	flag, ok := catalog.FindFlag(catalog.Root("tool"), "--align")
	if !ok || flag.Desc != "Vertical position (default: top). Details see --align-x" {
		t.Fatalf("repaired flag = %+v, found=%v", flag, ok)
	}
	flag, ok = catalog.FindFlag(catalog.Lookup([]string{"tool", "run"}), "--growable-table")
	if !ok || flag.Desc != "WebAssembly: remove maximum size from function table" {
		t.Fatalf("repaired zig-style flag = %+v, found=%v", flag, ok)
	}
}

func TestFetchCatalogRejectsAnUnpinnedRelease(t *testing.T) {
	if _, _, err := FetchCatalog(context.Background(), http.DefaultClient, Release{Tag: "v1.9.0"}); err == nil {
		t.Fatal("a release without a commit was fetched")
	}
}

func TestUpdateClientBoundsRedirects(t *testing.T) {
	client := UpdateClient()
	if client.Timeout != updateTimeout {
		t.Fatalf("timeout = %v", client.Timeout)
	}
	for _, test := range []struct {
		url   string
		hops  int
		allow bool
	}{
		{"https://codeload.github.com/archive", 1, true},
		{"https://api.github.com/repos/carapace-sh/carapace-bin", 1, true},
		{"https://objects.githubusercontent.com/archive", 1, true},
		{"https://gh-proxy.com/https://api.github.com/repos/carapace-sh/carapace-bin/releases/latest", 1, true},
		{"http://example.com", 1, false},
		{"https://evil.example/archive", 1, false},
		{"https://github.com.evil.example/archive", 1, false},
		{"https://example.com", 5, false},
	} {
		target, err := url.Parse(test.url)
		if err != nil {
			t.Fatal(err)
		}
		err = client.CheckRedirect(&http.Request{URL: target}, make([]*http.Request, test.hops))
		if (err == nil) != test.allow {
			t.Errorf("redirect %s (%d hops) = %v", test.url, test.hops, err)
		}
	}
}

func TestUpdateFallbackRetriesOnlyCatalogGitHubFailures(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests []string
			transport := updateFallbackTransport{primary: catalogTransport(func(request *http.Request) (*http.Response, error) {
				requests = append(requests, request.URL.String())
				if request.URL.Host == updateMirror {
					if request.Header.Get("Authorization") != "" {
						t.Fatal("credentials were sent to the mirror")
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v1.9.0"}`)), Header: make(http.Header)}, nil
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("failed")), Header: make(http.Header)}, nil
			})}
			client := &http.Client{Transport: transport}
			var release struct {
				Tag string `json:"tag_name"`
			}
			if err := getJSON(context.Background(), client, "https://api.github.com/repos/carapace-sh/carapace-bin/releases/latest", &release); err != nil || release.Tag != "v1.9.0" {
				t.Fatalf("mirrored release = %+v, %v", release, err)
			}
			if len(requests) != 2 || requests[1] != "https://"+updateMirror+"/"+requests[0] {
				t.Fatalf("requests = %v", requests)
			}
		})
	}
	requests := 0
	transport := updateFallbackTransport{primary: catalogTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("missing"))}, nil
	})}
	request, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/other/project/releases/latest", nil)
	response, err := transport.RoundTrip(request)
	if err != nil || requests != 1 || response.StatusCode != http.StatusNotFound {
		t.Fatalf("unrelated request: response=%v err=%v count=%d", response, err, requests)
	}
	response.Body.Close()
}

func TestUpdateFallbackFetchesPinnedArchiveOnConnectionFailure(t *testing.T) {
	archive := releaseArchive(t)
	var requests []string
	client := &http.Client{Transport: updateFallbackTransport{primary: catalogTransport(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.String())
		if request.URL.Host != updateMirror {
			return nil, errors.New("GitHub unavailable")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(archive)), Header: make(http.Header)}, nil
	})}}
	catalog, _, err := FetchCatalog(context.Background(), client, Release{Tag: "v1.9.0", Commit: "commit-sha"})
	if err != nil || catalog == nil || catalog.Root("tool") == nil {
		t.Fatalf("mirrored archive: catalog=%v err=%v", catalog, err)
	}
	if len(requests) != 2 || requests[1] != "https://"+updateMirror+"/"+requests[0] {
		t.Fatalf("requests = %v", requests)
	}
}

func TestUpdateFallbackDoesNotForwardRequestCredentials(t *testing.T) {
	requests := 0
	transport := updateFallbackTransport{primary: catalogTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Hostname() == updateMirror {
			if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
				t.Fatal("the mirror received request credentials")
			}
			return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
		}
		return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("blocked"))}, nil
	})}
	request, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/carapace-sh/carapace-bin/releases/latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("Cookie", "session=secret")
	response, err := transport.RoundTrip(request)
	if err != nil || response.StatusCode != http.StatusBadGateway || requests != 2 {
		t.Fatalf("fallback: response=%v err=%v requests=%d", response, err, requests)
	}
	response.Body.Close()
}

func TestFetchCatalogRejectsDownloadFailure(t *testing.T) {
	client := &http.Client{Transport: catalogTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Status: "502 Bad Gateway", Body: io.NopCloser(strings.NewReader("failed"))}, nil
	})}
	if _, _, err := FetchCatalog(context.Background(), client, Release{Tag: "v1.9.0", Commit: "commit-sha"}); err == nil {
		t.Fatal("a failed download was accepted")
	}
}

// rewriteTransport sends the updater's fixed absolute URLs to a test server.
type rewriteTransport struct{ target string }

func (transport rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(transport.target, "http://")
	return http.DefaultTransport.RoundTrip(clone)
}

func releaseArchive(t *testing.T) []byte {
	t.Helper()
	return tarGzipArchive(t, map[string]string{
		"carapace-bin-1.9.0/LICENSE":                    "MIT",
		"carapace-bin-1.9.0/man/cmd/tool/tool.yaml":     "name: tool\r\ndescription: a tool\r\n",
		"carapace-bin-1.9.0/man/cmd/tool/tool.run.yaml": "name: run\r\nflags:\r\n  '-v=': verbose\r\n",
		"carapace-bin-1.9.0/cmd/carapace/main.go":       "package main",
	})
}

// repairableReleaseArchive is a release that ships the upstream defect: one
// plain scalar contains ": ", which the importer must quote and count.
func repairableReleaseArchive(t *testing.T) []byte {
	t.Helper()
	return tarGzipArchive(t, map[string]string{
		"carapace-bin-1.9.0/LICENSE": "MIT",
		// The two defect shapes in real releases: mpv's "(default: top)" and
		// zig's leading "WebAssembly:" in an option description.
		"carapace-bin-1.9.0/man/cmd/tool/tool.yaml":     "name: tool\nflags:\n  --align?: Vertical position (default: top). Details see --align-x\n",
		"carapace-bin-1.9.0/man/cmd/tool/tool.run.yaml": "name: run\nflags:\n  --growable-table: WebAssembly: remove maximum size from function table\n",
	})
}

func tarGzipArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipper := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipper)
	// tar reads its members in order; the import sorts the source files itself.
	for name := range entries {
		data := entries[name]
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
