package update

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, test := range []struct {
		current, latest string
		want            bool
	}{
		{"0.1.0", "0.1.1", true}, {"0.1.0", "0.2", true}, {"0.9.0", "0.10.0", true}, {"1.0", "1.0.0", false},
		{"0.2.0", "0.1.9", false}, {"v0.1.0", "v0.1.0", false}, {"0.1.0", "next", false}, {"dev", "1.0.0", false},
		{"1.0.0", "1.0.0.1", false},
	} {
		if got := Newer(test.current, test.latest); got != test.want {
			t.Errorf("Newer(%q, %q) = %v", test.current, test.latest, got)
		}
	}
}

func TestStableName(t *testing.T) {
	// These are the names the mirror publishes after taking the version out of
	// a release asset, so they have to be the files the release workflow builds.
	for _, test := range []struct {
		goos, goarch, want string
	}{
		{"darwin", "arm64", "motty-darwin-arm64.zip"},
		{"darwin", "amd64", "motty-darwin-amd64.zip"},
		{"linux", "arm64", "motty-linux-arm64.tar.gz"},
		{"linux", "amd64", "motty-linux-amd64.tar.gz"},
		{"windows", "amd64", "motty-windows-amd64-setup.exe"},
		{"windows", "arm64", "motty-windows-arm64-setup.exe"},
		{"darwin", "386", ""},
		{"freebsd", "amd64", ""},
	} {
		if got := StableName(test.goos, test.goarch); got != test.want {
			t.Errorf("StableName(%q, %q) = %q, want %q", test.goos, test.goarch, got, test.want)
		}
	}
}

func TestDebPackageSelection(t *testing.T) {
	for _, architecture := range []string{"amd64", "arm64"} {
		if got := StableNamePackage("linux", architecture, PackageDeb); got != "motty_"+architecture+".deb" {
			t.Fatalf("DEB %s: %s", architecture, got)
		}
		server := mirrorServer(t, `{"version":"1.2.3","tag":"v1.2.3","source":"https://github.com/t8y2/motty/releases/tag/v1.2.3","files":[{"name":"motty_`+architecture+`.deb","size":100,"sha256":"`+strings.Repeat("a", 64)+`","signature":"c2ln"}]}`)
		release, err := latestMirrorPackage(context.Background(), server.Client(), server.URL, "linux", architecture, PackageDeb)
		server.Close()
		if err != nil || release.Asset.Name != "motty_1.2.3_"+architecture+".deb" || !strings.HasSuffix(release.Asset.URL, "/releases/v1.2.3/"+release.Asset.Name) {
			t.Fatalf("DEB %s: %+v, %v", architecture, release.Asset, err)
		}
	}
	if StableNamePackage("darwin", "amd64", PackageDeb) != "" || StableNamePackage("linux", "386", PackageDeb) != "" {
		t.Fatal("off-platform DEB was offered")
	}
}

func TestCurrentMatchesTheManifest(t *testing.T) {
	data, err := os.ReadFile("../../viva.toml")
	if os.IsNotExist(err) {
		t.Skip("the manifest is not next to this test (it was built elsewhere)")
	}
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^version\s*=\s*"([^"]+)"`).FindSubmatch(data)
	if match == nil || string(match[1]) != Current {
		t.Fatalf("viva.toml version %q, update.Current %q", match, Current)
	}
}

func serve(body string, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			status = http.StatusForbidden
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

func TestLatestReadsTheFeed(t *testing.T) {
	server := serve(`{"tag_name":"v0.3.0","html_url":"https://github.com/t8y2/motty/releases/tag/v0.3.0"}`, 200)
	defer server.Close()
	release, err := Latest(context.Background(), server.Client(), server.URL)
	if err != nil || release.Version != "0.3.0" || release.URL != "https://github.com/t8y2/motty/releases/tag/v0.3.0" {
		t.Fatalf("%+v %v", release, err)
	}
}

func TestLatestRefusesWhatItCannotTrust(t *testing.T) {
	for name, body := range map[string]string{
		"bad version": `{"tag_name":"nightly","html_url":"https://x/y"}`,
		"prerelease":  `{"tag_name":"v1.0.0","html_url":"https://x/y","prerelease":true}`,
		"draft":       `{"tag_name":"v1.0.0","html_url":"https://x/y","draft":true}`,
		"plain http":  `{"tag_name":"v1.0.0","html_url":"http://x/y"}`,
		"javascript":  `{"tag_name":"v1.0.0","html_url":"javascript:alert(1)"}`,
		"not json":    `<html>`,
	} {
		server := serve(body, 200)
		if _, err := Latest(context.Background(), server.Client(), server.URL); err == nil {
			t.Errorf("%s was accepted", name)
		}
		server.Close()
	}
	missing := serve(`{"message":"Not Found"}`, 404)
	defer missing.Close()
	if _, err := Latest(context.Background(), missing.Client(), missing.URL); !errors.Is(err, ErrNoRelease) {
		t.Errorf("a 404 gave %v, want ErrNoRelease", err)
	}
	server := serve(`{}`, 500)
	defer server.Close()
	if _, err := Latest(context.Background(), server.Client(), server.URL); err == nil {
		t.Error("an error status was accepted")
	}
}

func TestLatestKeepsTheReleaseNotes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.2.3","html_url":"https://example.com/r","body":"notes"}`))
	}))
	defer server.Close()
	release, err := Latest(context.Background(), server.Client(), server.URL)
	if err != nil || release.Notes != "notes" {
		t.Fatalf("release %+v, error %v", release, err)
	}
}

func mirrorServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(body)) }))
	t.Cleanup(server.Close)
	return server
}

const goodHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestTheMirrorNamesThePackageOfThePlatformAndWhereToGetIt(t *testing.T) {
	server := mirrorServer(t, `{"version":"1.2.3","tag":"v1.2.3","source":"https://github.com/o/r/releases/tag/v1.2.3","mirror":"https://evil.example","notes":"## Fixed",
	  "files":[{"name":"motty-darwin-arm64.zip","size":100,"sha256":"`+goodHash+`","url":"https://evil.example/x"},{"name":"motty-linux-amd64.tar.gz","size":5,"sha256":"`+goodHash+`"}]}`)
	release, err := LatestMirror(context.Background(), server.Client(), server.URL+"/latest.json", "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	want := server.URL + "/releases/v1.2.3/motty-1.2.3-darwin-arm64.zip"
	if release.Version != "1.2.3" || release.Notes != "## Fixed" || !release.PlatformPackageChecked ||
		release.Asset.URL != want || release.Asset.SHA256 != goodHash || release.Asset.Size != 100 {
		t.Fatalf("release %+v, want asset at %s", release, want)
	}

	// A platform the release has no package for still gets the page.
	release, err = LatestMirror(context.Background(), server.Client(), server.URL+"/latest.json", "darwin", "amd64")
	if err != nil || !release.PlatformPackageChecked || release.Asset.Has() || release.URL == "" {
		t.Fatalf("release %+v, err %v", release, err)
	}
}

func TestGitHubFallbackDoesNotClaimThePackageListWasChecked(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mirror":
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		case "/github":
			w.Write([]byte(`{"tag_name":"v1.2.3","html_url":"https://github.com/t8y2/motty/releases/tag/v1.2.3"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	release, err := checkFeeds(context.Background(), server.Client(), server.URL+"/mirror", server.URL+"/github", "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "1.2.3" || release.PlatformPackageChecked || release.Asset.Has() {
		t.Fatalf("fallback release = %+v", release)
	}
}

func TestTheMirrorCarriesTheReleaseSignature(t *testing.T) {
	signature := base64.StdEncoding.EncodeToString([]byte("minisign armour"))
	files := func(sig string) string {
		return `"files":[{"name":"motty-darwin-arm64.zip","size":100,"sha256":"` + goodHash + `","signature":"` + sig + `"}]`
	}
	server := mirrorServer(t, `{"version":"1.2.3","tag":"v1.2.3","source":"https://github.com/o/r",`+files(signature)+`}`)
	release, err := LatestMirror(context.Background(), server.Client(), server.URL, "darwin", "arm64")
	if err != nil || !release.Asset.Signed() || release.Asset.Signature != signature {
		t.Fatalf("release %+v, err %v", release, err)
	}

	for name, sig := range map[string]string{
		"absent":     "",
		"not base64": "no!",
		"too long":   strings.Repeat("A", 8192),
	} {
		server := mirrorServer(t, `{"version":"1.2.3","tag":"v1.2.3","source":"https://github.com/o/r",`+files(sig)+`}`)
		release, err := LatestMirror(context.Background(), server.Client(), server.URL, "darwin", "arm64")
		if err != nil || !release.Asset.Has() || release.Asset.Signed() {
			t.Errorf("%s: %+v %v", name, release, err)
		}
	}
}

func TestAMirrorAnswerThatCannotBeTrustedGivesNoPackage(t *testing.T) {
	for name, files := range map[string]string{
		"short hash": `[{"name":"motty-darwin-arm64.zip","size":100,"sha256":"abc"}]`,
		"no size":    `[{"name":"motty-darwin-arm64.zip","size":0,"sha256":"` + goodHash + `"}]`,
		"huge":       `[{"name":"motty-darwin-arm64.zip","size":99999999999,"sha256":"` + goodHash + `"}]`,
	} {
		server := mirrorServer(t, `{"version":"1.2.3","tag":"v1.2.3","source":"https://github.com/o/r","files":`+files+`}`)
		release, err := LatestMirror(context.Background(), server.Client(), server.URL, "darwin", "arm64")
		if err != nil || release.Asset.Has() {
			t.Errorf("%s: %+v %v", name, release, err)
		}
		if release.PlatformPackageChecked {
			t.Errorf("%s: a file the feed cannot hold a download to was reported as a checked package list: %+v", name, release)
		}
	}
	for name, body := range map[string]string{
		"tag and version differ": `{"version":"1.2.3","tag":"v9.9.9","source":"https://x","files":[]}`,
		"odd tag":                `{"version":"1.2.3","tag":"v1.2.3/../x","source":"https://x","files":[]}`,
		"page not https":         `{"version":"1.2.3","tag":"v1.2.3","source":"http://x","files":[]}`,
	} {
		server := mirrorServer(t, body)
		if _, err := LatestMirror(context.Background(), server.Client(), server.URL, "darwin", "arm64"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAMirrorThatNamesNoPackageForThisPlatformWasChecked(t *testing.T) {
	server := mirrorServer(t, `{"version":"1.2.3","tag":"v1.2.3","source":"https://github.com/o/r","files":[{"name":"motty-darwin-arm64.zip","size":100,"sha256":"`+goodHash+`"}]}`)
	release, err := LatestMirror(context.Background(), server.Client(), server.URL, "linux", "amd64")
	if err != nil || release.Asset.Has() || !release.PlatformPackageChecked {
		t.Fatalf("release %+v, err %v", release, err)
	}
}
