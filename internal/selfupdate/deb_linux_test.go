//go:build linux

package selfupdate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"aead.dev/minisign"
	"motty/internal/update"
	"motty/internal/updatesign"
)

func TestDebAuthorizationUsesTheRegisteredProgram(t *testing.T) {
	job := Job{Kind: KindLinuxDeb, PID: 42, Exe: "/usr/bin/motty", Root: "/usr/bin", Dir: "/home/user/updates", Staged: "/home/user/package.deb", Version: "1.2.3", Signature: "signed"}
	if want := []string{"/usr/bin/pkexec", "/usr/bin/motty", "install-deb", "--path=/home/user/package.deb", "--version=1.2.3", "--signature=signed"}; !reflect.DeepEqual(debInstallCommand(job).Args, want) {
		t.Fatalf("authorization command: %q", debInstallCommand(job).Args)
	}
	parsed, err := ParseJob(job.Arguments()[1:])
	if err != nil || !reflect.DeepEqual(parsed, job) {
		t.Fatalf("the DEB job did not survive its command line: %+v, %v", parsed, err)
	}
}

func TestDebFailureReportsThePackageManagersReason(t *testing.T) {
	output := []byte("Preparing package...\ndpkg: error: dpkg frontend lock is held by another process\n")
	if got := debFailure(output); !strings.Contains(got, "lock") {
		t.Fatalf("the lock failure was hidden: %s", got)
	}
}

func TestDebAuthorizationCancellationIsNotSuccess(t *testing.T) {
	for _, status := range []string{"126", "127", "1"} {
		output, err := exec.Command("/bin/sh", "-c", "exit "+status).CombinedOutput()
		failure := debInstallError(err, output)
		if failure == nil {
			t.Fatalf("exit %s reported success", status)
		}
		if status != "1" && !strings.Contains(failure.Error(), "authorization") {
			t.Fatalf("exit %s did not explain cancellation: %v", status, failure)
		}
	}
}

func TestPrivilegedCopyVerifiesBeforeDpkg(t *testing.T) {
	publicKey, privateKey, err := minisign.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	previous := releaseKey
	releaseKey = func() minisign.PublicKey { return publicKey }
	t.Cleanup(func() { releaseKey = previous })

	content := []byte("signed test package")
	name := "motty_1.2.3_amd64.deb"
	signature, err := updatesign.Sign(privateKey, bytes.NewReader(content), "1.2.3", name, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	release := update.Release{Version: "1.2.3", Asset: update.Asset{Name: name, Signature: signature}}
	dir := t.TempDir()
	source, trusted := filepath.Join(dir, name), filepath.Join(dir, "trusted.deb")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyVerifiedDeb(source, trusted, release); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("swapped"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyReleasePackage(trusted, release); err != nil {
		t.Fatalf("swapping the user copy changed the privileged package: %v", err)
	}
	if err := copyVerifiedDeb(source, filepath.Join(dir, "rejected.deb"), release); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("tampered DEB was accepted: %v", err)
	}
	if err := os.Symlink(trusted, filepath.Join(dir, "linked.deb")); err != nil {
		t.Fatal(err)
	}
	if err := copyVerifiedDeb(filepath.Join(dir, "linked.deb"), filepath.Join(dir, "linked-target.deb"), release); err == nil {
		t.Fatal("a symlinked source was accepted")
	}
}
