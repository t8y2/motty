package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPackages() downloads {
	return downloads{
		dmgARM64: asset{"motty-0.2.9-darwin-arm64.dmg", strings.Repeat("a", 64)},
		dmgAMD64: asset{"motty-0.2.9-darwin-amd64.dmg", strings.Repeat("b", 64)},
		zipARM64: asset{"motty-0.2.9-windows-arm64.zip", strings.Repeat("c", 64)},
		zipAMD64: asset{"motty-0.2.9-windows-amd64.zip", strings.Repeat("d", 64)},
	}
}

func TestRenderCaskInstallsTheNotarizedImage(t *testing.T) {
	cask := renderCask("0.2.9", testPackages())
	for _, want := range []string{
		`version "0.2.9"`,
		`sha256 arm:   "` + strings.Repeat("a", 64) + `",`,
		`intel: "` + strings.Repeat("b", 64) + `"`,
		"auto_updates true",
		"depends_on macos: :monterey",
		`app "Motty.app"`,
		"/releases/download/v#{version}/motty-#{version}-darwin-#{arch}.dmg",
	} {
		if !strings.Contains(cask, want) {
			t.Errorf("the cask has no %q:\n%s", want, cask)
		}
	}
	// The bundle inside the zip has no stapled ticket; only the image does.
	if strings.Contains(cask, ".zip") {
		t.Errorf("the cask must install the disk image, not the zip:\n%s", cask)
	}
}

func TestRenderManifestInstallsThePortableCopy(t *testing.T) {
	body, err := renderManifest("0.2.9", testPackages())
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version      string `json:"version"`
		Architecture map[string]struct {
			URL  string `json:"url"`
			Hash string `json:"hash"`
		} `json:"architecture"`
		Shortcuts  [][]string `json:"shortcuts"`
		Persist    string     `json:"persist"`
		Autoupdate struct {
			Architecture map[string]struct {
				URL string `json:"url"`
			} `json:"architecture"`
			Hash struct {
				URL  string `json:"url"`
				Find string `json:"find"`
			} `json:"hash"`
		} `json:"autoupdate"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatalf("%v:\n%s", err, body)
	}
	if manifest.Version != "0.2.9" {
		t.Errorf("version is %q", manifest.Version)
	}
	for architecture, want := range map[string]asset{
		"64bit": testPackages().zipAMD64,
		"arm64": testPackages().zipARM64,
	} {
		got, ok := manifest.Architecture[architecture]
		if !ok {
			t.Fatalf("no %s package", architecture)
		}
		if !strings.HasSuffix(got.URL, "/"+want.name) {
			t.Errorf("%s downloads %q, want %s", architecture, got.URL, want.name)
		}
		if got.Hash != want.sum {
			t.Errorf("%s hash is %q, want %q", architecture, got.Hash, want.sum)
		}
	}
	// The portable copy keeps its data in motty-data; Scoop persists that folder
	// across versions, which is what makes an upgrade keep the hosts.
	if manifest.Persist != "motty-data" {
		t.Errorf("persist is %q, want motty-data", manifest.Persist)
	}
	if len(manifest.Shortcuts) != 1 || len(manifest.Shortcuts[0]) != 2 || manifest.Shortcuts[0][0] != "motty.exe" {
		t.Errorf("shortcuts are %v", manifest.Shortcuts)
	}
	if !strings.Contains(manifest.Autoupdate.Hash.URL, "v$version/SHA256SUMS") {
		t.Errorf("autoupdate hashes from %q, want the release's SHA256SUMS", manifest.Autoupdate.Hash.URL)
	}
	if manifest.Autoupdate.Hash.Find != "$basename" {
		t.Errorf("autoupdate looks for %q", manifest.Autoupdate.Hash.Find)
	}
	if got := manifest.Autoupdate.Architecture["64bit"].URL; !strings.Contains(got, "motty-$version-windows-amd64.zip") {
		t.Errorf("64bit autoupdate points at %q", got)
	}
}

func TestPackagesOfRequiresEveryDownload(t *testing.T) {
	sums := map[string]string{
		"motty-0.2.9-darwin-arm64.dmg": strings.Repeat("a", 64),
		"motty-0.2.9-darwin-amd64.dmg": strings.Repeat("b", 64),
	}
	_, err := packagesOf("0.2.9", sums)
	if err == nil {
		t.Fatal("a release without the Windows packages was accepted")
	}
	for _, want := range []string{"motty-0.2.9-windows-arm64.zip", "motty-0.2.9-windows-amd64.zip"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %s: %v", want, err)
		}
	}
}

func TestReadSums(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SHA256SUMS")
	body := strings.Repeat("a", 64) + "  motty-0.2.9-darwin-arm64.dmg\n" +
		strings.Repeat("b", 64) + " *motty-0.2.9-windows-amd64.zip\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sums, err := readSums(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"motty-0.2.9-darwin-arm64.dmg":  strings.Repeat("a", 64),
		"motty-0.2.9-windows-amd64.zip": strings.Repeat("b", 64),
	} {
		if sums[name] != want {
			t.Errorf("%s is %q, want %q", name, sums[name], want)
		}
	}

	if err := os.WriteFile(path, []byte("not a checksum line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSums(path); err == nil {
		t.Error("a malformed line was accepted")
	}
}

func TestRunWritesBothFiles(t *testing.T) {
	dir := t.TempDir()
	var sums strings.Builder
	for _, file := range testPackages().all() {
		sums.WriteString(file.sum + "  " + file.name + "\n")
	}
	sumsPath := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sumsPath, []byte(sums.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if err := run([]string{"-version", "v0.2.9", "-sums", sumsPath, "-out", out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	cask, err := os.ReadFile(filepath.Join(out, caskFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cask), `version "0.2.9"`) {
		t.Errorf("the written cask has no version:\n%s", cask)
	}
	manifest, err := os.ReadFile(filepath.Join(out, manifestFile))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(manifest) {
		t.Errorf("the written manifest is not JSON:\n%s", manifest)
	}
}

// TestCaskMinimumMatchesVivaManifest keeps depends_on honest: it must be the
// oldest macOS the bundle says it runs on, which is [mac] min_system in
// viva.toml, defaulting to 12.0 when unset.
func TestCaskMinimumMatchesVivaManifest(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "viva.toml"))
	if err != nil {
		t.Fatal(err)
	}
	minimum := "12.0"
	section := ""
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && section == "mac" && strings.TrimSpace(key) == "min_system" {
			minimum = strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	if minimum != macOSMinimum {
		t.Fatalf("viva.toml runs on macOS %s but the cask says %s (%s)", minimum, macOSMinimum, macOSSymbol)
	}
}
