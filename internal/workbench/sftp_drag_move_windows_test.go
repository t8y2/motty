//go:build windows

package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
	"motty/internal/sftpclient"
)

func TestSFTPDragMoveResolvesWindowsShortPathAlias(t *testing.T) {
	base := t.TempDir()
	longName := strings.Repeat("drag-alias-dir-", 5)
	target := filepath.Join(base, longName)
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(target, "sub_target")
	if err := os.Mkdir(subDir, 0o700); err != nil {
		t.Fatal(err)
	}

	canonical := filepath.Clean(windowsPathName(t, target, windows.GetLongPathName))
	alias := filepath.Clean(windowsPathName(t, target, windows.GetShortPathName))
	if sameDirectoryTreePath(true, canonical, alias) {
		t.Skip("8.3 short names are unavailable on this volume")
	}

	// 1. sameSFTPPath must match short-path alias and canonical path for local files.
	if !sameSFTPPath(true, canonical, alias) {
		t.Fatalf("sameSFTPPath(true, %q, %q) = false, want true", canonical, alias)
	}
	if !sameSFTPPath(true, alias, canonical) {
		t.Fatalf("sameSFTPPath(true, %q, %q) = false, want true", alias, canonical)
	}

	// 2. isSFTPSubpath must recognize subdirectories even across short-name aliases.
	canonicalSub := filepath.Join(canonical, "sub_target")
	aliasSub := filepath.Join(alias, "sub_target")
	if !isSFTPSubpath(true, canonical, aliasSub) {
		t.Fatalf("isSFTPSubpath(true, %q, %q) = false, want true", canonical, aliasSub)
	}
	if !isSFTPSubpath(true, alias, canonicalSub) {
		t.Fatalf("isSFTPSubpath(true, %q, %q) = false, want true", alias, canonicalSub)
	}

	// 3. validFileMoveTarget must reject moving into the same folder or child across short names.
	childDir := filepath.Join(subDir, "nested")
	if err := os.Mkdir(childDir, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasChild := filepath.Join(aliasSub, "nested")
	entry := sftpclient.Entry{Name: "sub_target", Mode: os.ModeDir | 0o755}
	if validFileMoveTarget(true, canonical, []sftpclient.Entry{entry}, alias) {
		t.Fatal("validFileMoveTarget accepted move from canonical to alias of the same dir")
	}
	if validFileMoveTarget(true, alias, []sftpclient.Entry{entry}, canonical) {
		t.Fatal("validFileMoveTarget accepted move from alias to canonical of the same dir")
	}
	if validFileMoveTarget(true, canonical, []sftpclient.Entry{entry}, aliasSub) {
		t.Fatal("validFileMoveTarget accepted move into itself across short names")
	}
	if validFileMoveTarget(true, canonical, []sftpclient.Entry{entry}, aliasChild) {
		t.Fatal("validFileMoveTarget accepted move into its own child across short names")
	}

	// 4. finishLocalMove triggers listLocal when sourceDir or targetDir uses short-path alias.
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	view.local = true
	view.lfolder.path = canonical
	view.localBusy = true
	seqBefore := view.localSeq

	// Simulate move result reporting the 8.3 alias instead of canonical path.
	res := localMoveResult{
		sourceDir: alias,
		targetDir: aliasSub,
		total:     1,
		moved:     []string{filepath.Join(aliasSub, "test.txt")},
	}
	state.finishLocalMove(view, res)
	if view.localBusy {
		t.Fatal("expected view.localBusy to be reset")
	}
	if view.localSeq <= seqBefore {
		t.Fatalf("expected finishLocalMove to trigger listLocal (seqBefore=%d, current=%d)", seqBefore, view.localSeq)
	}
}
