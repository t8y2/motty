// Package selfupdate replaces the running program with a newer release and starts
// it again. It downloads the package a release names, holds it to its checksum
// and, on the systems that sign their programs, to the signer of the running
// copy; then a helper that outlives the program swaps the files in place.
//
// User-owned installations are replaced directly. A dpkg-registered Linux DEB
// instead goes through Polkit and dpkg; other system installs and portable
// programs are left to the caller to send to the download page.
package selfupdate

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"motty/internal/update"
)

// Kind is the way this installation is replaced.
type Kind uint8

const (
	KindNone Kind = iota
	KindMacApp
	KindWindowsSetup
	KindLinuxPrefix
	KindLinuxDeb
)

func (kind Kind) String() string {
	switch kind {
	case KindMacApp:
		return "mac-app"
	case KindWindowsSetup:
		return "windows-setup"
	case KindLinuxPrefix:
		return "linux-prefix"
	case KindLinuxDeb:
		return "linux-deb"
	}
	return "none"
}

func parseKind(name string) Kind {
	for _, kind := range []Kind{KindMacApp, KindWindowsSetup, KindLinuxPrefix, KindLinuxDeb} {
		if kind.String() == name {
			return kind
		}
	}
	return KindNone
}

// Reason says why an installation cannot replace itself. ReasonNone means it can.
type Reason uint8

const (
	ReasonNone Reason = iota
	// ReasonNotUserInstall is an installation that was not made the way the
	// packages make it: a system package, a portable program, a development build.
	ReasonNotUserInstall
	// ReasonNotWritable is a standard installation the user cannot change.
	ReasonNotWritable
	// ReasonTranslocated is a macOS app running from the temporary place the
	// system copies a downloaded, never moved app to.
	ReasonTranslocated
	// ReasonSystemInstall is a system-owned installation not supported by the
	// authorized DEB path. Its package manager or installer must replace it.
	ReasonSystemInstall
	// ReasonAuthorizationUnavailable is a registered DEB without pkexec.
	ReasonAuthorizationUnavailable
)

// Install is where the running program lives and how it is replaced.
type Install struct {
	Kind Kind
	// Exe is the running program, links resolved.
	Exe string
	// Root is what gets replaced: the .app bundle, the Windows install directory
	// or the Linux prefix.
	Root string
}

// Env is what Detect asks of the system, so a test can describe any machine.
type Env struct {
	GOOS         string
	LocalAppData string   // %LOCALAPPDATA% on Windows
	ProgramFiles []string // %ProgramFiles% and its variants
	// WindowsUserDir and WindowsMachineDir come from the NSIS uninstall entry
	// for this application, in the same 64-bit registry view as the installer.
	WindowsUserDir    string
	WindowsMachineDir string
	Writable          func(dir string) bool
	Exists            func(path string) bool
	DebInstalled      func(path string) bool
	PolkitAvailable   func() bool
}

// SystemEnv describes the machine this runs on.
func SystemEnv() Env {
	var programFiles []string
	for _, name := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432"} {
		if dir := os.Getenv(name); dir != "" {
			programFiles = append(programFiles, dir)
		}
	}
	userDir, machineDir := windowsInstallDirs()
	return Env{
		GOOS: runtime.GOOS, LocalAppData: os.Getenv("LOCALAPPDATA"), ProgramFiles: programFiles,
		WindowsUserDir: userDir, WindowsMachineDir: machineDir,
		Writable: writable, Exists: exists, DebInstalled: debInstalled, PolkitAvailable: polkitAvailable,
	}
}

// under reports whether path is inside dir.
func under(path, dir string) bool {
	relative, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// writable reports whether files can be made in dir.
func writable(dir string) bool {
	file, err := os.CreateTemp(dir, ".motty-write-*")
	if err != nil {
		return false
	}
	name := file.Name()
	file.Close()
	return os.Remove(name) == nil
}

// linuxPrefix is what install.sh accepts in a prefix.
var linuxPrefix = regexp.MustCompile(`^[A-Za-z0-9_./+-]+$`)

// Detect says how the program at exe (links resolved) is replaced, or why it
// cannot be.
func Detect(exe string, env Env) (Install, Reason) {
	install := Install{Exe: exe}
	switch env.GOOS {
	case "darwin":
		// <Name>.app/Contents/MacOS/<exe>
		macOS := filepath.Dir(exe)
		contents := filepath.Dir(macOS)
		bundle := filepath.Dir(contents)
		if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || !strings.HasSuffix(bundle, ".app") {
			return install, ReasonNotUserInstall
		}
		if strings.Contains(exe, "/AppTranslocation/") {
			return install, ReasonTranslocated
		}
		if !env.Writable(filepath.Dir(bundle)) {
			return install, ReasonNotWritable
		}
		install.Kind, install.Root = KindMacApp, bundle
	case "windows":
		dir := filepath.Dir(exe)
		registered := env.WindowsUserDir != "" && strings.EqualFold(filepath.Clean(dir), filepath.Clean(env.WindowsUserDir))
		if env.WindowsMachineDir != "" && strings.EqualFold(filepath.Clean(dir), filepath.Clean(env.WindowsMachineDir)) {
			return install, ReasonSystemInstall
		}
		for _, root := range env.ProgramFiles {
			if !registered && root != "" && under(exe, root) {
				return install, ReasonSystemInstall
			}
		}
		// A custom location must match the per-user installer's record, not
		// merely contain a copied Uninstall.exe. Keep the old default-location
		// detection only when no user installation is registered elsewhere.
		programs := filepath.Join(env.LocalAppData, "Programs")
		legacyDefault := env.WindowsUserDir == "" && env.LocalAppData != "" && strings.EqualFold(filepath.Dir(dir), programs)
		if (!registered && !legacyDefault) || !env.Exists(filepath.Join(dir, "Uninstall.exe")) {
			return install, ReasonNotUserInstall
		}
		if !env.Writable(dir) {
			return install, ReasonNotWritable
		}
		install.Kind, install.Root = KindWindowsSetup, dir
	case "linux":
		if exe == "/usr/bin/motty" && env.DebInstalled != nil && env.DebInstalled(exe) {
			install.Kind, install.Root = KindLinuxDeb, filepath.Dir(exe)
			if env.PolkitAvailable == nil || !env.PolkitAvailable() {
				return install, ReasonAuthorizationUnavailable
			}
			return install, ReasonNone
		}
		for _, root := range []string{"/usr", "/opt", "/snap", "/var/lib/flatpak"} {
			if under(exe, root) {
				return install, ReasonSystemInstall
			}
		}
		// <prefix>/bin/<exe>, with the menu entry install.sh makes under the prefix.
		bin := filepath.Dir(exe)
		prefix := filepath.Dir(bin)
		desktop := filepath.Join(prefix, "share", "applications", filepath.Base(exe)+".desktop")
		if filepath.Base(bin) != "bin" || !linuxPrefix.MatchString(prefix) || !env.Exists(desktop) {
			return install, ReasonNotUserInstall
		}
		if !env.Writable(bin) {
			return install, ReasonNotWritable
		}
		install.Kind, install.Root = KindLinuxPrefix, prefix
	default:
		return install, ReasonNotUserInstall
	}
	return install, ReasonNone
}

// Package describes which release asset belongs to this installation.
func (install Install) Package() update.Package {
	if install.Kind == KindLinuxDeb {
		return update.PackageDeb
	}
	return update.PackageArchive
}
