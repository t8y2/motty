package sftpclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// treeReader is a server's folders: each path lists its entries, and a path in
// fail cannot be read.
type treeReader struct {
	mu      sync.Mutex
	folders map[string][]os.FileInfo
	fail    map[string]bool
	read    []string
	// onRead runs before each read, outside the lock.
	onRead func(dir string)
}

func (reader *treeReader) ReadDir(dir string) ([]os.FileInfo, error) {
	if reader.onRead != nil {
		reader.onRead(dir)
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.read = append(reader.read, dir)
	if reader.fail[dir] {
		return nil, errors.New("permission denied")
	}
	return slices.Clone(reader.folders[dir]), nil
}

func joinSlash(dir, name string) string { return path.Join(dir, name) }
func joinLocal(dir, name string) string { return filepath.Join(dir, name) }

func dir(name string) os.FileInfo  { return fakeInfo{name: name, mode: os.ModeDir | 0o755} }
func file(name string) os.FileInfo { return fakeInfo{name: name, mode: 0o644, size: 1} }

func sampleTree() *treeReader {
	return &treeReader{folders: map[string][]os.FileInfo{
		"/srv":                {file("readme.md"), dir("app"), dir(".git"), fakeInfo{name: "loop", mode: os.ModeSymlink | 0o777}, dir("locked")},
		"/srv/app":            {dir("src"), file("App.go")},
		"/srv/app/src":        {file("main.go"), file("main_test.go")},
		"/srv/.git":           {file("HEAD")},
		"/srv/loop":           {dir("never")},
		"/srv/locked":         {file("secret")},
		"/srv/app/src/deeper": {file("unseen")},
	}, fail: map[string]bool{"/srv/locked": true}}
}

func walkAll(t *testing.T, reader DirReader, root string, options WalkOptions) ([]string, WalkSummary) {
	t.Helper()
	if options.Join == nil {
		options.Join = joinSlash
	}
	var got []string
	summary := Walk(context.Background(), reader, root, options, func(found []Found) {
		for _, item := range found {
			got = append(got, path.Join(item.Dir, item.Entry.Name))
		}
	})
	return got, summary
}

func TestWalkReadsTheTreeNearestFirstWithoutFollowingLinks(t *testing.T) {
	reader := sampleTree()
	got, summary := walkAll(t, reader, "/srv", WalkOptions{MaxEntries: 100, MaxDepth: 10, Parallel: 3})
	want := []string{
		".git", "app", "locked", "loop", "readme.md",
		".git/HEAD", "app/App.go", "app/src",
		"app/src/main.go", "app/src/main_test.go",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("walk reported\n%q\nwant\n%q", got, want)
	}
	if summary.Entries != len(want) || summary.Folders != 4 || summary.Failed != 1 || summary.Truncated || summary.Canceled {
		t.Fatalf("summary = %+v", summary)
	}
	if slices.Contains(reader.read, "/srv/loop") {
		t.Fatal("the walk followed a symbolic link")
	}
}

func TestWalkSkipsWhatItIsToldToAndDoesNotEnterIt(t *testing.T) {
	reader := sampleTree()
	hidden := func(info os.FileInfo) bool { return strings.HasPrefix(info.Name(), ".") }
	got, _ := walkAll(t, reader, "/srv", WalkOptions{MaxEntries: 100, MaxDepth: 10, Skip: hidden})
	if slices.Contains(got, ".git") || slices.Contains(got, ".git/HEAD") || slices.Contains(reader.read, "/srv/.git") {
		t.Fatalf("a skipped folder was reported or read: %q, read %q", got, reader.read)
	}
}

func TestWalkStopsAtItsBoundsAndSaysSo(t *testing.T) {
	got, summary := walkAll(t, sampleTree(), "/srv", WalkOptions{MaxEntries: 6, MaxDepth: 10})
	if len(got) != 6 || summary.Entries != 6 || !summary.Truncated {
		t.Fatalf("entry bound: got %q, summary %+v", got, summary)
	}
	got, summary = walkAll(t, sampleTree(), "/srv", WalkOptions{MaxEntries: 100, MaxDepth: 1})
	if slices.Contains(got, "app/src/main.go") || !slices.Contains(got, "app/src") || !summary.Truncated {
		t.Fatalf("depth bound: got %q, summary %+v", got, summary)
	}
	if _, summary := walkAll(t, sampleTree(), "/srv", WalkOptions{MaxEntries: 100, MaxDepth: 2}); summary.Truncated {
		t.Fatalf("a tree within its depth was called truncated: %+v", summary)
	}
}

func TestWalkSaysWhyTheRootCouldNotBeRead(t *testing.T) {
	reader := sampleTree()
	reader.fail["/srv"] = true
	got, summary := walkAll(t, reader, "/srv", WalkOptions{MaxEntries: 100, MaxDepth: 5})
	if len(got) != 0 || summary.RootErr == nil || summary.Failed != 1 || summary.Folders != 0 {
		t.Fatalf("got %q, summary %+v", got, summary)
	}
	// A folder below the root that cannot be read is counted, not a failure of the search.
	if _, summary := walkAll(t, sampleTree(), "/srv", WalkOptions{MaxEntries: 100, MaxDepth: 5}); summary.RootErr != nil || summary.Failed != 1 {
		t.Fatalf("summary %+v", summary)
	}
}

func TestWalkEntersOnlyPlainFolders(t *testing.T) {
	reader := &treeReader{folders: map[string][]os.FileInfo{
		"/srv":          {dir("real"), fakeInfo{name: "junction", mode: os.ModeIrregular}, fakeInfo{name: "tty", mode: os.ModeDevice | os.ModeCharDevice}, fakeInfo{name: "pipe", mode: os.ModeNamedPipe}},
		"/srv/real":     {file("a")},
		"/srv/junction": {file("b")},
	}}
	got, summary := walkAll(t, reader, "/srv", WalkOptions{MaxEntries: 100, MaxDepth: 5})
	if !slices.Contains(got, "junction") || !slices.Contains(got, "tty") || slices.Contains(got, "junction/b") || !slices.Equal(reader.read, []string{"/srv", "/srv/real"}) || summary.Truncated {
		t.Fatalf("got %q, read %q, summary %+v", got, reader.read, summary)
	}
}

func TestWalkPrunesFoldersItIsToldNotToEnter(t *testing.T) {
	reader := &treeReader{folders: map[string][]os.FileInfo{
		"/":         {dir("proc"), dir("home")},
		"/proc":     {dir("1")},
		"/home":     {file("notes")},
		"/home/sys": {},
	}}
	got, _ := walkAll(t, reader, "/", WalkOptions{MaxEntries: 100, MaxDepth: 5, Prune: func(dir string) bool { return dir == "/proc" }})
	if !slices.Contains(got, "proc") || slices.Contains(got, "proc/1") || !slices.Contains(got, "home/notes") || slices.Contains(reader.read, "/proc") {
		t.Fatalf("got %q, read %q", got, reader.read)
	}
	// The folder the walk starts in is read even when the prune would name it.
	reader = &treeReader{folders: map[string][]os.FileInfo{"/proc": {file("cpuinfo")}}}
	if got, _ := walkAll(t, reader, "/proc", WalkOptions{MaxEntries: 100, MaxDepth: 5, Prune: func(string) bool { return true }}); !slices.Equal(got, []string{"cpuinfo"}) {
		t.Fatalf("the root was pruned: %q", got)
	}
}

func TestCompareFoldOrdersLikeLowerCasedNames(t *testing.T) {
	names := []string{"", "a", "A", "ab", "Ab", "aB", "b", "B", "Zebra", "apple", "Ärger", "ärger", "zürich", "ZÜRICH", "日本", "日本語", "ǅ", "ǆ", "a\xff", "a\xfe"}
	for _, a := range names {
		for _, b := range names {
			want := strings.Compare(strings.ToLower(a), strings.ToLower(b))
			if got := compareFold(a, b); got != want {
				t.Errorf("compareFold(%q, %q) = %d, want %d", a, b, got, want)
			}
		}
	}
}

func TestWalkStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := sampleTree()
	reader.onRead = func(dir string) {
		if dir == "/srv/app" {
			cancel()
		}
	}
	var got []string
	summary := Walk(ctx, reader, "/srv", WalkOptions{Join: joinSlash, MaxEntries: 100, MaxDepth: 10}, func(found []Found) {
		for _, item := range found {
			got = append(got, path.Join(item.Dir, item.Entry.Name))
		}
	})
	if !summary.Canceled || slices.Contains(got, "app/src/main.go") || slices.Contains(reader.read, "/srv/app/src") {
		t.Fatalf("canceled walk went on: got %q, read %q, summary %+v", got, reader.read, summary)
	}
}

func TestLocalReaderWalksThisComputer(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a/b/c.txt", "a/d.txt", "e.txt"} {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	summary := Walk(context.Background(), LocalReader{}, root, WalkOptions{Join: joinLocal, MaxEntries: 100, MaxDepth: 10, Parallel: 2}, func(found []Found) {
		for _, item := range found {
			got = append(got, filepath.ToSlash(filepath.Join(item.Dir, item.Entry.Name)))
		}
	})
	want := []string{"a", "e.txt", "a/b", "a/d.txt", "a/b/c.txt"}
	if !slices.Equal(got, want) || summary.Failed != 0 {
		t.Fatalf("local walk = %q (%+v), want %q", got, summary, want)
	}
	if _, err := (LocalReader{}).ReadDir(filepath.Join(root, "missing")); err == nil {
		t.Fatal("a missing folder read without an error")
	}
}

// hugeFolder is one folder of n files with mixed-case names, the size at which
// the cost of sorting its names shows.
func hugeFolder(n int) *treeReader {
	infos := make([]os.FileInfo, n)
	for index := range infos {
		infos[index] = file(fmt.Sprintf("Report-%07d-%s.LOG", (index*7919)%n, strings.Repeat("Ab", index%5)))
	}
	return &treeReader{folders: map[string][]os.FileInfo{"/big": infos}}
}

func BenchmarkWalkHugeFolder(b *testing.B) {
	reader := hugeFolder(200_000)
	options := WalkOptions{Join: joinSlash, MaxEntries: 200_000, MaxDepth: 4}
	b.ReportAllocs()
	for b.Loop() {
		Walk(context.Background(), reader, "/big", options, func([]Found) {})
	}
}
