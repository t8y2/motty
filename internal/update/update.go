// Package update asks a release feed whether a newer version exists. It only
// reads: telling the user and opening the download page is the caller's job,
// and nothing here replaces the running program.
package update

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	vivaupdate "github.com/vivaui/viva/update"

	"motty/internal/netclient"
)

// Current is the version of this build. A test keeps it equal to viva.toml's.
const Current = "0.4.24"

// Feed is the GitHub release the project publishes its builds to.
const Feed = "https://api.github.com/repos/t8y2/motty/releases/latest"

// MirrorFeed is the copy of the newest release on the download host. Unlike
// Feed it names every file with its size and SHA-256, which is what a download
// is checked against.
const MirrorFeed = "https://dl.motty.page/latest.json"

// VivaFeed is the signed Ed25519 feed an installed copy moves to: the same
// release data as MirrorFeed, in the format github.com/vivaui/viva/update
// reads, with a patch per asset. It is published beside MirrorFeed while old
// versions still read that one; see the update ADR.
const VivaFeed = "https://dl.motty.page/viva/latest.json"

// VivaPublicKey is the base64 Ed25519 public key that signs VivaFeed. Its
// private half is MOTTY_VIVA_UPDATE_KEY in CI, never in the repository.
const VivaPublicKey = "nmgLeV6s/EOjzzDOC7G59NotyGPqjTY4/6Ejd4KpcbY="

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
	// FeedVerified marks a package whose size and digest were read from the
	// signed Viva feed. The feed's Ed25519 signature is its authenticity, so
	// there is no per-package minisign signature to check.
	FeedVerified bool
	// Format is the Viva feed's format for the asset: zip, nsis or tar.gz.
	Format string
	// Patches are the deltas the feed offers from earlier versions to this
	// one. Only a feed-verified package has them.
	Patches []Patch
}

// Patch is one delta update from an installed version to this release.
type Patch struct {
	FromVersion string
	URL         string
	Size        int64
	SHA256      string
}

// PatchFrom returns the patch that applies to version, if the feed offers one.
func (asset Asset) PatchFrom(version string) (Patch, bool) {
	for _, patch := range asset.Patches {
		if patch.FromVersion == version {
			return patch, true
		}
	}
	return Patch{}, false
}

// Has reports whether the release carries a package for the asking platform.
func (asset Asset) Has() bool { return asset.URL != "" }

// Signed reports whether the package's authenticity can be checked: a minisign
// signature, or the signed feed that named it.
func (asset Asset) Signed() bool { return asset.Signature != "" || asset.FeedVerified }

const (
	maxFeedBytes  = 1 << 20
	maxNotesBytes = 16 << 10
	// A minisign signature is a few hundred bytes; anything far past that is
	// not one, and the feed should not be allowed to make the app carry it.
	maxSignatureBytes = 4096
	// CheckBudget bounds a whole check, caller side: the feeds are
	// alternatives tried one after another, each may use feedAttemptTimeout,
	// and the last one still needs room to answer.
	CheckBudget = 35 * time.Second
)

// feedAttemptTimeout bounds one feed in the fallback chain, so a feed that
// stalls cannot spend the time the next one needs. Tests shorten it.
var feedAttemptTimeout = 10 * time.Second

// Latest reads the feed. The client decides timeouts and proxies; nil uses the
// desktop's proxy settings, as netclient does, with a 15 second timeout.
func Latest(ctx context.Context, client *http.Client, feed string) (Release, error) {
	if client == nil {
		client = netclient.Client(15 * time.Second)
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
	PackagePortable
)

// Source is where an installation reads releases and packages from.
type Source uint8

const (
	// SourceOfficial is the project's own mirror: the signed Viva feed on the
	// official download host, with its mirror and GitHub fallbacks.
	SourceOfficial Source = iota
	// SourceCNB is the copy of the same signed feed and the same packages on
	// CNB, for networks where the official host is slow or unreachable.
	SourceCNB
)

// String is the value the setting is saved as.
func (source Source) String() string {
	if source == SourceCNB {
		return "cnb"
	}
	return "official"
}

// ParseSource reads a saved source. Anything unknown is the official one, so a
// file written by an older or newer version never leaves the app without a
// source.
func ParseSource(value string) Source {
	if value == "cnb" {
		return SourceCNB
	}
	return SourceOfficial
}

// CNBFeed is the signed Viva feed published beside the packages on CNB. It is
// the newest release's asset under CNB's "latest" alias, so a check from that
// source never touches the official host.
const CNBFeed = "https://cnb.cool/dbxio.com/motty/-/releases/latest/download/latest.json"

// cnbReleases is the directory CNB serves a release's assets from.
const cnbReleases = "https://cnb.cool/dbxio.com/motty/-/releases/download/"

// CheckPackage selects the package matching the installation on this machine.
// It prefers the signed Viva feed, which is where patches and the one trust
// root live; the mirror feed stays for a .deb (the Viva feed has no Debian
// asset) and for the migration window, when the Viva feed is not published yet
// or cannot be reached. Each feed gets its own attempt timeout: a stalled Viva
// feed must not spend the time the mirror or GitHub fallback needs.
func CheckPackage(ctx context.Context, client *http.Client, goos, goarch string, pkg Package) (Release, error) {
	if pkg != PackageDeb {
		attempt, cancel := context.WithTimeout(ctx, feedAttemptTimeout)
		release, err := checkVivaPackageAt(attempt, client, VivaFeed, []string{VivaPublicKey}, goos, goarch, pkg)
		cancel()
		if err == nil {
			return release, nil
		}
	}
	return checkFeedsPackage(ctx, client, MirrorFeed, Feed, goos, goarch, pkg)
}

// CheckPackageFrom asks the source the user selected. The CNB source reads the
// same signed feed from the CNB copy and points the package at the CNB release.
// When that copy cannot answer, the official feeds still decide, so a mirror
// that is not published yet never makes updates impossible; the returned error
// is then the CNB one, because that is the source the user chose. A .deb has no
// asset in the signed feed, so it keeps the official path whatever the source.
func CheckPackageFrom(ctx context.Context, client *http.Client, goos, goarch string, pkg Package, source Source) (Release, error) {
	if source != SourceCNB || pkg == PackageDeb {
		return CheckPackage(ctx, client, goos, goarch, pkg)
	}
	attempt, cancel := context.WithTimeout(ctx, feedAttemptTimeout)
	release, err := checkVivaPackageAt(attempt, client, CNBFeed, []string{VivaPublicKey}, goos, goarch, pkg)
	cancel()
	if err == nil {
		return cnbRelease(release, goos, goarch, pkg), nil
	}
	if official, officialErr := CheckPackage(ctx, client, goos, goarch, pkg); officialErr == nil {
		return official, nil
	}
	return Release{}, err
}

// cnbRelease points the package and the patches of a release read from the CNB
// feed at the CNB copy. The feed's size and SHA-256 still hold every download,
// and the signed feed still names each file, so only the host changes. A patch
// keeps the file name its URL in the feed has; one whose URL names no file is
// dropped, which leaves that update to the full package.
func cnbRelease(release Release, goos, goarch string, pkg Package) Release {
	name := cnbAssetName(release.Version, goos, goarch, pkg)
	if !release.Asset.Has() || name == "" {
		return release
	}
	base := cnbReleases + "v" + release.Version + "/"
	release.Asset.URL = base + name
	patches := make([]Patch, 0, len(release.Asset.Patches))
	for _, patch := range release.Asset.Patches {
		file := patchFileName(patch.URL)
		if file == "" {
			continue
		}
		patch.URL = base + file
		patches = append(patches, patch)
	}
	if len(patches) == 0 {
		patches = nil
	}
	release.Asset.Patches = patches
	return release
}

// patchFileName is the escaped last path segment of a patch URL, or "" when the
// URL does not end in a .patch file name.
func patchFileName(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	file := path.Base(parsed.EscapedPath())
	if file == "." || file == "/" || !strings.HasSuffix(file, ".patch") {
		return ""
	}
	return file
}

// cnbAssetName is the name the release's asset has on CNB, which is the name
// the release published: the stable name with the version in it, and the .deb
// naming the architecture instead.
func cnbAssetName(version, goos, goarch string, pkg Package) string {
	stable := StableNamePackage(goos, goarch, pkg)
	if stable == "" || version == "" {
		return ""
	}
	if pkg == PackageDeb {
		return "motty_" + version + "_" + goarch + ".deb"
	}
	return strings.Replace(stable, "motty-", "motty-"+version+"-", 1)
}

// checkVivaPackageAt reads a Viva feed. The feed URL and keys are parameters so
// a test can sign a feed of its own.
func checkVivaPackageAt(ctx context.Context, client *http.Client, feedURL string, keys []string, goos, goarch string, pkg Package) (Release, error) {
	if client == nil {
		// The attempt context normally ends the feed first; the client's own
		// timeout is the backstop for a caller that sets no deadline.
		client = netclient.Client(30 * time.Second)
	}
	publicKeys := make([]ed25519.PublicKey, 0, len(keys))
	for _, encoded := range keys {
		key, err := vivaupdate.ParsePublicKey(encoded)
		if err != nil {
			return Release{}, fmt.Errorf("update: the Viva feed key: %w", err)
		}
		publicKeys = append(publicKeys, key)
	}
	vivaClient, err := vivaupdate.New(vivaupdate.Config{
		FeedURL: feedURL, PublicKeys: publicKeys, Current: Current, HTTP: client,
	})
	if err != nil {
		return Release{}, err
	}
	feed, err := vivaClient.Check(ctx)
	if err != nil {
		return Release{}, err
	}
	if feed == nil {
		// The feed is reachable and has nothing newer: the running version is
		// the latest, which is what the caller compares against.
		return Release{Version: Current, PlatformPackageChecked: true}, nil
	}
	asset, err := selectVivaAsset(feed, goos, goarch, pkg)
	if err != nil {
		return Release{}, err
	}
	release := Release{
		Version:                feed.Latest.Version,
		URL:                    feed.Latest.NotesURL,
		Notes:                  fetchReleaseNotes(ctx, client, feed.Latest.Version),
		PlatformPackageChecked: true,
		Asset: Asset{
			Name: StableNamePackage(goos, goarch, pkg), URL: asset.URL,
			SHA256: asset.SHA256, Size: asset.Size, FeedVerified: true,
			Format: asset.Format, Patches: mapVivaPatches(patchesFor(feed, goos, goarch, asset)),
		},
	}
	if release.URL == "" {
		release.URL = "https://github.com/t8y2/motty/releases/tag/v" + feed.Latest.Version
	}
	return release, nil
}

// patchesFor returns the patches an installation can use. The Windows feed
// lists a patch on the portable zip because that is the payload; an NSIS
// installation holds the same application files, so it may use that patch too.
func patchesFor(feed *vivaupdate.Feed, goos, goarch string, selected vivaupdate.Asset) []vivaupdate.Patch {
	if len(selected.Patches) > 0 {
		return selected.Patches
	}
	for _, candidate := range feed.Latest.Assets {
		if candidate.OS == goos && candidate.Arch == goarch && candidate.Format == vivaupdate.FormatZIP {
			return candidate.Patches
		}
	}
	return nil
}

// mapVivaPatches copies the feed's patch list into the application's type.
func mapVivaPatches(patches []vivaupdate.Patch) []Patch {
	if len(patches) == 0 {
		return nil
	}
	out := make([]Patch, len(patches))
	for index, patch := range patches {
		out[index] = Patch{FromVersion: patch.FromVersion, URL: patch.URL, Size: patch.Size, SHA256: patch.SHA256}
	}
	return out
}

// AppID is the application identifier the update feed names.
const AppID = "dev.motty.app"

// DownloadPatch fetches a delta listed by the signed feed and verifies its size
// and digest. The caller applies it with ApplyPatch.
func DownloadPatch(ctx context.Context, client *http.Client, asset Asset, patch Patch, dir string, progress func(done, total int64)) (string, error) {
	vivaClient, err := newVivaClient(client)
	if err != nil {
		return "", err
	}
	return vivaClient.DownloadPatch(ctx,
		vivaupdate.Asset{OS: runtime.GOOS, Arch: runtime.GOARCH, Format: asset.Format, URL: asset.URL, Size: asset.Size, SHA256: asset.SHA256},
		vivaupdate.Patch{FromVersion: patch.FromVersion, URL: patch.URL, Size: patch.Size, SHA256: patch.SHA256},
		dir, progress)
}

// ApplyPatch applies a downloaded patch to the running installation and returns
// a directory holding the new payload. The platform installer then replaces the
// application from it.
func ApplyPatch(patchPath, fromVersion, toVersion string) (string, error) {
	return vivaupdate.ApplyPatch(patchPath, AppID, fromVersion, toVersion)
}

// newVivaClient is the Viva feed client with the embedded key. Its requests
// follow the desktop's proxy settings; the caller's context bounds them, so
// the client itself carries no timeout and a large patch is not cut short.
func newVivaClient(client *http.Client) (*vivaupdate.Client, error) {
	if client == nil {
		client = netclient.Client(0)
	}
	key, err := vivaupdate.ParsePublicKey(VivaPublicKey)
	if err != nil {
		return nil, fmt.Errorf("update: the Viva feed key: %w", err)
	}
	return vivaupdate.New(vivaupdate.Config{FeedURL: VivaFeed, PublicKeys: []ed25519.PublicKey{key}, Current: Current, HTTP: client})
}

// selectVivaAsset picks the archive the installation kind needs. The feed's
// own choice prefers the NSIS setup on Windows; a portable copy needs the zip.
func selectVivaAsset(feed *vivaupdate.Feed, goos, goarch string, pkg Package) (vivaupdate.Asset, error) {
	if pkg == PackagePortable {
		for _, asset := range feed.Latest.Assets {
			if asset.OS == goos && asset.Arch == goarch && asset.Format == vivaupdate.FormatZIP {
				return asset, nil
			}
		}
		return vivaupdate.Asset{}, fmt.Errorf("%w: the feed has no portable zip for %s/%s", ErrNoRelease, goos, goarch)
	}
	return feed.Asset(goos, goarch)
}

// fetchReleaseNotes is releaseNotes, replaceable by tests.
var fetchReleaseNotes = releaseNotes

// releaseNotes reads the release notes for the update dialog. It checks the
// mirror feed first (which carries the notes without GitHub API rate limits),
// and falls back to the GitHub release body. A failed lookup leaves the dialog
// saying there are no notes.
func releaseNotes(ctx context.Context, client *http.Client, version string) string {
	githubURL := "https://api.github.com/repos/t8y2/motty/releases/tags/v" + version
	return releaseNotesFrom(ctx, client, MirrorFeed, githubURL, version)
}

func releaseNotesFrom(ctx context.Context, client *http.Client, mirrorURL, githubURL, version string) string {
	if client == nil {
		client = netclient.Client(15 * time.Second)
	}
	if notes := mirrorNotes(ctx, client, mirrorURL, version); notes != "" {
		return notes
	}
	return githubNotes(ctx, client, githubURL)
}

func mirrorNotes(ctx context.Context, client *http.Client, feedURL, version string) string {
	if feedURL == "" {
		return ""
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return ""
	}
	request.Header.Set("User-Agent", "motty/"+Current)
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	var body struct {
		Version string `json:"version"`
		Notes   string `json:"notes"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxFeedBytes)).Decode(&body); err != nil {
		return ""
	}
	if body.Version != version || strings.TrimSpace(body.Notes) == "" {
		return ""
	}
	return cutUTF8(body.Notes, maxNotesBytes)
}

func githubNotes(ctx context.Context, client *http.Client, url string) string {
	if url == "" {
		return ""
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "motty/"+Current)
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxFeedBytes)).Decode(&body); err != nil {
		return ""
	}
	return cutUTF8(body.Body, maxNotesBytes)
}

func checkFeeds(ctx context.Context, client *http.Client, mirrorFeed, githubFeed, goos, goarch string) (Release, error) {
	return checkFeedsPackage(ctx, client, mirrorFeed, githubFeed, goos, goarch, PackageArchive)
}

// checkFeedsPackage asks the mirror and, when it cannot answer, GitHub. Each
// one gets its own attempt timeout, so a mirror that stalls still leaves the
// fallback enough time to answer.
func checkFeedsPackage(ctx context.Context, client *http.Client, mirrorFeed, githubFeed, goos, goarch string, pkg Package) (Release, error) {
	mirrorCtx, cancelMirror := context.WithTimeout(ctx, feedAttemptTimeout)
	release, err := latestMirrorPackage(mirrorCtx, client, mirrorFeed, goos, goarch, pkg)
	cancelMirror()
	if err == nil {
		return release, nil
	}
	githubCtx, cancelGitHub := context.WithTimeout(ctx, feedAttemptTimeout)
	defer cancelGitHub()
	if fallback, fallbackErr := Latest(githubCtx, client, githubFeed); fallbackErr == nil || errors.Is(fallbackErr, ErrNoRelease) {
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
	if pkg == PackagePortable {
		if goos == "windows" && (goarch == "arm64" || goarch == "amd64") {
			return "motty-windows-" + goarch + ".zip"
		}
		return ""
	}
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
		client = netclient.Client(15 * time.Second)
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
