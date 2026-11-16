package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const (
	headSHA   = "0123456789abcdef0123456789abcdef01234567"
	originSHA = "0123456789abcdef0123456789abcdef01234567"
)

type fakeRepo struct {
	git    map[string]string
	gitErr map[string]error
	gh     map[string]string
	ghErr  map[string]error
	files  map[string]string
	writes map[string]string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		git:    map[string]string{},
		gitErr: map[string]error{},
		gh:     map[string]string{},
		ghErr:  map[string]error{},
		files:  map[string]string{},
		writes: map[string]string{},
	}
}

func (f *fakeRepo) deps() deps {
	lookup := func(table map[string]string, failures map[string]error) func(args ...string) (string, error) {
		return func(args ...string) (string, error) {
			key := strings.Join(args, " ")
			if err := failures[key]; err != nil {
				return "", err
			}
			value, ok := table[key]
			if !ok {
				return "", fmt.Errorf("unexpected call: %s", key)
			}
			return value, nil
		}
	}
	return deps{
		git: lookup(f.git, f.gitErr),
		gh:  lookup(f.gh, f.ghErr),
		read: func(file string) ([]byte, error) {
			value, ok := f.files[file]
			if !ok {
				return nil, errors.New("no such file")
			}
			return []byte(value), nil
		},
		write: func(file string, body []byte) error {
			f.files[file] = string(body)
			f.writes[file] = string(body)
			return nil
		},
	}
}

func (f *fakeRepo) standard(releases, runs string) {
	f.git["status --porcelain"] = ""
	f.git["branch --show-current"] = "main"
	f.git["fetch -q origin main"] = ""
	f.git["rev-parse HEAD"] = headSHA
	f.git["rev-parse origin/main"] = originSHA
	f.gitErr["rev-parse --verify refs/tags/v0.1.7"] = errors.New("not found")
	f.git["ls-remote --tags origin refs/tags/v0.1.7"] = ""
	f.gh["release list --limit 50 --json tagName,isDraft,isPrerelease"] = releases
	f.gh["run list --workflow tests.yml --commit "+headSHA+" --limit 1 --json status,conclusion,url"] = runs
	f.files[manifestFile] = "name = \"Motty\"\nversion = \"0.1.6\"\n"
	f.files[updateFile] = "package update\n\n// Current is the version of this build.\nconst Current = \"0.1.6\"\n"
	f.files["docs/releases/0.1.7.md"] = "Motty 0.1.7：something\n\n## 新功能\n"
	f.git["status --porcelain"] = "?? docs/releases/0.1.7.md"
}

const (
	stableReleases = `[{"tagName":"v0.1.6","isDraft":false,"isPrerelease":false},{"tagName":"v0.1.5","isDraft":false,"isPrerelease":false},{"tagName":"v0.1.7","isDraft":true,"isPrerelease":false},{"tagName":"v0.2.0","isDraft":false,"isPrerelease":true},{"tagName":"packages-v0.3.0","isDraft":false,"isPrerelease":false}]`
	greenRuns      = `[{"status":"completed","conclusion":"success","url":"https://example.test/run/1"}]`
)

func TestPrepareBumpsBothVersionFiles(t *testing.T) {
	repo := newFakeRepo()
	repo.standard(stableReleases, greenRuns)
	var out bytes.Buffer
	if err := prepare(repo.deps(), options{version: "0.1.7"}, &out); err != nil {
		t.Fatal(err)
	}
	if got := repo.writes[manifestFile]; got != "name = \"Motty\"\nversion = \"0.1.7\"\n" {
		t.Fatalf("manifest = %q", got)
	}
	if got := repo.writes[updateFile]; !strings.Contains(got, `const Current = "0.1.7"`) {
		t.Fatalf("update = %q", got)
	}
	for _, want := range []string{"next after v0.1.6", "0.1.7 exists neither locally nor on origin", "tests for 0123456 passed"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output misses %q:\n%s", want, out.String())
		}
	}
}

func TestPrepareBlocks(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeRepo, *options)
		want   string
	}{
		{
			name: "dirty worktree",
			mutate: func(r *fakeRepo, o *options) {
				r.git["status --porcelain"] = " M viva.toml"
			},
			want: "worktree is not clean",
		},
		{
			name: "wrong branch",
			mutate: func(r *fakeRepo, o *options) {
				r.git["branch --show-current"] = "feature"
			},
			want: "on main",
		},
		{
			name: "behind origin",
			mutate: func(r *fakeRepo, o *options) {
				r.git["rev-parse origin/main"] = "ffffffffffffffffffffffffffffffffffffffff"
			},
			want: "not origin/main",
		},
		{
			name: "tag exists locally",
			mutate: func(r *fakeRepo, o *options) {
				delete(r.gitErr, "rev-parse --verify refs/tags/v0.1.7")
				r.git["rev-parse --verify refs/tags/v0.1.7"] = headSHA
			},
			want: "already exists locally",
		},
		{
			name: "tag exists remotely",
			mutate: func(r *fakeRepo, o *options) {
				r.git["ls-remote --tags origin refs/tags/v0.1.7"] = "abc\trefs/tags/v0.1.7"
			},
			want: "already exists on origin",
		},
		{
			name: "english notes are allowed too",
			mutate: func(r *fakeRepo, o *options) {
				r.git["status --porcelain"] = "?? docs/releases/0.1.7.md\n?? docs/releases/0.1.7.en.md"
			},
			want: "",
		},
		{
			name: "another untracked file",
			mutate: func(r *fakeRepo, o *options) {
				r.git["status --porcelain"] = "?? docs/releases/0.1.7.md\n?? scratch.txt"
			},
			want: "not clean",
		},
		{
			name: "notes missing",
			mutate: func(r *fakeRepo, o *options) {
				delete(r.files, "docs/releases/0.1.7.md")
			},
			want: "release notes before preparing",
		},
		{
			name: "notes carry the viva line",
			mutate: func(r *fakeRepo, o *options) {
				r.files["docs/releases/0.1.7.md"] = "Motty 0.1.7\n\nBuilt against vivaui/viva abcdef.\n"
			},
			want: "checksums job appends it",
		},
		{
			name: "tests failed",
			mutate: func(r *fakeRepo, o *options) {
				r.gh["run list --workflow tests.yml --commit "+headSHA+" --limit 1 --json status,conclusion,url"] = `[{"status":"completed","conclusion":"failure","url":"https://example.test/run/2"}]`
			},
			want: "concluded \"failure\"",
		},
		{
			name: "tests still running",
			mutate: func(r *fakeRepo, o *options) {
				r.gh["run list --workflow tests.yml --commit "+headSHA+" --limit 1 --json status,conclusion,url"] = `[{"status":"in_progress","conclusion":"","url":"https://example.test/run/3"}]`
			},
			want: "wait for them",
		},
		{
			name: "tests still running but allowed",
			mutate: func(r *fakeRepo, o *options) {
				r.gh["run list --workflow tests.yml --commit "+headSHA+" --limit 1 --json status,conclusion,url"] = `[{"status":"in_progress","conclusion":"","url":"https://example.test/run/3"}]`
				o.allowCIPending = true
			},
			want: "",
		},
		{
			name: "version not newer",
			mutate: func(r *fakeRepo, o *options) {
				o.version = "0.1.6"
				r.git["status --porcelain"] = ""
				r.files[manifestFile] = "version = \"0.1.5\"\n"
				r.files[updateFile] = "const Current = \"0.1.5\"\n"
			},
			want: "not newer than the last release v0.1.6",
		},
		{
			name: "version files disagree",
			mutate: func(r *fakeRepo, o *options) {
				r.files[updateFile] = "const Current = \"0.1.5\"\n"
			},
			want: "make them agree first",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newFakeRepo()
			repo.standard(stableReleases, greenRuns)
			options := options{version: "0.1.7"}
			test.mutate(repo, &options)
			var out bytes.Buffer
			err := prepare(repo.deps(), options, &out)
			if test.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if len(repo.writes) != 0 {
				t.Fatalf("a blocked run wrote files: %v", repo.writes)
			}
		})
	}
}

func TestPreviousReleaseIgnoresDraftsPrereleasesAndPackageTags(t *testing.T) {
	repo := newFakeRepo()
	repo.gh["release list --limit 50 --json tagName,isDraft,isPrerelease"] = stableReleases
	got, err := previousRelease(repo.deps())
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.1.6" {
		t.Fatalf("previous = %q, want 0.1.6", got)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, test := range []struct {
		a, b string
		want int
	}{
		{"0.1.6", "0.1.5", 1},
		{"0.1.5", "0.1.6", -1},
		{"0.1.6", "0.1.6", 0},
		{"0.2.0", "0.1.9", 1},
		{"1.0.0", "0.9.9", 1},
	} {
		if got := compareVersions(test.a, test.b); got != test.want {
			t.Fatalf("compare(%s, %s) = %d, want %d", test.a, test.b, got, test.want)
		}
	}
}

func TestVersionWritersRejectMissingLines(t *testing.T) {
	if _, err := setManifestVersion([]byte("[app]\nname = \"Motty\"\n"), "0.1.7"); err == nil {
		t.Fatal("a manifest without a version should be refused")
	}
	if _, err := setUpdateVersion([]byte("package update\n"), "0.1.7"); err == nil {
		t.Fatal("an update file without Current should be refused")
	}
}
