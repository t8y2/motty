package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"aead.dev/minisign"
	"motty/internal/update"
	"motty/internal/updatesign"
)

func assetFor(server *httptest.Server, body []byte) update.Asset {
	sum := sha256.Sum256(body)
	return update.Asset{Name: "motty-1.0.0-test.zip", URL: server.URL + "/pkg", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}
}

func serve(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(body) }))
	t.Cleanup(server.Close)
	return server
}

func TestAPackageMustCarryTheReleaseSignature(t *testing.T) {
	publicKey, privateKey, err := minisign.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	previous := releaseKey
	releaseKey = func() minisign.PublicKey { return publicKey }
	t.Cleanup(func() { releaseKey = previous })

	body := []byte("the package bytes")
	path := filepath.Join(t.TempDir(), "motty-1.0.0-test.zip")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	signature, err := updatesign.Sign(privateKey, bytes.NewReader(body), "1.0.0", "motty-1.0.0-test.zip", time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	good := update.Release{Version: "1.0.0", Asset: update.Asset{Name: "motty-1.0.0-test.zip", Signature: signature}}
	if err := verifyReleasePackage(path, good); err != nil {
		t.Fatalf("a signed package was refused: %v", err)
	}

	for name, release := range map[string]update.Release{
		"no signature":  {Version: "1.0.0", Asset: update.Asset{Name: "motty-1.0.0-test.zip"}},
		"other version": {Version: "1.0.1", Asset: update.Asset{Name: "motty-1.0.0-test.zip", Signature: signature}},
		"other file":    {Version: "1.0.0", Asset: update.Asset{Name: "motty-1.0.0-other.zip", Signature: signature}},
		"broken base64": {Version: "1.0.0", Asset: update.Asset{Name: "motty-1.0.0-test.zip", Signature: "!!"}},
	} {
		if err := verifyReleasePackage(path, release); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyReleasePackage(path, good); err == nil {
		t.Error("a changed package was accepted")
	}
}

func TestADownloadMustBeExactlyWhatTheReleaseNames(t *testing.T) {
	body := bytes.Repeat([]byte("motty"), 1000)
	server := serve(t, body)
	good := assetFor(server, body)

	dir := t.TempDir()
	var last Progress
	path, err := Download(context.Background(), server.Client(), good, dir, func(p Progress) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, body) || last.Done != good.Size {
		t.Fatalf("file differs or progress %+v", last)
	}

	for name, mutate := range map[string]func(update.Asset) update.Asset{
		"altered":  func(a update.Asset) update.Asset { a.SHA256 = strings.Repeat("0", 64); return a },
		"too long": func(a update.Asset) update.Asset { a.Size--; return a },
		"too short": func(a update.Asset) update.Asset {
			a.Size++
			return a
		},
	} {
		dir := t.TempDir()
		if _, err := Download(context.Background(), server.Client(), mutate(good), dir, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("%s: left %d files behind", name, len(entries))
		}
	}
	if !errors.Is(func() error {
		bad := good
		bad.SHA256 = strings.Repeat("0", 64)
		_, err := Download(context.Background(), server.Client(), bad, t.TempDir(), nil)
		return err
	}(), ErrChecksum) {
		t.Error("an altered download is not reported as a checksum failure")
	}
}

func zipOf(t *testing.T, entries map[string]string, symlink string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.zip")
	file, _ := os.Create(path)
	writer := zip.NewWriter(file)
	for name, body := range entries {
		w, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if symlink != "" {
		header := &zip.FileHeader{Name: symlink}
		header.SetMode(os.ModeSymlink | 0o777)
		w, _ := writer.CreateHeader(header)
		w.Write([]byte("/etc/passwd"))
	}
	writer.Close()
	file.Close()
	return path
}

func TestAnArchiveCannotWriteOutsideItsDirectory(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out")
	if err := ExtractZip(zipOf(t, map[string]string{"Motty.app/Contents/MacOS/motty": "x"}, ""), dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Motty.app", "Contents", "MacOS", "motty")); err != nil {
		t.Fatal(err)
	}
	for name, archive := range map[string]string{
		"parent":   zipOf(t, map[string]string{"../evil": "x"}, ""),
		"absolute": zipOf(t, map[string]string{"/tmp/evil": "x"}, ""),
		"symlink":  zipOf(t, map[string]string{"ok": "x"}, "link"),
	} {
		if err := ExtractZip(archive, filepath.Join(t.TempDir(), "out")); err == nil {
			t.Errorf("zip %s accepted", name)
		}
	}

	tarball := func(header tar.Header, body string) string {
		var buffer bytes.Buffer
		zipped := gzip.NewWriter(&buffer)
		writer := tar.NewWriter(zipped)
		header.Size = int64(len(body))
		writer.WriteHeader(&header)
		writer.Write([]byte(body))
		writer.Close()
		zipped.Close()
		path := filepath.Join(t.TempDir(), "a.tar.gz")
		os.WriteFile(path, buffer.Bytes(), 0o600)
		return path
	}
	if err := ExtractTarGz(tarball(tar.Header{Name: "motty/install.sh", Typeflag: tar.TypeReg, Mode: 0o755}, "#!/bin/sh"), filepath.Join(t.TempDir(), "out")); err != nil {
		t.Fatal(err)
	}
	for name, header := range map[string]tar.Header{
		"parent":  {Name: "../evil", Typeflag: tar.TypeReg, Mode: 0o644},
		"symlink": {Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
	} {
		if err := ExtractTarGz(tarball(header, ""), filepath.Join(t.TempDir(), "out")); err == nil {
			t.Errorf("tar %s accepted", name)
		}
	}
}

func TestOnlyAStandardUserInstallationReplacesItself(t *testing.T) {
	writable := func(string) bool { return true }
	existing := func(set ...string) func(string) bool {
		return func(path string) bool {
			for _, want := range set {
				if filepath.ToSlash(path) == want {
					return true
				}
			}
			return false
		}
	}
	for _, test := range []struct {
		name string
		exe  string
		env  Env
		kind Kind
		why  Reason
	}{
		{"mac app", "/Applications/Motty.app/Contents/MacOS/motty", Env{GOOS: "darwin", Writable: writable}, KindMacApp, ReasonNone},
		{"mac bare binary", "/tmp/motty", Env{GOOS: "darwin", Writable: writable}, KindNone, ReasonNotUserInstall},
		{"mac read only", "/Applications/Motty.app/Contents/MacOS/motty", Env{GOOS: "darwin", Writable: func(string) bool { return false }}, KindNone, ReasonNotWritable},
		{"mac translocated", "/private/var/folders/x/AppTranslocation/ABC/d/Motty.app/Contents/MacOS/motty", Env{GOOS: "darwin", Writable: writable}, KindNone, ReasonTranslocated},
		{"linux user prefix", "/home/me/.local/bin/motty", Env{GOOS: "linux", Writable: writable, Exists: existing("/home/me/.local/share/applications/motty.desktop")}, KindLinuxPrefix, ReasonNone},
		{"linux deb", "/usr/bin/motty", Env{GOOS: "linux", Writable: func(string) bool { return false }, Exists: existing()}, KindNone, ReasonSystemInstall},
		{"linux prefix with a space", "/home/my user/.local/bin/motty", Env{GOOS: "linux", Writable: writable, Exists: existing("/home/my user/.local/share/applications/motty.desktop")}, KindNone, ReasonNotUserInstall},
		{"linux system package", "/usr/bin/motty", Env{GOOS: "linux", Writable: writable}, KindNone, ReasonSystemInstall},
		{"other system", "/x/motty", Env{GOOS: "plan9"}, KindNone, ReasonNotUserInstall},
	} {
		install, why := Detect(test.exe, test.env)
		if install.Kind != test.kind || why != test.why {
			t.Errorf("%s: %v %v, want %v %v", test.name, install.Kind, why, test.kind, test.why)
		}
	}
}

func TestWindowsInstallsAreRecognisedByTheirInstallerDirectory(t *testing.T) {
	env := Env{GOOS: "windows", LocalAppData: `C:\Users\me\AppData\Local`, Writable: func(string) bool { return true }, Exists: func(string) bool { return true }}
	if filepath.Separator != '\\' {
		t.Skip("Windows paths")
	}
	if install, why := Detect(`C:\Users\me\AppData\Local\Programs\Motty\motty.exe`, env); install.Kind != KindWindowsSetup || why != ReasonNone {
		t.Errorf("installer directory: %v %v", install.Kind, why)
	}
	if _, why := Detect(`C:\Tools\motty.exe`, env); why != ReasonNotUserInstall {
		t.Errorf("portable: %v", why)
	}
	env.ProgramFiles = []string{`C:\Program Files`}
	if _, why := Detect(`C:\Program Files\Motty\motty.exe`, env); why != ReasonSystemInstall {
		t.Errorf("Program Files: %v", why)
	}
}

func TestSignaturesAreReadFromTheSystemsOwnReports(t *testing.T) {
	signed := "Executable=/x\nIdentifier=dev.motty.app\nAuthority=Developer ID Application: Someone (ABCDE12345)\nAuthority=Developer ID Certification Authority\nTeamIdentifier=ABCDE12345\n"
	if identity, err := parseCodesign(signed); err != nil || identity.Team != "ABCDE12345" {
		t.Errorf("signed: %+v %v", identity, err)
	}
	for name, report := range map[string]string{
		"ad hoc":      "Signature=adhoc\nTeamIdentifier=not set\n",
		"development": "Authority=Apple Development: x (Y)\nTeamIdentifier=ABCDE12345\n",
		"empty":       "",
	} {
		if _, err := parseCodesign(report); !errors.Is(err, ErrUnsigned) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if status, identity := parseSignature("Valid\r\nCN=Someone, O=Motty\r\n"); status != "Valid" || identity.Subject != "CN=Someone, O=Motty" {
		t.Errorf("powershell: %q %+v", status, identity)
	}
	if status, identity := parseSignature("NotSigned\r\n"); status != "NotSigned" || identity.Subject != "" {
		t.Errorf("unsigned: %q %+v", status, identity)
	}
}

func TestAJobSurvivesItsCommandLine(t *testing.T) {
	job := Job{Kind: KindMacApp, PID: 4242, Staged: "/a b/New.app", Root: "/Applications/Motty.app", Exe: "/Applications/Motty.app/Contents/MacOS/motty", Dir: "/d"}
	args := job.Arguments()
	if args[0] != Command {
		t.Fatalf("first argument %q", args[0])
	}
	got, err := ParseJob(args[1:])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, job) {
		t.Fatalf("got %+v, want %+v", got, job)
	}
	if _, err := ParseJob([]string{"--kind=mac-app"}); err == nil {
		t.Fatal("an incomplete job was accepted")
	}
}

func fakeApp(t *testing.T, dir, name, marker string) string {
	t.Helper()
	app := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "MacOS", "motty"), []byte(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestTheNewBundleReplacesTheOldOneAndAFailureLeavesTheOldOne(t *testing.T) {
	apps, staging := t.TempDir(), t.TempDir()
	root := fakeApp(t, apps, "Motty.app", "old")
	next := fakeApp(t, staging, "Motty.app", "new")
	job := Job{Kind: KindMacApp, Staged: next, Root: root, Dir: staging}
	if err := job.applyMac(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "Contents", "MacOS", "motty")); string(data) != "new" {
		t.Fatalf("installed %q", data)
	}
	if entries, _ := os.ReadDir(apps); len(entries) != 1 {
		t.Fatalf("leftovers beside the app: %v", entries)
	}

	// A bundle that is missing cannot be placed; the app that is there stays.
	failing := Job{Kind: KindMacApp, Staged: filepath.Join(staging, "Gone.app"), Root: root, Dir: staging}
	if err := failing.applyMac(); err == nil {
		t.Fatal("a missing bundle was installed")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "Contents", "MacOS", "motty")); string(data) != "new" {
		t.Fatalf("the installed app was disturbed: %q", data)
	}
}

func TestAFailedUpdateIsRememberedOnceAndTheOldProgramIsStarted(t *testing.T) {
	dir := t.TempDir()
	started := 0
	job := Job{Kind: KindMacApp, Dir: dir, starter: func() error { started++; return nil }}
	if err := job.fail(errors.New("disk full"), true); err == nil || started != 1 {
		t.Fatalf("err %v, started %d", err, started)
	}
	if reason := TakeFailure(dir); reason != "disk full" {
		t.Fatalf("reason %q", reason)
	}
	if reason := TakeFailure(dir); reason != "" {
		t.Fatalf("the failure came back: %q", reason)
	}
}

func TestAHelperThatDiesAtOnceIsReportedAndOneThatIsReadyIsNot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the helpers here are shell scripts")
	}
	launch := func(script string) error {
		dir := t.TempDir()
		exe := filepath.Join(dir, "bin", "motty")
		if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(exe, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		install := Install{Kind: KindLinuxPrefix, Exe: exe, Root: dir}
		return (&Updater{Dir: filepath.Join(dir, "updates")}).Launch(Staged{Install: install, Path: dir})
	}
	if err := launch("exit 3"); err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("a helper that exited at once was accepted: %v", err)
	}
	if err := launch("echo ready; sleep 1"); err != nil {
		t.Fatalf("a helper that waits was refused: %v", err)
	}
}

func TestTheMacHelperIsACopyOfTheWholeBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("bundles are copied with ditto")
	}
	root := fakeApp(t, t.TempDir(), "Motty.app", "program")
	if err := os.WriteFile(filepath.Join(root, "Contents", "Info.plist"), []byte("plist"), 0o644); err != nil {
		t.Fatal(err)
	}
	install := Install{Kind: KindMacApp, Root: root, Exe: filepath.Join(root, "Contents", "MacOS", "motty")}
	helper, err := stageHelper(install, filepath.Join(t.TempDir(), "helper"))
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(helper); string(data) != "program" || filepath.Base(helper) != "motty" {
		t.Fatalf("helper %s holds %q", helper, data)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(helper), "..", "Info.plist")); err != nil {
		t.Fatalf("the helper was taken out of its bundle: %v", err)
	}
}
