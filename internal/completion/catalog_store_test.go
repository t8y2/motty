package completion

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testRoots builds count distinct valid root commands. Catalog tests need a
// baseline above the load floor but otherwise do not care what the commands are.
func testRoots(count int) []*Node {
	roots := make([]*Node, count)
	for index := range roots {
		token := fmt.Sprintf("tool%03d", index)
		roots[index] = &Node{Token: token}
	}
	return roots
}

// updateArchive is the release the store tests download: the load floor and the
// shrink check both compare root counts, so it carries as many roots as the
// installed baseline.
func updateArchive(t *testing.T) []byte {
	t.Helper()
	entries := map[string]string{"carapace-bin-1.9.0/LICENSE": "MIT"}
	for index := 0; index < 200; index++ {
		name := fmt.Sprintf("tool%03d", index)
		entries["carapace-bin-1.9.0/man/cmd/"+name+"/"+name+".yaml"] = "name: " + name + "\n"
	}
	return tarGzipArchive(t, entries)
}

// repairableUpdateArchive is updateArchive plus one file with the upstream
// plain-scalar defect.
func repairableUpdateArchive(t *testing.T) []byte {
	t.Helper()
	entries := map[string]string{
		"carapace-bin-1.9.0/LICENSE":                    "MIT",
		"carapace-bin-1.9.0/man/cmd/broken/broken.yaml": "name: broken\nflags:\n  --align?: Vertical position (default: top). Details see --align-x\n",
	}
	for index := 0; index < 200; index++ {
		name := fmt.Sprintf("tool%03d", index)
		entries["carapace-bin-1.9.0/man/cmd/"+name+"/"+name+".yaml"] = "name: " + name + "\n"
	}
	return tarGzipArchive(t, entries)
}

func installedStore(t *testing.T) (*CatalogStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	catalog, err := NewCatalog(RepositoryName, "v10.0.0", "old-commit", "MIT", testRoots(200))
	if err != nil {
		t.Fatal(err)
	}
	if err := InstallCatalog(path, catalog); err != nil {
		t.Fatal(err)
	}
	return NewCatalogStore(path), path
}

type catalogTransport func(*http.Request) (*http.Response, error)

func (transport catalogTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func catalogResponse(body []byte) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
}

func storeClient(t *testing.T, latest func(*http.Request) string, download func(*http.Request)) *http.Client {
	t.Helper()
	return storeClientForArchive(t, updateArchive(t), latest, download)
}

func storeClientForArchive(t *testing.T, archive []byte, latest func(*http.Request) string, download func(*http.Request)) *http.Client {
	t.Helper()
	return &http.Client{Transport: catalogTransport(func(request *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases/latest"):
			return catalogResponse([]byte(`{"tag_name":"` + latest(request) + `"}`)), nil
		case strings.Contains(request.URL.Path, "/git/ref/tags/"):
			return catalogResponse([]byte(`{"object":{"type":"commit","sha":"new-commit"}}`)), nil
		case strings.Contains(request.URL.Path, "/tar.gz/"):
			if download != nil {
				download(request)
			}
			return catalogResponse(archive), nil
		default:
			return nil, errors.New("unexpected request: " + request.URL.String())
		}
	})}
}

func TestCatalogStoreFirstUpdateLoadsInstalledBaseline(t *testing.T) {
	store, path := installedStore(t)
	var downloads atomic.Int32
	client := storeClient(t, func(*http.Request) string { return "v11.0.0" }, func(*http.Request) { downloads.Add(1) })
	catalog, generation, _, installed, err := store.Update(context.Background(), client)
	if err != nil || catalog == nil || catalog.Release != "v11.0.0" || generation != 2 || !installed {
		t.Fatalf("update = %v, %d, %v, installed=%v", catalog, generation, err, installed)
	}
	again, sameGeneration, _, installedAgain, err := store.Update(context.Background(), client)
	if err != nil || again != catalog || sameGeneration != generation || installedAgain || downloads.Load() != 1 {
		t.Fatalf("same-version update = %p, %d, %v, installed=%v; downloads=%d", again, sameGeneration, err, installedAgain, downloads.Load())
	}
	onDisk, err := LoadInstalled(path)
	if err != nil || !bytes.Equal(onDisk.Encode(), catalog.Encode()) {
		t.Fatalf("disk and memory differ: %v", err)
	}
}

func TestCatalogStoreUpdateReportsRepairedSources(t *testing.T) {
	store, _ := installedStore(t)
	client := storeClientForArchive(t, repairableUpdateArchive(t), func(*http.Request) string { return "v11.0.0" }, nil)
	catalog, generation, stats, installed, err := store.Update(context.Background(), client)
	if err != nil || catalog == nil || generation != 2 || !installed {
		t.Fatalf("update = %v, %d, %v, installed=%v", catalog, generation, err, installed)
	}
	if stats.RepairedFiles != 1 || stats.SkippedSources != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	// The snapshot carries the counters next to the catalog, so every window
	// reads them without a second, racy lookup.
	if _, _, snapshotStats := store.Snapshot(); snapshotStats.RepairedFiles != 1 || snapshotStats.SkippedSources != 0 {
		t.Fatalf("snapshot stats = %+v", snapshotStats)
	}
	if _, _, latestStats, installedAgain, err := store.Update(context.Background(), client); err != nil || latestStats != (ImportStats{}) || installedAgain {
		t.Fatalf("up-to-date stats = %+v, installed=%v, %v", latestStats, installedAgain, err)
	}
	if _, _, snapshotStats := store.Snapshot(); snapshotStats.RepairedFiles != 1 {
		t.Fatalf("a no-op update changed the snapshot stats: %+v", snapshotStats)
	}
}

func TestCatalogStorePreservesRecoveryStatsAfterRestart(t *testing.T) {
	store, path := installedStore(t)
	entries := map[string]string{
		"repo/LICENSE": "MIT",
		"repo/man/cmd/tool000/tool000.repair.yaml": "name: repair\nflags:\n  --align: Vertical position (default: top)\n",
		"repo/man/cmd/tool000/tool000.broken.yaml": "name: broken\nflags: [not-a-map]\n",
	}
	for index := 0; index < 200; index++ {
		name := fmt.Sprintf("tool%03d", index)
		entries["repo/man/cmd/"+name+"/"+name+".yaml"] = "name: " + name + "\n"
	}
	client := storeClientForArchive(t, tarGzipArchive(t, entries), func(*http.Request) string { return "v11.0.0" }, nil)
	_, _, stats, installed, err := store.Update(context.Background(), client)
	if err != nil || !installed || stats.RepairedFiles != 1 || stats.SkippedSources != 1 {
		t.Fatalf("update: installed=%v stats=%+v err=%v", installed, stats, err)
	}
	restarted := NewCatalogStore(path)
	if _, _, note, err := restarted.Load(); err != nil || note != nil {
		t.Fatalf("restart: note=%v err=%v", note, err)
	}
	_, _, loadedStats := restarted.Snapshot()
	if loadedStats != stats {
		t.Fatalf("restart lost recovery stats: got %+v, want %+v", loadedStats, stats)
	}
}

func TestCatalogStoreConcurrentUpdateCannotRegress(t *testing.T) {
	store, path := installedStore(t)
	downloadStarted, finishDownload := make(chan struct{}), make(chan struct{})
	var checks, downloads atomic.Int32
	client := storeClient(t, func(*http.Request) string {
		if checks.Add(1) == 1 {
			return "v12.0.0"
		}
		return "v11.0.0"
	}, func(*http.Request) {
		downloads.Add(1)
		close(downloadStarted)
		<-finishDownload
	})
	first := make(chan error, 1)
	go func() { _, _, _, _, err := store.Update(context.Background(), client); first <- err }()
	<-downloadStarted
	second := make(chan error, 1)
	go func() { _, _, _, _, err := store.Update(context.Background(), client); second <- err }()
	close(finishDownload)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	catalog, generation, _ := store.Snapshot()
	installed, err := LoadInstalled(path)
	if err != nil || catalog.Release != "v12.0.0" || installed.Release != "v12.0.0" || generation != 2 || downloads.Load() != 1 {
		t.Fatalf("regressed: memory=%s disk=%v generation=%d downloads=%d err=%v", catalog.Release, installed, generation, downloads.Load(), err)
	}
}

func TestCatalogStoreCanceledWaitDoesNotInstall(t *testing.T) {
	store, _ := installedStore(t)
	started, finish := make(chan struct{}), make(chan struct{})
	client := storeClient(t, func(*http.Request) string { return "v11.0.0" }, func(*http.Request) { close(started); <-finish })
	first := make(chan error, 1)
	go func() { _, _, _, _, err := store.Update(context.Background(), client); first <- err }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() { _, _, _, _, err := store.Update(ctx, client); second <- err }()
	cancel()
	select {
	case err := <-second:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting update = %v", err)
		}
	case <-time.After(time.Second):
		close(finish)
		t.Fatal("canceled update waited for the active download")
	}
	catalog, generation, _ := store.Snapshot()
	if catalog.Release != "v10.0.0" || generation != 1 {
		t.Fatalf("canceled waiter changed snapshot: %s, %d", catalog.Release, generation)
	}
	close(finish)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestCatalogStoreLoadContextCancelsWhileUpdateHoldsWriter(t *testing.T) {
	store, _ := installedStore(t)
	started, finish := make(chan struct{}), make(chan struct{})
	client := storeClient(t, func(*http.Request) string { return "v11.0.0" }, func(*http.Request) { close(started); <-finish })
	updating := make(chan error, 1)
	go func() { _, _, _, _, err := store.Update(context.Background(), client); updating <- err }()
	<-started
	defer func() {
		close(finish)
		if err := <-updating; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	loaded := make(chan error, 1)
	go func() { _, _, _, err := store.LoadContext(ctx); loaded <- err }()
	cancel()
	select {
	case err := <-loaded:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting load = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled load waited for another window's download")
	}
	catalog, generation, _ := store.Snapshot()
	if catalog.Release != "v10.0.0" || generation != 1 {
		t.Fatalf("canceled load changed snapshot: %s, %d", catalog.Release, generation)
	}
}

func TestCatalogStoreCanceledLoadDoesNotInitialize(t *testing.T) {
	store, _ := installedStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := store.LoadContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled load = %v", err)
	}
	if catalog, generation, _ := store.Snapshot(); catalog != nil || generation != 0 {
		t.Fatal("a canceled load initialized the store")
	}
	if catalog, generation, _, err := store.Load(); err != nil || catalog == nil || generation != 1 {
		t.Fatalf("loading after cancellation = %v, %d, %v", catalog, generation, err)
	}
}

func TestCatalogStoreCanceledDownloadKeepsDiskAndSnapshot(t *testing.T) {
	store, path := installedStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	client := storeClient(t, func(*http.Request) string { return "v11.0.0" }, func(*http.Request) { cancel() })
	_, _, _, _, err := store.Update(ctx, client)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled download = %v", err)
	}
	catalog, generation, _ := store.Snapshot()
	installed, loadErr := LoadInstalled(path)
	if loadErr != nil || catalog.Release != "v10.0.0" || installed.Release != "v10.0.0" || generation != 1 {
		t.Fatalf("canceled download changed baseline: %v %d %v", catalog, generation, loadErr)
	}
}

func TestCatalogStoreMissingOrBrokenCacheFallsBackOnce(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "broken"}[broken], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			if broken {
				if err := os.WriteFile(path, []byte("broken"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			store := NewCatalogStore(path)
			catalog, generation, note, err := store.Load()
			if err != nil || catalog == nil || generation != 1 || (note != nil) != broken {
				t.Fatalf("load = %v, %d, %v, %v", catalog, generation, note, err)
			}
			if err := InstallCatalog(path, testCatalog(t)); err != nil {
				t.Fatal(err)
			}
			again, againGeneration, _, err := store.Load()
			if err != nil || again != catalog || againGeneration != generation {
				t.Fatal("Load reread the file after initialization")
			}
		})
	}
}

func TestCatalogStoreFirstUpdateRejectsShrinkAgainstBuiltin(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	store := NewCatalogStore(path)
	client := storeClient(t, func(*http.Request) string { return "v99.0.0" }, nil)
	if _, _, _, _, err := store.Update(context.Background(), client); err == nil {
		t.Fatal("first update installed a one-command archive over the built-in catalog")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shrunk catalog was installed: %v", err)
	}
	catalog, generation, _ := store.Snapshot()
	if catalog == nil || catalog.RootCount() < 700 || generation != 1 {
		t.Fatalf("built-in baseline was not retained: %v, %d", catalog, generation)
	}
}

func TestCatalogStoreLoadPrefersNewerBuiltinAndRejectsInvalidVersion(t *testing.T) {
	builtin, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"v0.1.0", builtin.Release, "nightly"} {
		t.Run(version, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			catalog, err := NewCatalog(RepositoryName, version, "different-commit", "MIT", testRoots(200))
			if err != nil {
				t.Fatal(err)
			}
			if err := InstallCatalog(path, catalog); err != nil {
				t.Fatal(err)
			}
			loaded, generation, note, err := NewCatalogStore(path).Load()
			if err != nil || loaded != builtin || generation != 1 || (note != nil) != (version == "nightly") {
				t.Fatalf("load = %p, %d, %v, %v; builtin=%p", loaded, generation, note, err, builtin)
			}
		})
	}
}

// Root count alone can look healthy while most subcommands and options were
// lost; the update path refuses such a catalog and keeps the old file.
func TestCatalogStoreUpdateRejectsLowCoverage(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	roots := testRoots(200)
	for _, root := range roots[:150] {
		root.Children = []*Node{{Token: "child", Flags: []Flag{{Long: "flag"}}}}
	}
	baseline, err := NewCatalog(RepositoryName, "v10.0.0", "old-commit", "MIT", roots)
	if err != nil {
		t.Fatal(err)
	}
	if err := InstallCatalog(path, baseline); err != nil {
		t.Fatal(err)
	}
	store := NewCatalogStore(path)
	client := storeClient(t, func(*http.Request) string { return "v11.0.0" }, nil)
	catalog, generation, _, installed, err := store.Update(context.Background(), client)
	if err == nil || installed {
		t.Fatalf("partial catalog was accepted: release=%v generation=%d installed=%v", catalog.Release, generation, installed)
	}
	onDisk, diskErr := LoadInstalled(path)
	if diskErr != nil || !bytes.Equal(onDisk.Encode(), baseline.Encode()) {
		t.Fatalf("refused update changed the installed file: %v", diskErr)
	}
	still, stillGeneration, _ := store.Snapshot()
	if still.Release != "v10.0.0" || stillGeneration != 1 {
		t.Fatalf("refused update changed the snapshot: %s, %d", still.Release, stillGeneration)
	}
}

func TestCatalogStoreLoadRejectsAnImprobablySmallInstalledCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	tiny, err := NewCatalog(RepositoryName, "v99.0.0", "deadbeef", "MIT", []*Node{{Token: "tool"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := InstallCatalog(path, tiny); err != nil {
		t.Fatal(err)
	}
	loaded, generation, note, err := NewCatalogStore(path).Load()
	builtin, builtinErr := Builtin()
	if builtinErr != nil {
		t.Fatal(builtinErr)
	}
	if err != nil || note == nil || loaded != builtin || generation != 1 {
		t.Fatalf("load = %p, %d, %v, %v; builtin=%p", loaded, generation, note, err, builtin)
	}
}

func TestCatalogStoresKeepPathsIndependent(t *testing.T) {
	first, _ := installedStore(t)
	second, _ := installedStore(t)
	_, _, _, err := second.Load()
	if err != nil {
		t.Fatal(err)
	}
	client := storeClient(t, func(*http.Request) string { return "v11.0.0" }, nil)
	if _, _, _, _, err := first.Update(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	catalog, generation, _ := second.Snapshot()
	if catalog.Release != "v10.0.0" || generation != 1 {
		t.Fatalf("another data directory changed this store: %s, %d", catalog.Release, generation)
	}
}

func TestLoadInstalledRejectsOversizedCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxArchiveBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	if _, err := LoadInstalled(path); err == nil {
		t.Fatal("an oversized cache was loaded")
	}
	catalog, generation, note, err := NewCatalogStore(path).Load()
	if err != nil || note == nil || catalog == nil || generation != 1 {
		t.Fatalf("oversized cache fallback = %v, %d, %v, %v", catalog, generation, note, err)
	}
}

// treeRoots builds count roots that each hold size-1 subcommands.
func treeRoots(t *testing.T, count, size int) []*Node {
	t.Helper()
	roots := testRoots(count)
	for _, root := range roots {
		for index := 1; index < size; index++ {
			root.Children = append(root.Children, &Node{Token: fmt.Sprintf("sub%02d", index)})
		}
	}
	return roots
}

func newTestCatalog(t *testing.T, roots []*Node) *Catalog {
	t.Helper()
	catalog, err := NewCatalog(RepositoryName, "v10.0.0", "commit", "MIT", roots)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

// A release can keep nearly every command in aggregate and still have lost
// several whole command trees; that must not replace a more complete catalog.
func TestCheckContinuityRefusesLostAndShrunkCommands(t *testing.T) {
	current := newTestCatalog(t, treeRoots(t, 200, 10))

	lost := treeRoots(t, 200, 10)[5:]
	if !coversBaseline(newTestCatalog(t, lost), current) {
		t.Fatal("the fixture should pass the aggregate check, or it proves nothing")
	}
	err := checkContinuity(newTestCatalog(t, lost), current)
	if err == nil || !strings.Contains(err.Error(), "tool000") {
		t.Fatalf("a catalog missing five commands was accepted or the error is unreadable: %v", err)
	}

	shrunk := treeRoots(t, 200, 10)
	for _, root := range shrunk[:5] {
		root.Children = root.Children[:3]
	}
	err = checkContinuity(newTestCatalog(t, shrunk), current)
	if err == nil || !strings.Contains(err.Error(), "tool000 (10 to 4)") {
		t.Fatalf("a catalog with five shrunken command trees was accepted or the error is unreadable: %v", err)
	}
}

// Upstream retires or reshapes a command now and then. Refusing that would
// block every later update until the installed catalog was deleted.
func TestCheckContinuityAllowsOrdinaryUpstreamChange(t *testing.T) {
	current := newTestCatalog(t, treeRoots(t, 200, 10))
	next := treeRoots(t, 200, 10)[1:] // one command retired
	next[0].Children = next[0].Children[:5]
	next = append(next, &Node{Token: "brandnew"})
	if err := checkContinuity(newTestCatalog(t, next), current); err != nil {
		t.Fatalf("a normal upstream change was refused: %v", err)
	}
}

func TestCatalogStoreUpdateRefusesLostCommands(t *testing.T) {
	store, path := installedStore(t)
	entries := map[string]string{"carapace-bin-1.9.0/LICENSE": "MIT"}
	for index := 5; index < 200; index++ { // tool000 to tool004 are gone
		name := fmt.Sprintf("tool%03d", index)
		entries["carapace-bin-1.9.0/man/cmd/"+name+"/"+name+".yaml"] = "name: " + name + "\n"
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	client := storeClientForArchive(t, tarGzipArchive(t, entries), func(*http.Request) string { return "v11.0.0" }, nil)
	_, _, _, installed, err := store.Update(context.Background(), client)
	if err == nil || installed || !strings.Contains(err.Error(), "tool000") {
		t.Fatalf("an update that lost commands was installed or its reason is unreadable: installed=%v err=%v", installed, err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("a refused update changed the installed file: %v", readErr)
	}
}
