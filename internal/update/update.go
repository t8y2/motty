// Package update asks a release feed whether a newer version exists. It only
// reads: telling the user and opening the download page is the caller's job,
// and nothing here replaces the running program.
package update

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Current is the version of this build. A test keeps it equal to viva.toml's.
const Current = "0.3.9"

// Feed is the GitHub release the project publishes its builds to.
const Feed = "https://api.github.com/repos/t8y2/motty/releases/latest"

// MirrorFeed is the copy of the newest release on the download host. Unlike
// Feed it names every file with its size and SHA-256, which is what a download
// is checked against.
const MirrorFeed = "https://dl.motty.page/latest.json"

// ErrNoRelease means the feed has no published release, which is also what a
// private repository looks like to an anonymous request.
var ErrNoRelease = errors.New("update: no published release was found")

// Release is the newest published version.
type Release struct {
	Version string // without a leading v
	URL     string // the page to download it from
	Notes   string // the release notes as published (Markdown), possibly cut short
	// PlatformPackageChecked reports whether the answer included the feed's
	// package list for the platform that asked. A GitHub page fallback can
	// announce a release without proving whether that release has a package.
	PlatformPackageChecked bool
	// Asset is the package for the platform that asked, when the feed names one.
	// The zero value means there is none: either the feed checked and this
	// platform has none, or PlatformPackageChecked is false and it never said.
	Asset Asset
}

// Asset is one downloadable package, as the mirror describes it.
type Asset struct {
	Name   string // the file name of the versioned copy
	URL    string // https, on the host the feed came from
	SHA256 string // lower-case hex
	Size   int64
	// Signature is the base64 minisign signature over the package, naming its
	// version and file. An empty signature means the release cannot be
	// installed in place, only downloaded by hand.
	Signature string
}

// Has reports whether the release carries a package for the asking platform.
func (asset Asset) Has() bool { return asset.URL != "" }

// Signed reports whether the package can be checked against the release key.
func (asset Asset) Signed() bool { return asset.Signature != "" }

const (
	maxFeedBytes  = 1 << 20
	maxNotesBytes = 16 << 10
	// A minisign signature is a few hundred bytes; anything far past that is
	// not one, and the feed should not be allowed to make the app carry it.
	maxSignatureBytes = 4096
)

// Latest reads the feed. The client decides timeouts and proxies; nil uses one
// with a 15 second timeout and the environment's proxy.
func Latest(ctx context.Context, client *http.Client, feed string) (Release, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
	if err != nil {
		return Release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "motty/"+Current)
	response, err := client.Do(request)
	if err != nil {
		return Release{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return Release{}, ErrNoRelease
	}
	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("update: the release feed answered %s", response.Status)
	}
	var body struct {
		Tag        string `json:"tag_name"`
		URL        string `json:"html_url"`
		Notes      string `json:"body"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxFeedBytes)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("update: reading the release feed: %w", err)
	}
	version := strings.TrimPrefix(strings.TrimSpace(body.Tag), "v")
	if _, ok := parse(version); !ok || body.Draft || body.Prerelease {
		return Release{}, errors.New("update: the release feed has no usable version")
	}
	// The page is opened in the user's browser; only a web address is trusted.
	if !strings.HasPrefix(body.URL, "https://") {
		return Release{}, errors.New("update: the release page is not an https address")
	}
	return Release{Version: version, URL: body.URL, Notes: cutUTF8(body.Notes, maxNotesBytes)}, nil
}

// cutUTF8 keeps at most limit bytes of text, never splitting a character.
func cutUTF8(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

// Check asks the mirror for the newest release and, when the mirror cannot
// answer, GitHub. The package is chosen for goos and goarch; a GitHub answer has
// none, because it carries no checksums to hold a download to.
func Check(ctx context.Context, client *http.Client, goos, goarch string) (Release, error) {
	return CheckPackage(ctx, client, goos, goarch, PackageArchive)
}

// Package identifies the installation format, not just the host operating system.
type Package uint8

const (
	PackageArchive Package = iota
	PackageDeb
)

// CheckPackage selects the package matching the installation on this machine.
func CheckPackage(ctx context.Context, client *http.Client, goos, goarch string, pkg Package) (Release, error) {
	return checkFeedsPackage(ctx, client, MirrorFeed, Feed, goos, goarch, pkg)
}

func checkFeeds(ctx context.Context, client *http.Client, mirrorFeed, githubFeed, goos, goarch string) (Release, error) {
	return checkFeedsPackage(ctx, client, mirrorFeed, githubFeed, goos, goarch, PackageArchive)
}

func checkFeedsPackage(ctx context.Context, client *http.Client, mirrorFeed, githubFeed, goos, goarch string, pkg Package) (Release, error) {
	release, err := latestMirrorPackage(ctx, client, mirrorFeed, goos, goarch, pkg)
	if err == nil {
		return release, nil
	}
	if fallback, fallbackErr := Latest(ctx, client, githubFeed); fallbackErr == nil || errors.Is(fallbackErr, ErrNoRelease) {
		return fallback, fallbackErr
	}
	return Release{}, err
}

// StableName is the name the mirror gives the package of a platform, or "" for
// a platform that has none: the three systems the release builds for, on the
// architectures it builds for.
func StableName(goos, goarch string) string {
	switch {
	case goos == "darwin" && (goarch == "arm64" || goarch == "amd64"):
		return "motty-darwin-" + goarch + ".zip"
	case goos == "linux" && (goarch == "arm64" || goarch == "amd64"):
		return "motty-linux-" + goarch + ".tar.gz"
	case goos == "windows" && (goarch == "arm64" || goarch == "amd64"):
		return "motty-windows-" + goarch + "-setup.exe"
	}
	return ""
}

// StableNamePackage names a DEB only for the architectures we publish.
func StableNamePackage(goos, goarch string, pkg Package) string {
	if pkg == PackageDeb {
		if goos == "linux" && (goarch == "arm64" || goarch == "amd64") {
			return "motty_" + goarch + ".deb"
		}
		return ""
	}
	return StableName(goos, goarch)
}

const maxAssetBytes = 1 << 29

var (
	tagPattern    = regexp.MustCompile(`^v[0-9]+(\.[0-9]+){0,2}$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// LatestMirror reads the mirror's latest.json at feed. The package URL is built
// from the feed's own address and the release tag, never taken from the file,
// so the answer cannot send a download to another host.
func LatestMirror(ctx context.Context, client *http.Client, feed, goos, goarch string) (Release, error) {
	return latestMirrorPackage(ctx, client, feed, goos, goarch, PackageArchive)
}

func latestMirrorPackage(ctx context.Context, client *http.Client, feed, goos, goarch string, pkg Package) (Release, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	base, err := url.Parse(feed)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return Release{}, errors.New("update: the mirror feed is not an https address")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
	if err != nil {
		return Release{}, err
	}
	request.Header.Set("User-Agent", "motty/"+Current)
	response, err := client.Do(request)
	if err != nil {
		return Release{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("update: the mirror answered %s", response.Status)
	}
	var body struct {
		Version string `json:"version"`
		Tag     string `json:"tag"`
		Source  string `json:"source"`
		Notes   string `json:"notes"`
		Files   []struct {
			Name      string `json:"name"`
			Size      int64  `json:"size"`
			SHA256    string `json:"sha256"`
			Signature string `json:"signature"`
		} `json:"files"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxFeedBytes)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("update: reading the mirror feed: %w", err)
	}
	if _, ok := parse(body.Version); !ok || body.Tag != "v"+body.Version || !tagPattern.MatchString(body.Tag) {
		return Release{}, errors.New("update: the mirror feed has no usable version")
	}
	if !strings.HasPrefix(body.Source, "https://") {
		return Release{}, errors.New("update: the release page is not an https address")
	}
	release := Release{
		Version: body.Version, URL: body.Source, Notes: cutUTF8(body.Notes, maxNotesBytes),
		PlatformPackageChecked: true,
	}
	stable := StableNamePackage(goos, goarch, pkg)
	for _, file := range body.Files {
		if stable == "" || file.Name != stable {
			continue
		}
		if !sha256Pattern.MatchString(file.SHA256) || file.Size <= 0 || file.Size > maxAssetBytes {
			// The feed names this platform's file but nothing that could hold a
			// download to it. The list is there, the answer is not: calling the
			// release package-less would be the same wrong claim a bare page
			// fallback makes.
			release.PlatformPackageChecked = false
			break
		}
		name := strings.Replace(stable, "motty-", "motty-"+body.Version+"-", 1)
		if pkg == PackageDeb {
			name = "motty_" + body.Version + "_" + goarch + ".deb"
		}
		pinned := url.URL{Scheme: "https", Host: base.Host, Path: "/releases/" + body.Tag + "/" + name}
		release.Asset = Asset{Name: name, URL: pinned.String(), SHA256: file.SHA256, Size: file.Size, Signature: validSignature(file.Signature)}
		break
	}
	return release, nil
}

// validSignature keeps a signature the app can use and drops anything that is
// not a base64 string of a sane length: a broken one is the same as none, and
// the download page still works.
func validSignature(signature string) string {
	if signature == "" || len(signature) > maxSignatureBytes {
		return ""
	}
	if _, err := base64.StdEncoding.DecodeString(signature); err != nil {
		return ""
	}
	return signature
}

// Newer reports whether latest is a later version than current. Either that is
// not a plain dotted number is "no".
func Newer(current, latest string) bool {
	a, okA := parse(strings.TrimPrefix(current, "v"))
	b, okB := parse(strings.TrimPrefix(latest, "v"))
	if !okA || !okB {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return b[index] > a[index]
		}
	}
	return false
}

func parse(version string) (parts [3]int, ok bool) {
	fields := strings.Split(version, ".")
	if len(fields) == 0 || len(fields) > 3 {
		return parts, false
	}
	for index, field := range fields {
		number, err := strconv.Atoi(field)
		if err != nil || number < 0 || number > 1<<20 {
			return parts, false
		}
		parts[index] = number
	}
	return parts, true
}
