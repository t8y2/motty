// Command releaseprep prepares a Motty release without changing history: it
// checks the checkout, the last release, the tag and the exact-head tests, then
// bumps the two version files. It never commits, tags, pushes, or edits a
// GitHub release; the tag and the draft stay deliberate steps.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	manifestFile = "viva.toml"
	updateFile   = "internal/update/update.go"
	notesDir     = "docs/releases"
)

var (
	versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	stableTag      = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	manifestLine   = regexp.MustCompile(`(?m)^version = "[^"]*"`)
	updateLine     = regexp.MustCompile(`(?m)^const Current = "[^"]*"`)
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "releaseprep:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("releaseprep", flag.ContinueOnError)
	version := flags.String("version", "", "target version without the v, for example 0.1.7")
	root := flags.String("root", ".", "Motty checkout")
	allowCI := flags.Bool("allow-ci-pending", false, "prepare while the exact-head tests are queued or running (a failure still blocks)")
	dryRun := flags.Bool("dry-run", false, "run the checks and show the version change without writing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *version == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: releaseprep -version X.Y.Z [-root DIR] [-allow-ci-pending] [-dry-run]")
	}
	if !versionPattern.MatchString(*version) {
		return fmt.Errorf("%q is not a version like 0.1.7", *version)
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	return prepare(newDeps(absolute), options{version: *version, allowCIPending: *allowCI, dryRun: *dryRun}, out)
}

type options struct {
	version        string
	allowCIPending bool
	dryRun         bool
}

type deps struct {
	git   func(args ...string) (string, error)
	gh    func(args ...string) (string, error)
	read  func(file string) ([]byte, error)
	write func(file string, body []byte) error
}

func newDeps(root string) deps {
	command := func(name string) func(args ...string) (string, error) {
		return func(args ...string) (string, error) {
			cmd := exec.Command(name, args...)
			cmd.Dir = root
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				message := strings.TrimSpace(stderr.String())
				if message == "" {
					message = err.Error()
				}
				return strings.TrimSpace(stdout.String()), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), message)
			}
			return strings.TrimSpace(stdout.String()), nil
		}
	}
	return deps{
		git:  command("git"),
		gh:   command("gh"),
		read: func(file string) ([]byte, error) { return os.ReadFile(filepath.Join(root, file)) },
		write: func(file string, body []byte) error {
			return os.WriteFile(filepath.Join(root, file), body, 0o644)
		},
	}
}

type release struct {
	Tag        string `json:"tagName"`
	Draft      bool   `json:"isDraft"`
	Prerelease bool   `json:"isPrerelease"`
}

type testRun struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}

func prepare(d deps, o options, out io.Writer) error {
	tag := "v" + o.version
	fmt.Fprintf(out, "== preflight for %s\n", tag)

	notes := filepath.Join(notesDir, o.version+".md")
	// The optional English notes are written in the same commit as the Chinese
	// ones, so both may be untracked when the preflight runs.
	prepared := []string{notes, filepath.Join(notesDir, o.version+".en.md")}
	if status, err := d.git("status", "--porcelain"); err != nil {
		return err
	} else if dirty := dirtyExcept(status, prepared); dirty != "" {
		return fmt.Errorf("the worktree is not clean (%s); commit or undo those changes first", dirty)
	}
	branch, err := d.git("branch", "--show-current")
	if err != nil {
		return err
	}
	if branch != "main" {
		return fmt.Errorf("releases are prepared on main, not %q", branch)
	}
	if _, err := d.git("fetch", "-q", "origin", "main"); err != nil {
		return err
	}
	head, err := d.git("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	remote, err := d.git("rev-parse", "origin/main")
	if err != nil {
		return err
	}
	if head != remote {
		return fmt.Errorf("HEAD %s is not origin/main %s; fetch and fast-forward first", short(head), short(remote))
	}
	fmt.Fprintf(out, "  clean main at %s\n", short(head))

	previous, err := previousRelease(d)
	if err != nil {
		return err
	}
	manifest, err := d.read(manifestFile)
	if err != nil {
		return err
	}
	update, err := d.read(updateFile)
	if err != nil {
		return err
	}
	manifestVersion, err := currentManifestVersion(manifest)
	if err != nil {
		return err
	}
	updateVersion, err := currentUpdateVersion(update)
	if err != nil {
		return err
	}
	if manifestVersion != updateVersion {
		return fmt.Errorf("%s says %s but %s says %s; make them agree first", manifestFile, manifestVersion, updateFile, updateVersion)
	}
	switch {
	case previous == "":
		fmt.Fprintf(out, "  no published release yet; %s is the baseline\n", manifestVersion)
	case previous != "" && compareVersions(o.version, previous) <= 0:
		return fmt.Errorf("%s is not newer than the last release v%s", o.version, previous)
	case manifestVersion != previous:
		fmt.Fprintf(out, "  warning: %s says %s but the last release is v%s\n", manifestFile, manifestVersion, previous)
	case manifestVersion == o.version:
		return fmt.Errorf("%s is already %s", manifestFile, o.version)
	}
	if previous != "" {
		fmt.Fprintf(out, "  next after v%s (current %s)\n", previous, manifestVersion)
	}

	if _, err := d.git("rev-parse", "--verify", "refs/tags/"+tag); err == nil {
		return fmt.Errorf("tag %s already exists locally", tag)
	}
	remoteTag, err := d.git("ls-remote", "--tags", "origin", "refs/tags/"+tag)
	if err != nil {
		return err
	}
	if remoteTag != "" {
		return fmt.Errorf("tag %s already exists on origin", tag)
	}
	fmt.Fprintf(out, "  %s exists neither locally nor on origin\n", tag)

	body, err := d.read(notes)
	if err != nil {
		return fmt.Errorf("%s is missing: write the release notes before preparing (%w)", notes, err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("%s is empty: write the release notes before preparing", notes)
	}
	if strings.Contains(string(body), "Built against vivaui/viva") {
		return fmt.Errorf("%s carries the \"Built against\" line; the checksums job appends it, keep it out of the file", notes)
	}
	fmt.Fprintf(out, "  notes %s (%d bytes)\n", notes, len(body))

	if err := checkHeadTests(d, head, o.allowCIPending, out); err != nil {
		return err
	}

	if err := bump(d, o, manifest, update, out); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nnext: review `git diff`, commit %q, tag %s with the notes as its body,\n", "chore(release): "+o.version, tag)
	fmt.Fprintf(out, "push the commit and the tag, then create the draft release the workflow waits for.\n")
	return nil
}

func checkHeadTests(d deps, head string, allowPending bool, out io.Writer) error {
	raw, err := d.gh("run", "list", "--workflow", "tests.yml", "--commit", head, "--limit", "1", "--json", "status,conclusion,url")
	if err != nil {
		return err
	}
	var runs []testRun
	if err := json.Unmarshal([]byte(raw), &runs); err != nil {
		return fmt.Errorf("reading the tests run: %w", err)
	}
	if len(runs) == 0 {
		if allowPending {
			fmt.Fprintln(out, "  warning: no tests run found for this commit yet; continuing because --allow-ci-pending")
			return nil
		}
		return fmt.Errorf("no tests run found for %s yet; wait for it or pass -allow-ci-pending", short(head))
	}
	run := runs[0]
	switch {
	case run.Status != "completed":
		if allowPending {
			fmt.Fprintf(out, "  warning: tests for %s are %s; continuing because --allow-ci-pending\n", short(head), run.Status)
			return nil
		}
		return fmt.Errorf("tests for %s are %s; wait for them or pass -allow-ci-pending (%s)", short(head), run.Status, run.URL)
	case run.Conclusion != "success":
		return fmt.Errorf("tests for %s concluded %q; fix that before releasing (%s)", short(head), run.Conclusion, run.URL)
	}
	fmt.Fprintf(out, "  tests for %s passed\n", short(head))
	return nil
}

func previousRelease(d deps) (string, error) {
	raw, err := d.gh("release", "list", "--limit", "50", "--json", "tagName,isDraft,isPrerelease")
	if err != nil {
		return "", err
	}
	var releases []release
	if err := json.Unmarshal([]byte(raw), &releases); err != nil {
		return "", fmt.Errorf("reading the release list: %w", err)
	}
	best := ""
	for _, item := range releases {
		if item.Draft || item.Prerelease || !stableTag.MatchString(item.Tag) {
			continue
		}
		if best == "" || compareVersions(strings.TrimPrefix(item.Tag, "v"), best) > 0 {
			best = strings.TrimPrefix(item.Tag, "v")
		}
	}
	return best, nil
}

func bump(d deps, o options, manifest, update []byte, out io.Writer) error {
	nextManifest, err := setManifestVersion(manifest, o.version)
	if err != nil {
		return err
	}
	nextUpdate, err := setUpdateVersion(update, o.version)
	if err != nil {
		return err
	}
	if o.dryRun {
		fmt.Fprintf(out, "\n== dry run: version files would go to %s (nothing written)\n", o.version)
		return nil
	}
	if err := d.write(manifestFile, nextManifest); err != nil {
		return err
	}
	if err := d.write(updateFile, nextUpdate); err != nil {
		_ = d.write(manifestFile, manifest)
		return err
	}
	written, err := d.read(manifestFile)
	if err != nil {
		_ = d.write(manifestFile, manifest)
		_ = d.write(updateFile, update)
		return err
	}
	writtenUpdate, err := d.read(updateFile)
	if err != nil {
		_ = d.write(manifestFile, manifest)
		_ = d.write(updateFile, update)
		return err
	}
	if string(written) != string(nextManifest) || string(writtenUpdate) != string(nextUpdate) {
		_ = d.write(manifestFile, manifest)
		_ = d.write(updateFile, update)
		return fmt.Errorf("the version files did not keep the change; restored them")
	}
	fmt.Fprintf(out, "\n== version %s\n  %s\n  %s\n", o.version, manifestFile, updateFile)
	return nil
}

func setManifestVersion(body []byte, version string) ([]byte, error) {
	if matches := manifestLine.FindAllIndex(body, -1); len(matches) != 1 {
		return nil, fmt.Errorf("%s needs exactly one `version = \"...\"` line, found %d", manifestFile, len(matches))
	}
	return manifestLine.ReplaceAll(body, []byte(`version = "`+version+`"`)), nil
}

func setUpdateVersion(body []byte, version string) ([]byte, error) {
	if matches := updateLine.FindAllIndex(body, -1); len(matches) != 1 {
		return nil, fmt.Errorf("%s needs exactly one `const Current = \"...\"` line, found %d", updateFile, len(matches))
	}
	return updateLine.ReplaceAll(body, []byte(`const Current = "`+version+`"`)), nil
}

func currentManifestVersion(body []byte) (string, error) {
	match := regexp.MustCompile(`(?m)^version = "([^"]*)"`).FindSubmatch(body)
	if match == nil {
		return "", fmt.Errorf("no version in %s", manifestFile)
	}
	return string(match[1]), nil
}

func currentUpdateVersion(body []byte) (string, error) {
	match := regexp.MustCompile(`(?m)^const Current = "([^"]*)"`).FindSubmatch(body)
	if match == nil {
		return "", fmt.Errorf("no current version in %s", updateFile)
	}
	return string(match[1]), nil
}

// compareVersions orders two X.Y.Z versions; it returns -1, 0 or 1.
func compareVersions(a, b string) int {
	left, right := strings.Split(a, "."), strings.Split(b, ".")
	for index := 0; index < 3; index++ {
		var x, y int
		if index < len(left) {
			x, _ = strconv.Atoi(left[index])
		}
		if index < len(right) {
			y, _ = strconv.Atoi(right[index])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// dirtyExcept reports the first changed path other than the notes file being
// prepared, which is written before the version bump and stays untracked until
// the release commit.
func dirtyExcept(status string, allowed []string) string {
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		keep := false
		for _, path := range allowed {
			if strings.HasSuffix(line, " "+path) {
				keep = true
				break
			}
		}
		if keep {
			continue
		}
		return line
	}
	return ""
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
