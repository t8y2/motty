package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"runtime"
	"strings"
	"testing"
)

const testVersion = "0.1.6"

func checksumsFor(t *testing.T, signatures []byte) (map[string]string, []byte) {
	t.Helper()
	sum := sha256.Sum256(signatures)
	lines := []string{
		strings.Repeat("a", 64) + "  motty-0.1.6-darwin-arm64.dmg",
		strings.Repeat("b", 64) + "  motty-0.1.6-darwin-amd64.dmg",
		strings.Repeat("c", 64) + "  motty-0.1.6-darwin-arm64.zip",
		strings.Repeat("d", 64) + "  motty-0.1.6-darwin-amd64.zip",
		strings.Repeat("e", 64) + "  motty-0.1.6-linux-arm64.tar.gz",
		strings.Repeat("f", 64) + "  motty-0.1.6-linux-amd64.tar.gz",
		strings.Repeat("1", 64) + "  motty-0.1.6-windows-arm64-setup.exe",
		strings.Repeat("2", 64) + "  motty-0.1.6-windows-amd64-setup.exe",
		strings.Repeat("5", 64) + "  motty-0.1.6-windows-arm64.zip",
		strings.Repeat("6", 64) + "  motty-0.1.6-windows-amd64.zip",
		strings.Repeat("3", 64) + "  motty_0.1.6_arm64.deb",
		strings.Repeat("4", 64) + "  motty_0.1.6_amd64.deb",
		strings.Repeat("7", 64) + "  motty-0.1.6-linux-arm64.PKGBUILD",
		strings.Repeat("8", 64) + "  motty-0.1.6-linux-amd64.PKGBUILD",
		strings.Repeat("9", 64) + "  motty-0.1.6-linux-arm64.SRCINFO",
		strings.Repeat("a", 64) + "  motty-0.1.6-linux-amd64.SRCINFO",
		hex.EncodeToString(sum[:]) + "  SIGNATURES",
	}
	body := []byte(strings.Join(lines, "\n") + "\n")
	checksums, err := parseChecksums(body)
	if err != nil {
		t.Fatal(err)
	}
	return checksums, body
}

func TestParseChecksums(t *testing.T) {
	body := []byte(strings.Repeat("a", 64) + "  *file.tar.gz\n")
	checksums, err := parseChecksums(body)
	if err != nil {
		t.Fatal(err)
	}
	if checksums["file.tar.gz"] != strings.Repeat("a", 64) {
		t.Fatalf("checksums = %v", checksums)
	}
	if _, err := parseChecksums([]byte("not a checksum line\n")); err == nil {
		t.Fatal("a malformed line should be refused")
	}
}

func TestExpectedAssetsCoversTheMatrix(t *testing.T) {
	assets := expectedAssets(testVersion)
	if len(assets) != 18 {
		t.Fatalf("expected 18 assets, got %d", len(assets))
	}
	for _, asset := range []string{"motty-0.1.6-linux-amd64.tar.gz", "motty_0.1.6_arm64.deb", "motty-0.1.6-windows-amd64.zip", "SHA256SUMS", "SIGNATURES"} {
		found := false
		for _, want := range assets {
			if asset == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s is missing from the matrix", asset)
		}
	}
}

func TestCheckAssets(t *testing.T) {
	signatures := []byte("one signature line\n")
	checksums, _ := checksumsFor(t, signatures)
	assets := make([]githubAsset, 0, 18)
	for name, digest := range checksums {
		if name == "SIGNATURES" {
			continue
		}
		assets = append(assets, githubAsset{Name: name, Digest: "sha256:" + digest})
	}
	assets = append(assets, githubAsset{Name: "SIGNATURES", Digest: "sha256:" + checksums["SIGNATURES"]})
	assets = append(assets, githubAsset{Name: "SHA256SUMS"})
	expected := expectedAssets(testVersion)
	if problems := checkAssets(expected, assets, checksums, signatures); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	build := func(mutate func(*[]githubAsset)) []string {
		copied := append([]githubAsset(nil), assets...)
		mutate(&copied)
		return checkAssets(expected, copied, checksums, signatures)
	}
	if problems := build(func(list *[]githubAsset) { (*list)[0].Digest = "sha256:deadbeef" }); len(problems) == 0 {
		t.Fatal("a wrong digest should be refused")
	}
	if problems := build(func(list *[]githubAsset) { *list = (*list)[1:] }); len(problems) == 0 {
		t.Fatal("a missing asset should be refused")
	}
	if problems := build(func(list *[]githubAsset) {
		*list = append(*list, githubAsset{Name: "motty-0.1.6-extra.pkg"})
	}); len(problems) == 0 {
		t.Fatal("an unexpected asset should be refused")
	}
	if problems := checkAssets(expected, assets, checksums, []byte("different signatures")); len(problems) == 0 {
		t.Fatal("a SIGNATURES mismatch should be refused")
	}
}

func TestCheckFeed(t *testing.T) {
	signatures := []byte("one signature line\n")
	checksums, _ := checksumsFor(t, signatures)
	body := "Motty 0.1.6：notes\n\n## 新功能\n\n- something\n"
	feed := updateFeed{
		Version:     testVersion,
		Tag:         "v" + testVersion,
		PublishedAt: "2026-10-28T14:47:54Z",
		Mirror:      "https://dl.motty.page",
		Notes:       body + "\nBuilt against vivaui/viva `0123456789abcdef0123456789abcdef01234567`.\n",
	}
	for _, stable := range stableNames() {
		asset := assetForStable(testVersion, stable)
		feed.Files = append(feed.Files, feedFile{Name: stable, SHA256: checksums[asset], Signature: "sig"})
	}
	if problems := checkFeed(options{tag: "v" + testVersion}, testVersion, body, feed, checksums); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	mutate := func(change func(*updateFeed)) []string {
		copied := feed
		copied.Files = append([]feedFile(nil), feed.Files...)
		change(&copied)
		return checkFeed(options{tag: "v" + testVersion}, testVersion, body, copied, checksums)
	}
	if problems := mutate(func(f *updateFeed) { f.Version = "0.1.5" }); len(problems) == 0 {
		t.Fatal("a wrong feed version should be refused")
	}
	if problems := mutate(func(f *updateFeed) { f.Notes = "different notes" }); len(problems) == 0 {
		t.Fatal("notes that differ from the body should be refused")
	}
	if problems := mutate(func(f *updateFeed) { f.Files[0].Signature = "" }); len(problems) == 0 {
		t.Fatal("a package without a signature should be refused")
	}
	if problems := mutate(func(f *updateFeed) { f.Files[0].SHA256 = strings.Repeat("0", 64) }); len(problems) == 0 {
		t.Fatal("a feed hash that differs from SHA256SUMS should be refused")
	}
	if problems := mutate(func(f *updateFeed) { f.Files = f.Files[1:] }); len(problems) == 0 {
		t.Fatal("a missing stable name should be refused")
	}
}

func TestCheckMirror(t *testing.T) {
	checksums := []byte("sums")
	signatures := []byte("sigs")
	served := func(overrides map[string][]byte) func(string) ([]byte, error) {
		return func(url string) ([]byte, error) {
			for suffix, body := range overrides {
				if strings.HasSuffix(url, "/"+suffix) {
					return body, nil
				}
			}
			return nil, nil
		}
	}
	if problems := checkMirror("https://dl.motty.page", "v0.1.6", checksums, signatures, served(map[string][]byte{
		"SHA256SUMS": checksums, "SIGNATURES": signatures,
	})); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if problems := checkMirror("https://dl.motty.page", "v0.1.6", checksums, signatures, served(map[string][]byte{
		"SHA256SUMS": []byte("other"), "SIGNATURES": signatures,
	})); len(problems) == 0 {
		t.Fatal("a different mirrored manifest should be refused")
	}
}

func TestNormalizeNotes(t *testing.T) {
	body := "notes\n\nBuilt against vivaui/viva `abc1234`.\n\n"
	if got := normalizeNotes(body); got != "notes" {
		t.Fatalf("normalized = %q", got)
	}
}

func TestTeamFromCodesign(t *testing.T) {
	output := "Executable=/x\nIdentifier=dev.motty.app\nAuthority=Developer ID Application: Name (TVDM965TDL)\nAuthority=Developer ID Certification Authority\nTeamIdentifier=TVDM965TDL\n"
	team, err := teamFromCodesign(output)
	if err != nil || team != "TVDM965TDL" {
		t.Fatalf("team = %q, err = %v", team, err)
	}
	if _, err := teamFromCodesign("Signature=adhoc\nTeamIdentifier=not set\n"); err == nil {
		t.Fatal("an unsigned app should be refused")
	}
	if _, err := teamFromCodesign("Authority=Apple Development: x (Y)\nTeamIdentifier=Y\n"); err == nil {
		t.Fatal("a non-Developer-ID signature should be refused")
	}
}

func TestAssetForStable(t *testing.T) {
	for stable, want := range map[string]string{
		"motty-darwin-arm64.zip":        "motty-0.1.6-darwin-arm64.zip",
		"motty-linux-amd64.tar.gz":      "motty-0.1.6-linux-amd64.tar.gz",
		"motty-windows-arm64-setup.exe": "motty-0.1.6-windows-arm64-setup.exe",
	} {
		if got := assetForStable(testVersion, stable); got != want {
			t.Fatalf("assetForStable(%q) = %q, want %q", stable, got, want)
		}
	}
}

func TestCodesignDependencyKeepsTheReport(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign only exists on macOS")
	}
	const installed = "/Applications/Motty.app"
	if _, err := os.Stat(installed); err != nil {
		t.Skip("no installed app to read a signature from")
	}
	output, err := newDeps(defaultRepo).codesign(installed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := teamFromCodesign(output); err != nil {
		t.Fatalf("the codesign dependency lost the report: %v", err)
	}
}
