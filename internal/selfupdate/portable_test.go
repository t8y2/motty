package selfupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"aead.dev/minisign"
	"motty/internal/update"
	"motty/internal/updatesign"
)

func portableArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, data := range entries {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o644)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func portableImage(architecture string) []byte {
	image := make([]byte, 128)
	copy(image, "MZ")
	binary.LittleEndian.PutUint32(image[0x3c:], 64)
	copy(image[64:], "PE\x00\x00")
	machine := uint16(0x8664)
	if architecture == "arm64" {
		machine = 0xaa64
	}
	binary.LittleEndian.PutUint16(image[68:], machine)
	return image
}

func TestSignedPortableUpgradePreservesTheDataAndReplacesTheProgramAndConpty(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "motty-data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "motty.exe")
	if err := os.WriteFile(exe, []byte("old image"), 0o755); err != nil {
		t.Fatal(err)
	}
	key := []byte("a saved vault key")
	if err := os.WriteFile(filepath.Join(dataDir, "vault.key"), key, 0o600); err != nil {
		t.Fatal(err)
	}
	conpty := filepath.Join(root, "conpty", "x64")
	if err := os.MkdirAll(conpty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conpty, "conpty.dll"), []byte("old conpty"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "my-notes.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	image := portableImage(runtime.GOARCH)
	archive := portableArchive(t, map[string][]byte{
		"motty.exe": image, "motty-data/README.txt": []byte("marker only"),
		"conpty/x64/conpty.dll": []byte("new conpty"),
	})
	public, private, err := minisign.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	previousKey, previousRun := releaseKey, run
	releaseKey = func() minisign.PublicKey { return public }
	run = func(context.Context, []string, string, ...string) (string, error) { return "NotSigned\n", nil }
	t.Cleanup(func() { releaseKey, run = previousKey, previousRun })
	server := serve(t, archive)
	asset := assetFor(server, archive)
	asset.Name = "motty-1.2.3-windows-" + runtime.GOARCH + ".zip"
	asset.Signature, err = updatesign.Sign(private, bytes.NewReader(archive), "1.2.3", asset.Name, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	updater := &Updater{Dir: filepath.Join(dataDir, "updates"), Client: server.Client(), install: Install{Kind: KindWindowsPortable, Root: root, Exe: exe}}
	if updater.Package() != update.PackagePortable {
		t.Fatal("the installed copy did not select the portable package")
	}
	staged, err := updater.Prepare(context.Background(), update.Release{Version: "1.2.3", Asset: asset}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Job{Kind: KindWindowsPortable, Root: root, Exe: exe, Staged: staged.Path}).applyPortable(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(exe); err != nil || !bytes.Equal(got, image) {
		t.Fatalf("program: %v, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dataDir, "vault.key")); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("vault key: %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dataDir, "README.txt")); err != nil || string(got) != "marker only" {
		t.Fatalf("portable note: %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(conpty, "conpty.dll")); err != nil || string(got) != "new conpty" {
		t.Fatalf("conpty: %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "my-notes.txt")); err != nil || string(got) != "keep" {
		t.Fatalf("unrelated file changed: %q, %v", got, err)
	}
}

func TestPortableUpgradeRejectsUnexpectedContentsAndArchitecture(t *testing.T) {
	image := portableImage(runtime.GOARCH)
	opposite := "arm64"
	if runtime.GOARCH == "arm64" {
		opposite = "amd64"
	}
	for name, entries := range map[string]map[string][]byte{
		"missing executable": {"motty-data/README.txt": []byte("marker")},
		"extra data":         {"motty.exe": image, "motty-data/README.txt": []byte("marker"), "motty-data/vault.key": []byte("overwrite")},
		"empty conpty":       {"motty.exe": image, "motty-data/README.txt": []byte("marker"), "conpty/x64/conpty.dll": {}},
		"path traversal":     {"motty.exe": image, "motty-data/README.txt": []byte("marker"), "../bad": []byte("bad")},
		"wrong architecture": {"motty.exe": portableImage(opposite), "motty-data/README.txt": []byte("marker")},
	} {
		t.Run(name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "portable.zip")
			if err := os.WriteFile(archive, portableArchive(t, entries), 0o600); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(t.TempDir(), "out")
			err := extractPortable(archive, dest)
			if err == nil {
				err = verifyPortableArchitecture(filepath.Join(dest, "motty.exe"))
			}
			if err == nil {
				t.Fatal("unsafe portable package was accepted")
			}
		})
	}
	root := t.TempDir()
	exe := filepath.Join(root, "motty.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(t.TempDir(), "motty.exe")
	if err := os.WriteFile(staged, image, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Job{Kind: KindWindowsPortable, Exe: exe, Root: root, Staged: staged}).applyPortable(); err != ErrUnsupported {
		t.Fatalf("missing portable marker: %v", err)
	}
	if got, _ := os.ReadFile(exe); !strings.EqualFold(string(got), "old") {
		t.Fatal("an unsupported update changed the program")
	}
}
