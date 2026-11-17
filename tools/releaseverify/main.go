// Command releaseverify checks a published Motty release against its own
// manifests, the update feed and the mirror, and on macOS against the signature
// of the installed app. It only reads; the release itself is never changed.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"motty/internal/update"
)

const (
	defaultRepo  = "t8y2/motty"
	defaultFeed  = "https://dl.motty.page/latest.json"
	builtAgainst = "Built against vivaui/viva "
)

var builtAgainstLine = regexp.MustCompile("^Built against vivaui/viva `([0-9a-f]{7,40})`\\.$")

type options struct {
	tag       string
	repo      string
	feed      string
	mirror    string
	team      string
	installed string
	sample    bool
}

type deps struct {
	gh       func(args ...string) (string, error)
	get      func(url string) ([]byte, error)
	download func(tag, dir string) error
	unzip    func(file, dir string) (string, error)
	codesign func(app string) (string, error)
	tempDir  func() (string, error)
	read     func(path string) ([]byte, error)
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "releaseverify:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("releaseverify", flag.ContinueOnError)
	tag := flags.String("tag", "", "release tag, for example v0.1.6")
	repo := flags.String("repo", defaultRepo, "GitHub repository")
	feed := flags.String("feed", defaultFeed, "update feed URL")
	mirror := flags.String("mirror", "", "mirror base URL; defaults to the feed's mirror field")
	team := flags.String("team", "", "expected macOS team identifier; defaults to the installed app's")
	installed := flags.String("installed", "/Applications/Motty.app", "installed app whose signature the release is compared with")
	sample := flags.Bool("sample", true, "download one package from the mirror and hash it (set -sample=false to skip)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *tag == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: releaseverify -tag vX.Y.Z [-repo OWNER/NAME] [-team TEAM] [-sample=false]")
	}
	return verify(options{tag: *tag, repo: *repo, feed: *feed, mirror: *mirror, team: *team, installed: *installed, sample: *sample}, newDeps(*repo), out)
}

func newDeps(repo string) deps {
	command := func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = err.Error()
			}
			return strings.TrimSpace(stdout.String()), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), message)
		}
		// codesign prints its report on standard error, so both streams are the
		// command's output here.
		return strings.TrimSpace(stdout.String() + stderr.String()), nil
	}
	// A release package is around ten megabytes and the mirror can be slow;
	// the timeout has to cover the whole body, not a single read.
	client := &http.Client{Timeout: 5 * time.Minute}
	return deps{
		gh: func(args ...string) (string, error) { return command("gh", args...) },
		get: func(url string) ([]byte, error) {
			var last error
			for attempt := 0; attempt < 3; attempt++ {
				if attempt > 0 {
					time.Sleep(3 * time.Second)
				}
				request, err := http.NewRequest(http.MethodGet, url, nil)
				if err != nil {
					return nil, err
				}
				request.Header.Set("User-Agent", "motty-release-verify")
				response, err := client.Do(request)
				if err != nil {
					last = err
					continue
				}
				body, err := io.ReadAll(io.LimitReader(response.Body, 1<<29))
				response.Body.Close()
				if err != nil {
					last = err
					continue
				}
				if response.StatusCode != http.StatusOK {
					last = fmt.Errorf("%s: %s", url, response.Status)
					continue
				}
				return body, nil
			}
			return nil, last
		},
		download: func(tag, dir string) error {
			_, err := command("gh", "release", "download", tag, "--repo", repo, "--pattern", "SHA256SUMS", "--pattern", "SIGNATURES", "--dir", dir, "--clobber")
			return err
		},
		unzip: func(file, dir string) (string, error) {
			// The system unzip keeps the bundle's symlinks, which codesign needs.
			if _, err := command("unzip", "-q", file, "-d", dir); err != nil {
				return "", err
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				return "", err
			}
			for _, entry := range entries {
				if entry.IsDir() && strings.HasSuffix(entry.Name(), ".app") {
					return filepath.Join(dir, entry.Name()), nil
				}
			}
			return "", errors.New("the archive holds no .app bundle")
		},
		codesign: func(app string) (string, error) { return command("codesign", "-dv", "--verbose=2", app) },
		tempDir:  func() (string, error) { return os.MkdirTemp("", "motty-release-verify-") },
		read:     os.ReadFile,
	}
}

func verify(o options, d deps, out io.Writer) error {
	var problems []string
	failed := func(list []string) {
		for _, problem := range list {
			fmt.Fprintf(out, "  FAIL %s\n", problem)
		}
		problems = append(problems, list...)
	}
	passed := func(list []string, format string, args ...any) {
		if len(list) == 0 {
			fmt.Fprintf(out, "  ok   %s\n", fmt.Sprintf(format, args...))
		}
	}

	fmt.Fprintf(out, "== release %s in %s\n", o.tag, o.repo)
	release, err := getRelease(o, d)
	if err != nil {
		return err
	}
	releaseProblems := checkRelease(o, release)
	failed(releaseProblems)
	passed(releaseProblems, "public, named Motty %s, notes name one Viva commit", o.tag)
	if release.Draft {
		return fmt.Errorf("%s is still a draft; nothing else can be checked", o.tag)
	}

	version := strings.TrimPrefix(o.tag, "v")
	expected := expectedAssets(version)

	dir, err := d.tempDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := d.download(o.tag, dir); err != nil {
		return fmt.Errorf("download the manifests: %w", err)
	}
	checksumsBytes, err := d.read(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return err
	}
	signaturesBytes, err := d.read(filepath.Join(dir, "SIGNATURES"))
	if err != nil {
		return err
	}
	checksums, err := parseChecksums(checksumsBytes)
	if err != nil {
		return err
	}
	assetProblems := checkAssets(expected, release.Assets, checksums, signaturesBytes)
	failed(assetProblems)
	passed(assetProblems, "the release holds the %d assets of the matrix and every digest matches SHA256SUMS", len(expected))

	fmt.Fprintf(out, "== update feed %s\n", o.feed)
	feedBytes, err := d.get(o.feed)
	if err != nil {
		return fmt.Errorf("read the update feed: %w", err)
	}
	var feed updateFeed
	if err := json.Unmarshal(feedBytes, &feed); err != nil {
		return fmt.Errorf("parse the update feed: %w", err)
	}
	feedProblems := checkFeed(o, version, release.Body, feed, checksums)
	failed(feedProblems)
	passed(feedProblems, "the feed names %s with a signature for every updatable package", o.tag)

	mirror := o.mirror
	if mirror == "" {
		mirror = feed.Mirror
	}
	mirrorProblems := checkMirror(mirror, o.tag, checksumsBytes, signaturesBytes, d.get)
	failed(mirrorProblems)
	passed(mirrorProblems, "the mirror serves the same SHA256SUMS and SIGNATURES as the release")

	if o.sample {
		fmt.Fprintf(out, "== sample package from the mirror\n")
		sampleProblems := checkSample(o, version, mirror, checksums, d)
		failed(sampleProblems)
		passed(sampleProblems, "a package downloaded from the mirror matches SHA256SUMS and the app signature")
	}

	if len(problems) != 0 {
		return fmt.Errorf("%d checks failed", len(problems))
	}
	fmt.Fprintf(out, "\nall checks passed for %s\n", o.tag)
	return nil
}

type githubRelease struct {
	Draft   bool          `json:"draft"`
	Name    string        `json:"name"`
	TagName string        `json:"tag_name"`
	Body    string        `json:"body"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type updateFeed struct {
	Version     string     `json:"version"`
	Tag         string     `json:"tag"`
	PublishedAt string     `json:"publishedAt"`
	Mirror      string     `json:"mirror"`
	Notes       string     `json:"notes"`
	Files       []feedFile `json:"files"`
}

type feedFile struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
	URL       string `json:"url"`
}

func getRelease(o options, d deps) (githubRelease, error) {
	raw, err := d.gh("api", "repos/"+o.repo+"/releases/tags/"+o.tag)
	if err != nil {
		return githubRelease{}, fmt.Errorf("read the release: %w", err)
	}
	var release githubRelease
	if err := json.Unmarshal([]byte(raw), &release); err != nil {
		return githubRelease{}, fmt.Errorf("parse the release: %w", err)
	}
	return release, nil
}

func checkRelease(o options, release githubRelease) []string {
	var problems []string
	if release.Draft {
		problems = append(problems, "the release is still a draft")
	}
	if release.Name != "Motty "+o.tag {
		problems = append(problems, fmt.Sprintf("the release is named %q, not \"Motty %s\"", release.Name, o.tag))
	}
	if release.TagName != o.tag {
		problems = append(problems, fmt.Sprintf("the release names tag %q, not %q", release.TagName, o.tag))
	}
	var built []string
	for _, line := range strings.Split(release.Body, "\n") {
		if strings.HasPrefix(line, builtAgainst) {
			built = append(built, line)
		}
	}
	if len(built) != 1 || !builtAgainstLine.MatchString(built[0]) {
		problems = append(problems, fmt.Sprintf("the notes hold %d \"Built against vivaui/viva\" lines, want exactly one", len(built)))
	}
	if strings.TrimSpace(release.Body) == "" {
		problems = append(problems, "the release body is empty")
	}
	return problems
}

// expectedAssets is the release matrix; it must match release_assets in
// tools/release.sh.
func expectedAssets(version string) []string {
	assets := []string{
		"motty-" + version + "-darwin-arm64.dmg",
		"motty-" + version + "-darwin-amd64.dmg",
		"motty-" + version + "-darwin-arm64.zip",
		"motty-" + version + "-darwin-amd64.zip",
		"motty-" + version + "-linux-arm64.tar.gz",
		"motty-" + version + "-linux-amd64.tar.gz",
		"motty-" + version + "-windows-arm64-setup.exe",
		"motty-" + version + "-windows-amd64-setup.exe",
		"motty-" + version + "-windows-arm64.zip",
		"motty-" + version + "-windows-amd64.zip",
		"motty_" + version + "_arm64.deb",
		"motty_" + version + "_amd64.deb",
		"motty-" + version + "-linux-arm64.PKGBUILD",
		"motty-" + version + "-linux-amd64.PKGBUILD",
		"motty-" + version + "-linux-arm64.SRCINFO",
		"motty-" + version + "-linux-amd64.SRCINFO",
	}
	return append(assets, "SHA256SUMS", "SIGNATURES")
}

func parseChecksums(body []byte) (map[string]string, error) {
	checksums := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != 64 {
			return nil, fmt.Errorf("malformed SHA256SUMS line: %q", line)
		}
		checksums[strings.TrimPrefix(fields[1], "*")] = fields[0]
	}
	return checksums, nil
}

func checkAssets(expected []string, assets []githubAsset, checksums map[string]string, signatures []byte) []string {
	var problems []string
	have := make(map[string]githubAsset, len(assets))
	for _, asset := range assets {
		have[asset.Name] = asset
	}
	if len(assets) != len(expected) {
		problems = append(problems, fmt.Sprintf("the release holds %d assets, want %d", len(assets), len(expected)))
	}
	for _, name := range expected {
		asset, ok := have[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("missing asset %s", name))
			continue
		}
		if name == "SHA256SUMS" {
			// A manifest cannot carry its own hash; the SIGNATURES line covers
			// the other manifest and the package hashes cover the rest.
			continue
		}
		want, ok := checksums[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("SHA256SUMS does not list %s", name))
			continue
		}
		if asset.Digest != "sha256:"+want {
			problems = append(problems, fmt.Sprintf("%s: GitHub digest %q does not match SHA256SUMS", name, asset.Digest))
		}
	}
	wanted := make(map[string]bool, len(expected))
	for _, name := range expected {
		wanted[name] = true
	}
	for name := range have {
		if !wanted[name] {
			problems = append(problems, fmt.Sprintf("unexpected asset %s", name))
		}
	}
	sum := sha256.Sum256(signatures)
	if want, ok := checksums["SIGNATURES"]; !ok {
		problems = append(problems, "SHA256SUMS does not list SIGNATURES")
	} else if hex.EncodeToString(sum[:]) != want {
		problems = append(problems, "SIGNATURES does not match its SHA256SUMS entry")
	}
	// SHA256SUMS lists every asset of the matrix but itself.
	if len(checksums) != len(expected)-1 {
		problems = append(problems, fmt.Sprintf("SHA256SUMS lists %d files, want %d", len(checksums), len(expected)-1))
	}
	return problems
}

func checkFeed(o options, version, body string, feed updateFeed, checksums map[string]string) []string {
	var problems []string
	if feed.Version != version {
		problems = append(problems, fmt.Sprintf("the feed says version %q, not %q", feed.Version, version))
	}
	if feed.Tag != o.tag {
		problems = append(problems, fmt.Sprintf("the feed says tag %q, not %q", feed.Tag, o.tag))
	}
	if _, err := time.Parse(time.RFC3339, feed.PublishedAt); err != nil {
		problems = append(problems, fmt.Sprintf("the feed's publishedAt does not parse: %v", err))
	}
	if feed.Mirror == "" {
		problems = append(problems, "the feed does not name a mirror")
	}
	if normalizeNotes(feed.Notes) != normalizeNotes(body) {
		problems = append(problems, "the feed's notes differ from the release body")
	}
	for _, stable := range stableNames() {
		asset := assetForStable(version, stable)
		var entry feedFile
		found := false
		for _, file := range feed.Files {
			if file.Name == stable {
				entry, found = file, true
				break
			}
		}
		if !found {
			problems = append(problems, fmt.Sprintf("the feed does not list %s", stable))
			continue
		}
		if entry.Signature == "" {
			problems = append(problems, fmt.Sprintf("%s has no signature in the feed", stable))
		}
		if want, ok := checksums[asset]; !ok {
			problems = append(problems, fmt.Sprintf("SHA256SUMS does not list %s", asset))
		} else if entry.SHA256 != want {
			problems = append(problems, fmt.Sprintf("%s: the feed's sha256 does not match SHA256SUMS", stable))
		}
	}
	return problems
}

// stableNames are the download names an installed Motty asks for; keep them in
// step with update.StableName.
func stableNames() []string {
	var names []string
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, goarch := range []string{"arm64", "amd64"} {
			if name := update.StableName(goos, goarch); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

// assetForStable maps a stable download name back to the release asset it came
// from, for example motty-darwin-arm64.zip to motty-0.1.6-darwin-arm64.zip.
func assetForStable(version, stable string) string {
	return "motty-" + version + "-" + strings.TrimPrefix(stable, "motty-")
}

func checkMirror(mirror, tag string, checksums, signatures []byte, get func(string) ([]byte, error)) []string {
	if mirror == "" {
		return []string{"the feed does not name a mirror to check"}
	}
	base := strings.TrimSuffix(mirror, "/") + "/releases/" + tag + "/"
	var problems []string
	for _, test := range []struct {
		name string
		body []byte
	}{
		{"SHA256SUMS", checksums},
		{"SIGNATURES", signatures},
	} {
		served, err := get(base + test.name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("the mirror does not serve %s: %v", test.name, err))
			continue
		}
		if !bytes.Equal(served, test.body) {
			problems = append(problems, fmt.Sprintf("the mirror's %s differs from the release's", test.name))
		}
	}
	return problems
}

func checkSample(o options, version, mirror string, checksums map[string]string, d deps) []string {
	stable := update.StableName(runtime.GOOS, runtime.GOARCH)
	if stable == "" {
		return nil
	}
	if mirror == "" {
		return []string{"cannot sample a package without a mirror"}
	}
	asset := assetForStable(version, stable)
	url := strings.TrimSuffix(mirror, "/") + "/releases/" + o.tag + "/" + asset
	body, err := d.get(url)
	if err != nil {
		return []string{fmt.Sprintf("download %s from the mirror: %v", asset, err)}
	}
	sum := sha256.Sum256(body)
	if want := checksums[asset]; hex.EncodeToString(sum[:]) != want {
		return []string{fmt.Sprintf("%s from the mirror does not match SHA256SUMS", asset)}
	}
	var problems []string
	problems = append(problems, checkTeam(o, body, d)...)
	return problems
}

func checkTeam(o options, archive []byte, d deps) []string {
	dir, err := d.tempDir()
	if err != nil {
		return []string{fmt.Sprintf("check the app signature: %v", err)}
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "package.zip")
	if err := os.WriteFile(file, archive, 0o600); err != nil {
		return []string{fmt.Sprintf("check the app signature: %v", err)}
	}
	app, err := d.unzip(file, filepath.Join(dir, "app"))
	if err != nil {
		return []string{fmt.Sprintf("unpack the app: %v", err)}
	}
	output, err := d.codesign(app)
	if err != nil {
		return []string{fmt.Sprintf("read the app's signature: %v", err)}
	}
	team, err := teamFromCodesign(output)
	if err != nil {
		return []string{err.Error()}
	}
	want := o.team
	if want == "" {
		if _, statErr := os.Stat(o.installed); statErr != nil {
			return nil
		}
		installedOutput, err := d.codesign(o.installed)
		if err != nil {
			return []string{fmt.Sprintf("read the installed app's signature: %v", err)}
		}
		if want, err = teamFromCodesign(installedOutput); err != nil {
			return []string{fmt.Sprintf("installed app: %v", err)}
		}
	}
	if team != want {
		return []string{fmt.Sprintf("the release is signed by team %s, not %s", team, want)}
	}
	return nil
}

func teamFromCodesign(output string) (string, error) {
	var team, authority string
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "TeamIdentifier":
			team = value
		case "Authority":
			if authority == "" {
				authority = value
			}
		}
	}
	if team == "" || team == "not set" {
		return "", errors.New("the app has no Developer ID team")
	}
	if !strings.HasPrefix(authority, "Developer ID Application") {
		return "", fmt.Errorf("the app is signed by %q, not a Developer ID", authority)
	}
	return team, nil
}

// normalizeNotes drops the line the checksums job appends and trailing blank
// lines, so the feed's notes can be compared with the release body.
func normalizeNotes(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, builtAgainst) {
			continue
		}
		lines = append(lines, line)
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
