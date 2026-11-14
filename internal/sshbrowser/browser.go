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
	"sync"

	"motty/internal/portforward"
)

// ErrBrowserNotFound means no supported browser executable was found.
var ErrBrowserNotFound = errors.New("Chrome, Edge or Chromium was not found")

// Browser owns a local browser process and its loopback proxy.
type Browser struct {
	process *os.Process
	forward *portforward.Forward
	stopped chan struct{}
	stop    sync.Once
}

// Start opens a new private browser window. The browser uses its own profile
// and proxy, so other browser windows are unaffected.
func Start(dial portforward.DialFunc) (*Browser, error) {
	path, err := findBrowser()
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

func findBrowser() (string, error) {
	var names, paths []string
	switch runtime.GOOS {
	case "windows":
		names = []string{"chrome.exe", "msedge.exe", "chromium.exe"}
		for _, root := range []string{"LOCALAPPDATA", "ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
			if dir := os.Getenv(root); filepath.IsAbs(dir) {
				paths = append(paths,
					filepath.Join(dir, "Google", "Chrome", "Application", "chrome.exe"),
					filepath.Join(dir, "Microsoft", "Edge", "Application", "msedge.exe"),
					filepath.Join(dir, "Chromium", "Application", "chrome.exe"),
				)
			}
		}
	case "darwin":
		for _, root := range []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")} {
			if !filepath.IsAbs(root) {
				continue
			}
			for _, name := range []string{"Google Chrome", "Microsoft Edge", "Chromium"} {
				paths = append(paths, filepath.Join(root, name+".app", "Contents", "MacOS", name))
			}
		}
	default:
		names = []string{"google-chrome", "google-chrome-stable", "microsoft-edge", "microsoft-edge-stable", "chromium", "chromium-browser"}
	}
	for _, path := range append(paths, names...) {
		if executable, err := exec.LookPath(path); err == nil {
			return executable, nil
		}
	}
	return "", ErrBrowserNotFound
}
