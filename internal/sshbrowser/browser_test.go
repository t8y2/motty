package sshbrowser

import (
	"slices"
	"strings"
	"testing"
)

func TestBrowserArgs(t *testing.T) {
	profile := "/tmp/test-profile"
	proxy := "127.0.0.1:1080"
	targetURL := "http://127.0.0.1:8080/dashboard"

	args := browserArgs(profile, proxy, targetURL)

	if !slices.Contains(args, "--user-data-dir="+profile) {
		t.Errorf("missing user-data-dir in args: %v", args)
	}
	if !slices.Contains(args, "--proxy-server=socks5://"+proxy) {
		t.Errorf("missing proxy-server in args: %v", args)
	}
	if !slices.Contains(args, "--proxy-bypass-list=<-loopback>") {
		t.Errorf("missing proxy-bypass-list in args: %v", args)
	}
	if !slices.Contains(args, targetURL) {
		t.Errorf("missing targetURL in args: %v", args)
	}

	blankArgs := browserArgs(profile, proxy, "")
	if !slices.Contains(blankArgs, "about:blank") {
		t.Errorf("expected about:blank for empty URL, got: %v", blankArgs)
	}
}

func TestFindBrowserDoesNotPanic(t *testing.T) {
	path, err := findBrowser()
	if err == nil {
		if path == "" {
			t.Fatal("found browser returned empty path with nil error")
		}
		t.Logf("found browser at %s", path)
	} else if err != ErrBrowserNotFound && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
