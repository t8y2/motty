package selfupdate

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	vivaupdate "github.com/vivaui/viva/update"
	"motty/internal/update"
	"motty/internal/updatesign"
)

// vivaAsset is the Viva package type for a feed-verified asset.
func vivaAsset(asset update.Asset) vivaupdate.Asset {
	return vivaupdate.Asset{OS: runtime.GOOS, Arch: runtime.GOARCH, Format: asset.Format, URL: asset.URL, Size: asset.Size, SHA256: asset.SHA256}
}

// Phase is what Prepare is doing, for the progress it shows.
type Phase uint8

const (
	PhaseDownloading Phase = iota
	PhaseVerifying
)

// ErrUnsupported means this installation cannot replace itself, or the release
// has no package for it.
var ErrUnsupported = errors.New("selfupdate: this installation cannot be updated in place")

// releaseKey is what a package signature is checked against. A test replaces it
// with a key it can sign with.
var releaseKey = updatesign.ReleaseKey

// Staged is a downloaded and checked version, ready to replace the running one.
type Staged struct {
	Install   Install
	Version   string
	Signature string
	// Path is the new .app, the installer, or the directory of the unpacked
	// archive, depending on the installation's Kind.
	Path string
	// Payload marks Path as a directory a patch applied: the application's
	// files, ready for the platform installer, not an archive to unpack.
	Payload bool
	// Asset and App are what Install needs when Payload is set.
	Asset update.Asset
	App   string
}

// Updater replaces the program it runs in.
type Updater struct {
	// Dir is where downloads and the helper are kept between runs.
	Dir    string
	Client *http.Client

	install Install
	reason  Reason
}

// New describes the running program and keeps updates in dir.
func New(dir string) *Updater {
	updater := &Updater{Dir: dir}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		updater.reason = ReasonNotUserInstall
		return updater
	}
	updater.install, updater.reason = Detect(exe, SystemEnv())
	return updater
}

// Support says whether this installation can be updated in place, and if not why.
func (updater *Updater) Support() Reason { return updater.reason }

// Package selects the release asset for the detected installation.
func (updater *Updater) Package() update.Package { return updater.install.Package() }

// TakeFailure is why the last update failed, if it did.
func (updater *Updater) TakeFailure() string { return TakeFailure(updater.Dir) }

// clean removes what an earlier update left, except the record of its failure.
func (updater *Updater) clean() {
	entries, err := os.ReadDir(updater.Dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Name() != failureFile {
			os.RemoveAll(filepath.Join(updater.Dir, entry.Name()))
		}
	}
}

// Prepare downloads the release's package and checks it against the running
// program: its checksum, and where there is a signature, who signed it. Nothing
// is changed on the system; a failure or a cancelled context leaves it as it was.
func (updater *Updater) Prepare(ctx context.Context, release update.Release, progress func(Progress), phase func(Phase)) (Staged, error) {
	if updater.reason != ReasonNone || !release.Asset.Has() {
		return Staged{}, ErrUnsupported
	}
	install := updater.install
	// Who signed the running program decides who may sign its replacement, and
	// a program with no such signature cannot be updated safely: ask before
	// downloading anything.
	var want Identity
	var err error
	switch install.Kind {
	case KindMacApp:
		want, err = macIdentity(ctx, install.Root)
	case KindWindowsSetup, KindWindowsPortable:
		// A Windows installer may be unsigned: the release signature is what
		// decides whether it can be installed. If this copy is signed, the new
		// installer still has to be signed by the same subject.
		want, err = windowsIdentity(ctx, install.Exe)
		if errors.Is(err, ErrUnsigned) {
			want, err = Identity{}, nil
		}
	}
	if err != nil {
		return Staged{}, err
	}
	// A signature that is missing, malformed or from another key cannot pass
	// the check after the download, so stop before spending the bandwidth. A
	// package named by the signed Viva feed has no per-package signature: the
	// feed's Ed25519 signature and the digest it carries are the check.
	if !release.Asset.FeedVerified {
		if err := updatesign.UsableWith(releaseKey(), release.Asset.Signature); err != nil {
			return Staged{}, err
		}
	}
	// A delta is tried first when the signed feed lists one for the running
	// version. A patch that does not fit, or cannot be applied, is not a failed
	// update: the full archive is downloaded instead.
	if release.Asset.FeedVerified {
		if patch, ok := release.Asset.PatchFrom(update.Current); ok {
			if staged, err := updater.preparePatch(ctx, release, patch, progress, phase); err == nil {
				return staged, nil
			}
		}
	}

	if install.Kind != KindLinuxDeb {
		updater.clean()
	}
	dir := filepath.Join(updater.Dir, release.Version)
	if phase != nil {
		phase(PhaseDownloading)
	}
	archive := filepath.Join(dir, release.Asset.Name)
	if install.Kind != KindLinuxDeb || ctx.Err() != nil || verifyCachedPackage(archive, release) != nil {
		archive, err = Download(ctx, updater.Client, release.Asset, dir, progress)
	}
	if err != nil {
		return Staged{}, err
	}
	if phase != nil {
		phase(PhaseVerifying)
	}
	if err := verifyReleasePackage(archive, release); err != nil {
		return Staged{}, err
	}
	staged := Staged{Install: install, Version: release.Version, Signature: release.Asset.Signature}
	switch install.Kind {
	case KindMacApp:
		unpacked := filepath.Join(dir, "unpacked")
		if err := ExtractZip(archive, unpacked); err != nil {
			return Staged{}, err
		}
		app := filepath.Join(unpacked, filepath.Base(install.Root))
		if info, err := os.Stat(app); err != nil || !info.IsDir() {
			return Staged{}, fmt.Errorf("selfupdate: the package does not hold %s", filepath.Base(install.Root))
		}
		if err := verifyMac(ctx, app, want); err != nil {
			return Staged{}, err
		}
		staged.Path = app
	case KindWindowsSetup:
		if want.Subject != "" {
			if err := verifyWindows(ctx, archive, want); err != nil {
				return Staged{}, err
			}
		}
		staged.Path = archive
	case KindWindowsPortable:
		unpacked := filepath.Join(dir, "unpacked")
		if err := extractPortable(archive, unpacked); err != nil {
			return Staged{}, err
		}
		candidate := filepath.Join(unpacked, "motty.exe")
		if err := verifyPortableArchitecture(candidate); err != nil {
			return Staged{}, err
		}
		if want.Subject != "" {
			if err := verifyWindows(ctx, candidate, want); err != nil {
				return Staged{}, err
			}
		}
		staged.Path = candidate
	case KindLinuxPrefix:
		unpacked := filepath.Join(dir, "unpacked")
		if err := ExtractTarGz(archive, unpacked); err != nil {
			return Staged{}, err
		}
		script, err := findInstallScript(unpacked)
		if err != nil {
			return Staged{}, err
		}
		if _, err := os.Stat(filepath.Join(script, filepath.Base(install.Exe))); err != nil {
			return Staged{}, fmt.Errorf("selfupdate: the package does not hold %s", filepath.Base(install.Exe))
		}
		staged.Path = script
	case KindLinuxDeb:
		staged.Path = archive
	}
	return staged, nil
}

// preparePatch downloads a delta and applies it to the running installation in
// a staging directory. Nothing installed is changed; the platform installer
// swaps the payload in when the application quits.
func (updater *Updater) preparePatch(ctx context.Context, release update.Release, patch update.Patch, progress func(Progress), phase func(Phase)) (Staged, error) {
	dir := filepath.Join(updater.Dir, release.Version+".patch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Staged{}, err
	}
	if phase != nil {
		phase(PhaseDownloading)
	}
	if progress != nil {
		progress(Progress{Done: 0, Size: patch.Size})
	}
	path, err := update.DownloadPatch(ctx, updater.Client, release.Asset, patch, dir, func(done, total int64) {
		if progress != nil {
			progress(Progress{Done: done, Size: total})
		}
	})
	if err != nil {
		os.RemoveAll(dir)
		return Staged{}, err
	}
	if phase != nil {
		phase(PhaseVerifying)
	}
	payload, err := update.ApplyPatch(path, patch.FromVersion, release.Version)
	if err != nil {
		os.RemoveAll(dir)
		return Staged{}, err
	}
	return Staged{
		Install: updater.install, Version: release.Version, Path: payload,
		Payload: true, Asset: release.Asset, App: update.AppID,
	}, nil
}

// verifyReleasePackage holds the downloaded package to the signature the feed
// carries: the bytes, the version and the file name all have to be the ones the
// release key signed. The package is streamed, not read into memory.
func verifyReleasePackage(path string, release update.Release) error {
	if release.Asset.FeedVerified {
		return nil
	}
	return updatesign.VerifyFile(releaseKey(), path, release.Asset.Signature, release.Version, release.Asset.Name)
}

// findInstallScript is the directory of the unpacked archive that holds
// install.sh: the archive's one top-level directory.
func findInstallScript(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(filepath.Join(root, entry.Name(), "install.sh")); err == nil {
				return filepath.Join(root, entry.Name()), nil
			}
		}
	}
	return "", errors.New("selfupdate: the package holds no install.sh")
}

// Ready is the line the helper prints once it has read its job and is waiting
// for the program to quit.
const Ready = "ready"

// helperStartTimeout is how long Launch waits for the helper to say Ready. A
// helper that starts does so at once; this only ends a wait on one that hangs.
const helperStartTimeout = 10 * time.Second

// Launch starts the helper that will replace the program once this process has
// quit and start the new version. The caller must quit right after it returns
// nil; the helper waits for that. The helper runs from a copy of the program, so
// it holds no file of the installation that is being replaced. Launch returns
// only once the helper has started: one the system refuses to run, or that
// cannot read its job, is an error and the caller keeps running.
func (updater *Updater) Launch(staged Staged, quit func()) error {
	if staged.Payload {
		err := vivaupdate.Install(vivaupdate.InstallOptions{
			Payload: staged.Path, Asset: vivaAsset(staged.Asset),
			Version: staged.Version, App: staged.App,
		}, quit)
		if err != nil {
			os.RemoveAll(staged.Path)
			return fmt.Errorf("selfupdate: %w", err)
		}
		return nil
	}
	exe := staged.Install.Exe
	helper, err := stageHelper(staged.Install, filepath.Join(updater.Dir, "helper"))
	if err != nil {
		return fmt.Errorf("selfupdate: preparing the installer: %w", err)
	}
	job := Job{
		Kind: staged.Install.Kind, PID: os.Getpid(), Staged: staged.Path,
		Version: staged.Version, Signature: staged.Signature,
		Root: staged.Install.Root, Exe: exe, Dir: updater.Dir,
	}
	cmd := exec.Command(helper, job.Arguments()...)
	detach(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	started := make(chan error, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		if strings.TrimSpace(line) == Ready {
			started <- nil
			return
		}
		err := cmd.Wait()
		if err == nil {
			err = errors.New("it exited")
		}
		started <- fmt.Errorf("selfupdate: the installer stopped as it started (%w); the system may have refused to run it", err)
	}()
	select {
	case err := <-started:
		return err
	case <-time.After(helperStartTimeout):
		cmd.Process.Kill()
		return errors.New("selfupdate: the installer did not start")
	}
}
