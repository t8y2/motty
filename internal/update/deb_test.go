package update

import (
	"context"
	"testing"
)

func TestDebFeedSelectionKeepsTheNativePackage(t *testing.T) {
	server := mirrorServer(t, `{"version":"1.2.3","tag":"v1.2.3","source":"https://github.com/o/r/releases/tag/v1.2.3","files":[{"name":"motty-linux-amd64.tar.gz","size":5,"sha256":"`+goodHash+`"},{"name":"motty_amd64.deb","size":10,"sha256":"`+goodHash+`","signature":"c2ln"},{"name":"motty_arm64.deb","size":12,"sha256":"`+goodHash+`","signature":"c2ln"}]}`)
	for _, arch := range []string{"amd64", "arm64"} {
		release, err := latestMirrorPackage(context.Background(), server.Client(), server.URL, "linux", arch, "deb")
		want := "motty_1.2.3_" + arch + ".deb"
		if err != nil || release.Asset.Name != want || release.Asset.URL != server.URL+"/releases/v1.2.3/"+want || !release.Asset.Signed() {
			t.Fatalf("DEB offer: %+v %v", release, err)
		}
	}
	archive, err := LatestMirror(context.Background(), server.Client(), server.URL, "linux", "amd64")
	if err != nil || archive.Asset.Name != "motty-1.2.3-linux-amd64.tar.gz" {
		t.Fatalf("archive changed: %+v %v", archive, err)
	}
	missing := mirrorServer(t, `{"version":"1.2.3","tag":"v1.2.3","source":"https://example.com","files":[{"name":"motty-linux-amd64.tar.gz","size":5,"sha256":"`+goodHash+`"}]}`)
	release, err := latestMirrorPackage(context.Background(), missing.Client(), missing.URL, "linux", "amd64", "deb")
	if err != nil || release.Asset.Has() || !release.PlatformPackageChecked {
		t.Fatalf("missing DEB silently fell back: %+v %v", release, err)
	}
}
