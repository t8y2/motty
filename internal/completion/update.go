package completion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"motty/internal/netclient"
)

// The catalog comes from one public source. The repository is a constant so a
// build cannot be pointed anywhere else by configuration or an environment
// variable: the update request carries no host, input line, directory or key.
const (
	RepositoryOwner = "carapace-sh"
	RepositoryName  = "carapace-bin"

	// FileName is the installed snapshot's name inside the data directory.
	FileName = "catalog.bin"

	updateTimeout = 120 * time.Second
	updateMirror  = "gh-proxy.com"
)

// Release is a published catalog version resolved to an immutable commit.
type Release struct {
	Tag    string
	Commit string
}

// CatalogPath is where an installed snapshot lives for a configuration store.
// An empty store path means no writable place, so there is no installed
// catalog and the built-in one is used.
func CatalogPath(statePath string) string {
	if statePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(statePath), "completion", FileName)
}

// LoadInstalled reads the snapshot at path. It reports an error when the file
// is missing or unusable, which the caller answers with the built-in catalog.
func LoadInstalled(path string) (*Catalog, error) {
	if path == "" {
		return nil, errors.New("no data directory")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxArchiveBytes {
		return nil, errors.New("the installed catalog is too large")
	}
	data, err := readLimited(file, maxArchiveBytes)
	if err != nil {
		return nil, err
	}
	return Decode(data)
}

// InstallCatalog replaces the snapshot at path atomically: a failed write
// leaves the previous file intact.
func InstallCatalog(path string, catalog *Catalog) error {
	return installCatalog(context.Background(), path, catalog)
}

func installCatalog(ctx context.Context, path string, catalog *Catalog) error {
	if path == "" {
		return errors.New("no data directory")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data := catalog.Encode()
	temporary, err := os.CreateTemp(dir, "catalog-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	remove := true
	defer func() {
		temporary.Close()
		if remove {
			os.Remove(name)
		}
	}()
	if err := writeCatalogFile(temporary, data); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	remove = false
	// Make the replacement durable without letting a crash expose the old
	// name and the new bytes as different files. Best effort: some filesystems
	// refuse to fsync a directory.
	if directory, err := os.Open(dir); err == nil {
		_ = directory.Sync()
		directory.Close()
	}
	return nil
}

func writeCatalogFile(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	return file.Close()
}

// Newer reports whether release a is newer than release b by its numeric
// version components. A tag that does not parse is never newer, so a damaged
// answer cannot replace a working catalog.
func Newer(a, b string) bool {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return false
	}
	for index := 0; index < len(av) || index < len(bv); index++ {
		var ap, bp int
		if index < len(av) {
			ap = av[index]
		}
		if index < len(bv) {
			bp = bv[index]
		}
		if ap != bp {
			return ap > bp
		}
	}
	return false
}

func parseVersion(tag string) ([]int, bool) {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if tag == "" {
		return nil, false
	}
	parts := strings.Split(tag, ".")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		// A prerelease suffix is not a version this program installs.
		if strings.ContainsAny(part, "-+") {
			return nil, false
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

// LatestCommit asks the release feed for the newest stable release and resolves
// its tag to the commit that the archive will be fetched by. Only the public
// repository is queried, and only after the user asked for an update.
func LatestCommit(ctx context.Context, client *http.Client) (Release, error) {
	var latest struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := getJSON(ctx, client, "https://api.github.com/repos/"+RepositoryOwner+"/"+RepositoryName+"/releases/latest", &latest); err != nil {
		return Release{}, err
	}
	if latest.Draft || latest.Prerelease {
		return Release{}, errors.New("the latest release is not stable")
	}
	if latest.Tag == "" {
		return Release{}, errors.New("the release has no tag")
	}
	return resolveTag(ctx, client, latest.Tag)
}

func resolveTag(ctx context.Context, client *http.Client, tag string) (Release, error) {
	release := Release{Tag: tag}
	for depth := 0; depth < 3; depth++ {
		var ref struct {
			Object struct {
				Type string `json:"type"`
				SHA  string `json:"sha"`
				URL  string `json:"url"`
			} `json:"object"`
		}
		endpoint := "https://api.github.com/repos/" + RepositoryOwner + "/" + RepositoryName + "/git/ref/tags/" + url.PathEscape(tag)
		if depth > 0 {
			endpoint = "https://api.github.com/repos/" + RepositoryOwner + "/" + RepositoryName + "/git/tags/" + release.Commit
		}
		if err := getJSON(ctx, client, endpoint, &ref); err != nil {
			return Release{}, err
		}
		if ref.Object.SHA == "" {
			return Release{}, errors.New("the tag has no object")
		}
		release.Commit = ref.Object.SHA
		switch ref.Object.Type {
		case "commit":
			return release, nil
		case "tag":
			continue
		default:
			return Release{}, fmt.Errorf("the tag points at a %s", ref.Object.Type)
		}
	}
	return Release{}, errors.New("the tag could not be resolved to a commit")
}

func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	return readUpdate(ctx, client, url, "application/vnd.github+json", "the release service", func(body io.Reader) error {
		return json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(out)
	})
}

// FetchCatalog downloads the archive of one immutable commit and imports it
// under the same limits as the built-in snapshot. The archive is read into
// memory only up to the configured compressed size; the tar reader enforces
// the expanded, member and file limits. The returned stats describe how the
// import went, including any upstream files that needed repair or were skipped.
func FetchCatalog(ctx context.Context, client *http.Client, release Release) (*Catalog, ImportStats, error) {
	if release.Tag == "" || release.Commit == "" {
		return nil, ImportStats{}, errors.New("the release is not pinned")
	}
	url := "https://codeload.github.com/" + RepositoryOwner + "/" + RepositoryName + "/tar.gz/" + release.Commit
	var source *SourceArchive
	err := readUpdate(ctx, client, url, "", "the download", func(body io.Reader) error {
		var err error
		source, err = ReadSourceArchive(body, DefaultArchiveLimits())
		return err
	})
	if err != nil {
		return nil, ImportStats{}, err
	}
	metadata := Metadata{Source: RepositoryName, Release: release.Tag, Commit: release.Commit, License: "MIT"}
	catalog, stats, err := Import(source.Files, metadata, DefaultImportOptions())
	if err != nil {
		return nil, ImportStats{}, err
	}
	return catalog, stats, nil
}

// readLimited reads at most limit bytes and rejects a longer answer instead of
// silently truncating it.
func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("the download passes %d bytes", limit)
	}
	return data, nil
}

// allowedUpdateHosts are the only hosts a redirect may reach. The requests
// themselves use fixed constants; the allowlist keeps a redirect from handing
// the catalog download to an unrelated HTTPS host.
var allowedUpdateHosts = []string{"api.github.com", "codeload.github.com", "github.com"}

// allowedUpdateHost reports whether host is one of GitHub's own download hosts.
// Release assets on GitHub are served from *.githubusercontent.com.
func allowedUpdateHost(host string) bool {
	host = strings.ToLower(host)
	if strings.HasSuffix(host, ".githubusercontent.com") {
		return true
	}
	for _, allowed := range allowedUpdateHosts {
		if host == allowed {
			return true
		}
	}
	return false
}

// UpdateClient returns the HTTP client the updater uses: each complete request
// is bounded in time, follows the desktop's proxy policy and redirects only to
// GitHub or the fixed fallback over https.
func UpdateClient() *http.Client {
	client := netclient.Client(updateTimeout)
	client.CheckRedirect = checkUpdateRedirect
	return client
}

// DirectUpdateClient returns an HTTP client that connects directly, bypassing
// the desktop proxy settings. It is used when the user explicitly chooses to
// retry a catalog check directly after a proxy failure.
func DirectUpdateClient() *http.Client {
	client := netclient.DirectClient(updateTimeout)
	client.CheckRedirect = checkUpdateRedirect
	return client
}

func checkUpdateRedirect(request *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	if request.URL.Scheme != "https" {
		return errors.New("only https downloads are followed")
	}
	if !allowedUpdateHost(request.URL.Hostname()) && request.URL.Hostname() != updateMirror {
		return fmt.Errorf("the redirect leaves GitHub for %q", request.URL.Hostname())
	}
	return nil
}

// readUpdate retries the fixed public catalog only after a complete HTTP attempt
// fails, including a stalled or interrupted response body. Each attempt has its
// own client timeout; cancellation of the caller stops the fallback as well.
// A fresh request uses the mirror's authority and receives no credentials.
func readUpdate(ctx context.Context, client *http.Client, endpoint, accept, service string, read func(io.Reader) error) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "motty")
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	retry, err := readUpdateAttempt(client, request, service, read)
	if err == nil || !retry || ctx.Err() != nil {
		return err
	}
	api := request.URL.Hostname() == "api.github.com" &&
		strings.HasPrefix(request.URL.Path, "/repos/"+RepositoryOwner+"/"+RepositoryName+"/")
	archive := request.URL.Hostname() == "codeload.github.com" &&
		strings.HasPrefix(request.URL.Path, "/"+RepositoryOwner+"/"+RepositoryName+"/tar.gz/")
	if request.URL.Scheme != "https" || (!api && !archive) {
		return err
	}
	mirror, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+updateMirror+"/"+endpoint, nil)
	if err != nil {
		return err
	}
	mirror.Header.Set("User-Agent", "motty")
	if accept != "" {
		mirror.Header.Set("Accept", accept)
	}
	_, err = readUpdateAttempt(client, mirror, service, read)
	return err
}

// readUpdateAttempt closes the failed body before a new source is tried. Parse,
// size and integrity errors remain failures; only incomplete transfers retry.
func readUpdateAttempt(client *http.Client, request *http.Request, service string, read func(io.Reader) error) (bool, error) {
	response, err := client.Do(request)
	if err != nil {
		return true, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		retry := response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		return retry, fmt.Errorf("%s answered %s", service, response.Status)
	}
	err = read(response.Body)
	var networkError net.Error
	retry := errors.As(err, &networkError) || errors.Is(err, io.ErrUnexpectedEOF)
	return retry, err
}
