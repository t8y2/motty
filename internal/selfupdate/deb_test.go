package selfupdate

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestDebInstallationDetection(t *testing.T) {
	env := Env{GOOS: "linux", Exists: func(string) bool { return true }, Writable: func(string) bool { return false }}
	for _, registered := range []bool{false, true} {
		env.LinuxDeb = func(string) bool { return registered }
		got, reason := Detect("/usr/bin/motty", env)
		if registered {
			if reason != ReasonNone || got.Kind != KindLinuxDeb || got.Root != "/usr" {
				t.Fatalf("registered DEB: %+v %v", got, reason)
			}
			if parseKind(got.Kind.String()) != KindLinuxDeb {
				t.Fatal("helper cannot parse DEB job")
			}
		} else if reason != ReasonSystemInstall {
			t.Fatalf("unregistered system install: %+v %v", got, reason)
		}
	}
	for _, exe := range []string{"/snap/motty/motty", "/opt/motty/motty", "/usr/local/bin/motty"} {
		if got, reason := Detect(exe, env); got.Kind == KindLinuxDeb || reason != ReasonSystemInstall {
			t.Fatalf("unrelated installation allowed: %+v %v", got, reason)
		}
	}
	if (&Updater{install: Install{Kind: KindLinuxDeb}}).PackageFormat() != "deb" || (&Updater{install: Install{Kind: KindLinuxPrefix}}).PackageFormat() != "" {
		t.Fatal("wrong update format")
	}
}

func TestDebMetadataMustMatchTheOffer(t *testing.T) {
	good := "Package: motty\nVersion: 1.2.3\nArchitecture: amd64\n"
	if err := checkDebMetadata(good, "1.2.3", "amd64"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(good, "motty", "other", 1), strings.Replace(good, "1.2.3", "1.2.2", 1), strings.Replace(good, "amd64", "arm64", 1), ""} {
		if err := checkDebMetadata(bad, "1.2.3", "amd64"); err == nil {
			t.Fatalf("invalid metadata accepted: %q", bad)
		}
	}
}

func TestDebAuthorizationUsesSystemToolsAndLiteralArguments(t *testing.T) {
	path := "/tmp/a path/$(touch ignored); package.deb"
	cmd := debInstallCommand(path)
	want := []string{"/usr/bin/pkexec", "/usr/bin/dpkg", "--install", path}
	if cmd.Path != want[0] || !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("authorization command: %q", cmd.Args)
	}
}

func TestDebInstallerExitFixture(t *testing.T) {
	if value := os.Getenv("MOTTY_TEST_DEB_EXIT_STATUS"); value != "" {
		code, _ := strconv.Atoi(value)
		os.Exit(code)
	}
}

func TestDebInstallationFailureAndAuthorizationCancellation(t *testing.T) {
	for _, code := range []int{0, 1, 126, 127} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDebInstallerExitFixture$")
		cmd.Env = append(os.Environ(), "MOTTY_TEST_DEB_EXIT_STATUS="+strconv.Itoa(code))
		err := runDebInstaller(cmd)
		if (err == nil) != (code == 0) {
			t.Fatalf("exit %d: %v", code, err)
		}
		if code == 126 || code == 127 {
			if !strings.Contains(err.Error(), "authorization") {
				t.Fatalf("authorization error lost: %v", err)
			}
		} else if code == 1 {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("package-manager error lost: %v", err)
			}
		}
	}
}
