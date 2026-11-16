//go:build windows

package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func windowsPathName(t *testing.T, path string, name func(*uint16, *uint16, uint32) (uint32, error)) string {
	t.Helper()
	source, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	size, err := name(source, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, size)
	size, err = name(source, &buffer[0], uint32(len(buffer)))
	if err != nil {
		t.Fatal(err)
	}
	return windows.UTF16ToString(buffer[:size])
}

func TestSFTPDirectoryTreeResolvesWindowsShortPathAlias(t *testing.T) {
	base := t.TempDir()
	longName := strings.Repeat("long-directory-name-", 5)
	target := filepath.Join(base, longName)
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}

	canonical := filepath.Clean(windowsPathName(t, target, windows.GetLongPathName))
	alias := filepath.Clean(windowsPathName(t, target, windows.GetShortPathName))
	if sameDirectoryTreePath(true, canonical, alias) {
		t.Skip("8.3 short names are unavailable on this volume")
	}

	rootPath := directoryTreeRoot(true, canonical)
	parts, ok := directoryTreeRelativeParts(true, rootPath, canonical)
	if !ok || len(parts) == 0 {
		t.Fatalf("cannot split canonical path %q", canonical)
	}

	root := &sftpTreeNode{path: rootPath, name: directoryTreeRootLabel(rootPath), loaded: true, expanded: true}
	nodes := map[string]*sftpTreeNode{directoryTreePathKey(true, root.path): root}
	parent := root
	actual := rootPath
	for index, part := range parts {
		actual = filepath.Join(actual, part)
		child := &sftpTreeNode{
			path:     actual,
			name:     part,
			loaded:   true,
			expanded: index == len(parts)-1,
		}
		parent.children = []*sftpTreeNode{child}
		nodes[directoryTreePathKey(true, child.path)] = child
		parent = child
	}

	w := &sftpWindow{directoryTree: [2]sftpDirectoryTree{{
		enabled:  true,
		rootPath: rootPath,
		roots:    []*sftpTreeNode{root},
		nodes:    nodes,
	}}}
	w.syncDirectoryTreeToPath(true, alias)

	if !sameDirectoryTreePath(true, w.directoryTree[0].selectedPath, canonical) {
		t.Fatalf("selected path=%q, want canonical path %q", w.directoryTree[0].selectedPath, canonical)
	}
	for node := root; len(node.children) > 0; node = node.children[0] {
		if !node.expanded {
			t.Fatalf("ancestor %q was not revealed through the short-path alias", node.path)
		}
	}
}
