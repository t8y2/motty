package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"aead.dev/minisign"
	"motty/internal/update"
	"motty/internal/updatesign"
)

func TestPrepareDebVerifiesSignedPackageAndMetadata(t *testing.T) {
	if !exists("/usr/bin/dpkg-deb") {
		t.Skip("dpkg-deb is not installed")
	}
	public, private, err := minisign.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	old := releaseKey
	releaseKey = func() minisign.PublicKey { return public }
	t.Cleanup(func() { releaseKey = old })
	for _, mode := range []string{"valid", "checksum", "signature", "package", "version", "architecture"} {
		t.Run(mode, func(t *testing.T) {
			name, version, arch := "motty", "1.2.3", runtime.GOARCH
			switch mode {
			case "package":
				name = "other"
			case "version":
				version = "1.2.2"
			case "architecture":
				arch = "all"
			}
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "DEBIAN"), 0755); err != nil {
				t.Fatal(err)
			}
			control := "Package: " + name + "\nVersion: " + version + "\nArchitecture: " + arch + "\nMaintainer: Test <test@example.invalid>\nDescription: update fixture\n"
			if err := os.WriteFile(filepath.Join(root, "DEBIAN", "control"), []byte(control), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "fixture.deb")
			if output, err := exec.Command("/usr/bin/dpkg-deb", "--build", root, path).CombinedOutput(); err != nil {
				t.Fatalf("building fixture: %s %v", output, err)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			server := serve(t, body)
			sum := sha256.Sum256(body)
			asset := update.Asset{Name: "motty_1.2.3_" + runtime.GOARCH + ".deb", URL: server.URL + "/pkg", Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
			asset.Signature, err = updatesign.Sign(private, bytes.NewReader(body), "1.2.3", asset.Name, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if mode == "checksum" {
				asset.SHA256 = strings.Repeat("0", 64)
			}
			if mode == "signature" {
				asset.Signature = "c2ln"
			}
			updater := &Updater{Dir: t.TempDir(), Client: server.Client(), install: Install{Kind: KindLinuxDeb, Exe: "/usr/bin/motty", Root: "/usr"}}
			staged, err := updater.Prepare(context.Background(), update.Release{Version: "1.2.3", Asset: asset}, nil, nil)
			if mode == "valid" {
				if err != nil || filepath.Base(staged.Path) != asset.Name || staged.Install.Kind != KindLinuxDeb {
					t.Fatalf("prepared %+v %v", staged, err)
				}
			} else if err == nil {
				t.Fatalf("%s package accepted", mode)
			}
			if mode == "checksum" && !errors.Is(err, ErrChecksum) {
				t.Fatalf("lost checksum error: %v", err)
			}
		})
	}
}

func TestSystemDebDetectionOnInstalledMotty(t *testing.T) {
	if !exists("/usr/bin/motty") {
		t.Skip("Motty is not installed on this test host")
	}
	if !registeredLinuxDeb("/usr/bin/motty") {
		t.Fatal("installed Motty DEB was not recognized")
	}
}
