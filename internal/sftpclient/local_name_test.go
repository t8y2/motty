package sftpclient

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSafeLocalNameKeepsFilesApart(t *testing.T) {
	seen := map[string]string{}
	for _, remote := range []string{"/remote/a:b.txt", "/remote/a*b.txt", "/remote/a?.txt", "/remote/.env", "/remote/env", "/remote/a:b", "/remote/a_b", "/remote/a_b.txt"} {
		local := SafeLocalName(remote)
		if strings.ContainsAny(local, `<>:"/\|?*`) {
			t.Fatalf("%q -> %q still holds a rejected character", remote, local)
		}
		if previous, ok := seen[local]; ok {
			t.Fatalf("%q and %q share the local name %q", previous, remote, local)
		}
		seen[local] = remote
	}
	for remote, want := range map[string]string{
		"/etc/nginx/nginx.conf": "nginx.conf",
		"/tmp/ok name.sh":       "ok name.sh",
		"/remote/.env":          ".env",
	} {
		if got := SafeLocalName(remote); got != want {
			t.Fatalf("%q -> %q, want %q", remote, got, want)
		}
	}
	dangerous := SafeLocalName("/remote/a:b*c?.txt")
	if !strings.HasSuffix(dangerous, ".txt") || !strings.Contains(dangerous, "~") {
		t.Fatalf("dangerous name %q lost its extension or digest", dangerous)
	}
}

func TestSafeLocalNameAvoidsWindowsDeviceNames(t *testing.T) {
	for _, remote := range []string{"/remote/CON", "/remote/con.txt", "/remote/LPT1.log", "/remote/NUL"} {
		local := SafeLocalName(remote)
		if !strings.HasPrefix(local, "_") {
			t.Fatalf("%q -> %q keeps a reserved device name", remote, local)
		}
		stem, _, _ := strings.Cut(local, ".")
		if reservedLocalName(stem) {
			t.Fatalf("%q -> %q is still reserved", remote, local)
		}
	}
}

func TestSafeLocalNameTruncatesWholeRunesAndEndsCleanly(t *testing.T) {
	long := strings.Repeat("名", 200) + "."
	local := SafeLocalName("/remote/" + long)
	if len(local) > localNameLimit {
		t.Fatalf("local name of %d bytes exceeds the limit", len(local))
	}
	if !utf8.ValidString(local) {
		t.Fatalf("%q split a rune", local)
	}
	if strings.HasSuffix(local, ".") || strings.HasSuffix(local, " ") {
		t.Fatalf("%q ends with a character Windows trims", local)
	}
	withExtension := SafeLocalName("/remote/" + strings.Repeat("a", 200) + ".txt")
	if !strings.HasSuffix(withExtension, ".txt") || len(withExtension) > localNameLimit {
		t.Fatalf("long name %q lost its extension or limit", withExtension)
	}
}

func TestLocalRelativePathSanitizesWindowsComponentsOnly(t *testing.T) {
	const relative = "a:b/CON/file?.txt"
	windows := localRelativePath(relative, true)
	for _, part := range strings.Split(windows, string(filepath.Separator)) {
		if strings.ContainsAny(part, `<>:"\|?*`) || reservedLocalName(part) {
			t.Fatalf("windows component %q is not usable", part)
		}
	}
	other := localRelativePath(relative, false)
	if other != filepath.FromSlash(relative) {
		t.Fatalf("non-Windows path changed to %q", other)
	}
}

func TestJoinLocalWithinSanitizesWindowsChildren(t *testing.T) {
	joined, err := joinLocalWithinFor("/tmp/root", "a:b/CON/file?.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range strings.Split(joined, string(filepath.Separator)) {
		if strings.ContainsAny(part, `<>:"\|?*`) || reservedLocalName(part) {
			t.Fatalf("joined component %q is not usable", part)
		}
	}
	if _, err := joinLocalWithinFor("/tmp/root", "../escape", true); err == nil {
		t.Fatal("traversal accepted")
	}
}
