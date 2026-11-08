//go:build darwin || windows || linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"motty/internal/buildinfo"
	"motty/internal/vault"
)

func TestLaunchArgumentsDropWhatTheSystemAdds(t *testing.T) {
	rest, background := launchArguments([]string{"-psn_0_12345", "--background", "ssh://h/", "user@h"})
	if !background || !reflect.DeepEqual(rest, []string{"ssh://h/", "user@h"}) {
		t.Fatalf("rest %q background %v", rest, background)
	}
	if rest, background := launchArguments(nil); background || len(rest) != 0 {
		t.Fatalf("rest %q background %v", rest, background)
	}
}

func TestADataDirectoryIsACopyOfItsOwn(t *testing.T) {
	if instanceID("") != applicationID || instanceID("/a") == instanceID("/b") || instanceID("/a") == applicationID {
		t.Fatalf("ids %q %q %q", instanceID(""), instanceID("/a"), instanceID("/b"))
	}
}

func TestClaimIdentityCanonicalizesDataDirectories(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "new", "nested")
	absoluteMissing, err := claimInstanceID(missing)
	if err != nil {
		t.Fatal(err)
	}
	relativeMissing, err := claimInstanceID(relativePathFrom(t, dir, missing))
	if err != nil {
		t.Fatal(err)
	}
	if relativeMissing != absoluteMissing {
		t.Fatal("the relative path was not anchored to the working directory")
	}

	if got, err := claimInstanceID(""); err != nil || (got == applicationID) == buildinfo.Development {
		t.Fatalf("default id %q, %v", got, err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Setenv("HOME", dir)
	}
	defaultDir, err := defaultDataDir()
	if err != nil {
		t.Fatal(err)
	}
	implicit, err := claimInstanceID("")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := claimInstanceID(defaultDir); err != nil || got != implicit {
		t.Fatalf("explicit default id %q, %v", got, err)
	}
	explicit, err := claimInstanceID(filepath.Join(dir, "new", "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if explicit != absoluteMissing {
		t.Fatalf("explicit new directory id %q, want %q", explicit, absoluteMissing)
	}

	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	realID, err := claimInstanceID(real)
	if err != nil {
		t.Fatal(err)
	}
	linkID, err := claimInstanceID(link)
	if err != nil {
		t.Fatal(err)
	}
	if realID != linkID {
		t.Fatalf("a symbolic link has another claim: %q != %q", linkID, realID)
	}

	deep := filepath.Join(real, "deep")
	if err := os.Mkdir(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(real, "other")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	deepLink := filepath.Join(dir, "deep-link")
	if err := os.Symlink(deep, deepLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	throughSuffixPath := deepLink + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "other"
	throughSuffix, err := canonicalPath(throughSuffixPath)
	if err != nil {
		t.Fatal(err)
	}
	wantThroughSuffix, err := canonicalPath(other)
	if err != nil {
		t.Fatal(err)
	}
	if throughSuffix != wantThroughSuffix {
		t.Fatalf("a symlink before .. resolved to %q, want %q", throughSuffix, wantThroughSuffix)
	}
	missingThroughSuffixPath := deepLink + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "missing"
	missingThroughSuffix, err := canonicalPath(missingThroughSuffixPath)
	if err != nil {
		t.Fatal(err)
	}
	realCanonical, err := canonicalPath(real)
	if err != nil {
		t.Fatal(err)
	}
	wantMissing := filepath.Join(realCanonical, "missing")
	if missingThroughSuffix != wantMissing {
		t.Fatalf("a missing suffix resolved to %q, want %q", missingThroughSuffix, wantMissing)
	}
}

func TestClaimIdentityCanonicalizesCaseAliases(t *testing.T) {
	dir := t.TempDir()
	actual := filepath.Join(dir, "Data")
	if err := os.Mkdir(actual, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "DATA")
	actualInfo, err := os.Stat(actual)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Stat(alias)
	if err != nil || !os.SameFile(actualInfo, aliasInfo) {
		t.Skip("the test volume distinguishes names by case")
	}
	actualID, err := claimInstanceID(actual)
	if err != nil {
		t.Fatal(err)
	}
	aliasID, err := claimInstanceID(alias)
	if err != nil {
		t.Fatal(err)
	}
	if aliasID != actualID {
		t.Fatalf("a case alias has another claim: %q != %q", aliasID, actualID)
	}
}

// TestClaimIdentityFollowsALinkedPath covers what a Scoop install looks like:
// the version directory sits behind a junction (a symlink off Windows) and the
// data folder is reached through it. Lstat describes the link itself, so a walk
// that only compared that with the directory it names reported no on-disk case
// and the app refused to start.
func TestClaimIdentityFollowsALinkedPath(t *testing.T) {
	dir := t.TempDir()
	version := filepath.Join(dir, "0.3.0")
	if err := os.MkdirAll(filepath.Join(version, "motty-data"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "current")
	if runtime.GOOS == "windows" {
		// A junction, as Scoop makes it, needs no privilege.
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, version).CombinedOutput(); err != nil {
			t.Skipf("cannot make a junction: %v: %s", err, out)
		}
	} else if err := os.Symlink(version, link); err != nil {
		t.Skipf("cannot make a symlink: %v", err)
	}

	// The volume is normalized to lower case on Windows, so the whole path is
	// compared without regard to case; the component names keep their spelling.
	if spelled, err := canonicalExistingCase(link); err != nil {
		t.Fatalf("a link component has no on-disk case: %v", err)
	} else if !strings.EqualFold(spelled, link) || filepath.Base(spelled) != filepath.Base(link) {
		t.Fatalf("canonicalExistingCase(%q) = %q", link, spelled)
	}

	dataDir := filepath.Join(link, "motty-data")
	first, err := claimInstanceID(dataDir)
	if err != nil {
		t.Fatalf("a data folder through a link has no claim: %v", err)
	}
	again, err := claimInstanceID(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Fatalf("the same path claimed %q and then %q", first, again)
	}
	if first == applicationID {
		t.Fatal("a linked data folder used the default claim")
	}
}

func TestClaimIdentityNormalizesVolumeCase(t *testing.T) {
	if got, want := canonicalVolume(`\\Server\Share`), `\\server\share`; got != want {
		t.Fatalf("UNC volume = %q, want %q", got, want)
	}
	if got, want := canonicalVolume(`C:`), `c:`; got != want {
		t.Fatalf("drive volume = %q, want %q", got, want)
	}
}

func TestClaimIdentityRejectsAPathItCannotResolve(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(parent, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := claimInstanceID(filepath.Join(parent, "child")); err == nil {
		t.Fatal("a path below a file got a claim")
	}
}

func TestHeadlessNeedsAnExplicitNonDefaultDataDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Setenv("HOME", dir)
	}
	if err := headlessClaim(""); err == nil {
		t.Fatal("headless accepted no data directory")
	}
	defaultDir, err := defaultDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(defaultDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := headlessClaim(defaultDir); err == nil {
		t.Fatal("headless accepted the default data directory")
	}
	caseAlias := filepath.Join(filepath.Dir(defaultDir), strings.ToUpper(filepath.Base(defaultDir)))
	defaultInfo, defaultErr := os.Stat(defaultDir)
	aliasInfo, aliasErr := os.Stat(caseAlias)
	if defaultErr == nil && aliasErr == nil && os.SameFile(defaultInfo, aliasInfo) && caseAlias != defaultDir {
		if err := headlessClaim(caseAlias); err == nil {
			t.Fatal("headless accepted a case alias of the default data directory")
		}
	}
	testDir := filepath.Join(dir, "headless-test")
	if err := os.Mkdir(testDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := headlessClaim(testDir); err != nil {
		t.Fatalf("headless rejected a test directory: %v", err)
	}
}

func relativePathFrom(t *testing.T, base, target string) string {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(current); err != nil {
			t.Fatal(err)
		}
	})
	relative, err := filepath.Rel(base, target)
	if err != nil {
		t.Fatal(err)
	}
	return relative
}

func TestBuildIdentityDoesNotChangeSharedDataNames(t *testing.T) {
	if buildinfo.ProductName == "" || applicationID != "dev.motty.app" || vault.KeyService != "motty vault" {
		t.Fatalf("product=%q application=%q vault=%q", buildinfo.ProductName, applicationID, vault.KeyService)
	}
}

// Windows reports a path below a file as missing rather than as a bad
// directory, so the walk up a missing tail has to stop on a directory whatever
// the resolver says.
func TestCanonicalPathRejectsAFileAnchoringAMissingTail(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(file, "child")
	// What Windows returns for the child, leaving the file itself resolvable.
	asWindows := func(path string) (string, error) {
		if path == child {
			return "", &os.PathError{Op: "lstat", Path: path, Err: os.ErrNotExist}
		}
		return filepath.EvalSymlinks(path)
	}
	if _, err := canonicalPathWith(child, asWindows); err == nil {
		t.Fatal("a path below a file was canonicalized")
	}
	if _, err := canonicalPathWith(child, filepath.EvalSymlinks); err == nil {
		t.Fatal("a path below a file was canonicalized by the resolver")
	}
}
