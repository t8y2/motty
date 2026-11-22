// Package sshbrowser launches an isolated local browser through an SSH
// connection's SOCKS5 forward.
package sshbrowser

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"motty/internal/portforward"
)

// ErrBrowserNotFound means no supported browser executable was found.
var ErrBrowserNotFound = errors.New("Chrome, Edge or Chromium was not found")

// Family names the browser the tunnel browser launches. The values are written
// to the state file, so they never change.
type Family string

const (
	FamilyAuto     Family = ""
	FamilyChrome   Family = "chrome"
	FamilyEdge     Family = "edge"
	FamilyChromium Family = "chromium"
)

// Families lists every family the setting offers, in the order it lists them.
var Families = [...]Family{FamilyAuto, FamilyChrome, FamilyEdge, FamilyChromium}

// ParseFamily makes a saved value a family this build knows. A value from
// another version falls back to the automatic choice rather than leaving the
// tab unable to open any browser.
func ParseFamily(value string) Family {
	switch family := Family(strings.TrimSpace(value)); family {
	case FamilyChrome, FamilyEdge, FamilyChromium:
		return family
	}
	return FamilyAuto
}

// known reports whether this build can look for the family.
func (family Family) known() bool {
	return family == FamilyChrome || family == FamilyEdge || family == FamilyChromium
}

// Browser owns a local browser process and its loopback proxy.
type Browser struct {
	process *os.Process
	forward *portforward.Forward
	stopped chan struct{}
	stop    sync.Once
}

// Start opens a new private browser window of the given family. The browser
// uses its own profile and proxy, so other browser windows are unaffected.
// FamilyAuto takes the first browser installed in the usual order.
func Start(dial portforward.DialFunc, family Family) (*Browser, error) {
	path, err := findBrowser(family)
	if err != nil {
		return nil, err
	}
	profile, err := os.MkdirTemp("", "motty-ssh-browser-")
	if err != nil {
		return nil, fmt.Errorf("create browser profile: %w", err)
	}
	forward, err := portforward.Start(portforward.Config{
		ListenAddress: "127.0.0.1:0",
		Mode:          portforward.ModeSOCKS5,
		Dial:          dial,
		MaxStreams:    256,
	})
	if err != nil {
		_ = os.RemoveAll(profile)
		return nil, err
	}
	cmd := exec.Command(path, browserArgs(profile, forward.Status().ListenAddress)...)
	if err := cmd.Start(); err != nil {
		forward.CloseAsync()
		_ = os.RemoveAll(profile)
		return nil, fmt.Errorf("launch browser: %w", err)
	}
	browser := &Browser{process: cmd.Process, forward: forward, stopped: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		browser.Close()
		_ = os.RemoveAll(profile)
	}()
	return browser, nil
}

func browserArgs(profile, proxy string) []string {
	return []string{
		"--user-data-dir=" + profile,
		"--proxy-server=socks5://" + proxy,
		// OAuth redirect listeners on localhost are on the SSH host.
		"--proxy-bypass-list=<-loopback>",
		// SOCKS resolves destinations on the SSH host. Prevent browser-side
		// DNS prefetch and direct QUIC or WebRTC UDP traffic.
		"--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1",
		"--disable-quic",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--disable-background-networking",
		"--disable-extensions",
		"--no-first-run",
		"--no-default-browser-check",
		"--incognito",
		"--new-window",
		"about:blank",
	}
}

// Running reports whether the browser session is still active.
func (browser *Browser) Running() bool {
	select {
	case <-browser.stopped:
		return false
	default:
		return true
	}
}

// Close stops the browser and releases its proxy. Active SSH streams and the
// temporary profile are cleaned up in the background.
func (browser *Browser) Close() {
	browser.stop.Do(func() {
		_ = browser.process.Kill()
		browser.forward.CloseAsync()
		close(browser.stopped)
	})
}

// browserCandidate is one place a family may be installed: an absolute
// location, or a name to look for on PATH.
type browserCandidate struct {
	family Family
	path   string
	name   string
}

func findBrowser(family Family) (string, error) {
	return findBrowserWith(family, exec.LookPath)
}

// findBrowserWith takes the lookPath seam the tests replace, so they can prove
// a chosen family never falls back to another browser.
func findBrowserWith(family Family, lookPath func(string) (string, error)) (string, error) {
	wanted := func(candidate browserCandidate) bool {
		return !family.known() || candidate.family == family
	}
	// Install locations are tried before PATH names, and the automatic choice
	// keeps the order it has always had: Chrome, then Edge, then Chromium.
	for _, candidate := range installCandidates() {
		if !wanted(candidate) {
			continue
		}
		if executable, err := lookPath(candidate.path); err == nil {
			return executable, nil
		}
	}
	for _, candidate := range pathCandidates() {
		if !wanted(candidate) {
			continue
		}
		if executable, err := lookPath(candidate.name); err == nil {
			return executable, nil
		}
	}
	return "", ErrBrowserNotFound
}

func installCandidates() []browserCandidate {
	switch runtime.GOOS {
	case "windows":
		var candidates []browserCandidate
		for _, root := range []string{"LOCALAPPDATA", "ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
			dir := os.Getenv(root)
			if !filepath.IsAbs(dir) {
				continue
			}
			candidates = append(candidates,
				browserCandidate{family: FamilyChrome, path: filepath.Join(dir, "Google", "Chrome", "Application", "chrome.exe")},
				browserCandidate{family: FamilyEdge, path: filepath.Join(dir, "Microsoft", "Edge", "Application", "msedge.exe")},
				browserCandidate{family: FamilyChromium, path: filepath.Join(dir, "Chromium", "Application", "chrome.exe")},
			)
		}
		return candidates
	case "darwin":
		var candidates []browserCandidate
		for _, root := range []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")} {
			if !filepath.IsAbs(root) {
				continue
			}
			for _, browser := range []struct {
				family Family
				name   string
			}{
				{family: FamilyChrome, name: "Google Chrome"},
				{family: FamilyEdge, name: "Microsoft Edge"},
				{family: FamilyChromium, name: "Chromium"},
			} {
				candidates = append(candidates, browserCandidate{
					family: browser.family,
					path:   filepath.Join(root, browser.name+".app", "Contents", "MacOS", browser.name),
				})
			}
		}
		return candidates
	default:
		return nil
	}
}

func pathCandidates() []browserCandidate {
	switch runtime.GOOS {
	case "windows":
		return []browserCandidate{
			{family: FamilyChrome, name: "chrome.exe"},
			{family: FamilyEdge, name: "msedge.exe"},
			{family: FamilyChromium, name: "chromium.exe"},
		}
	case "darwin":
		return nil
	default:
		return []browserCandidate{
			{family: FamilyChrome, name: "google-chrome"},
			{family: FamilyChrome, name: "google-chrome-stable"},
			{family: FamilyEdge, name: "microsoft-edge"},
			{family: FamilyEdge, name: "microsoft-edge-stable"},
			{family: FamilyChromium, name: "chromium"},
			{family: FamilyChromium, name: "chromium-browser"},
		}
	}
}
